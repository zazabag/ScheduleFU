-- Справочник аудиторий. Обновляется редко: фонд меняется раз в семестр.
CREATE TABLE auditoriums (
    oid            BIGINT PRIMARY KEY,
    name           TEXT        NOT NULL,
    prefix         TEXT        NOT NULL DEFAULT '',
    room           TEXT        NOT NULL,
    building       TEXT        NOT NULL DEFAULT '',
    campus         TEXT        NOT NULL,
    kind           TEXT        NOT NULL DEFAULT '',
    -- NULL означает «неизвестно», а не «первый этаж»: нумерация в корпусах
    -- разная, и там, где правило не подтверждено, этаж не угадывается.
    floor          SMALLINT,
    -- Вместимость приходит только вместе с парой, поэтому у аудиторий без
    -- занятий она неизвестна.
    capacity       SMALLINT,
    is_study_space BOOLEAN     NOT NULL DEFAULT TRUE,
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX auditoriums_campus_idx ON auditoriums (campus, building, room)
    WHERE is_study_space;

-- Группы. id — идентификатор источника из поиска.
CREATE TABLE groups (
    id             BIGINT PRIMARY KEY,
    name           TEXT        NOT NULL,
    -- Источник отдаёт только числовой oid факультета; названия у него нет.
    faculty_oid    TEXT        NOT NULL DEFAULT '',
    -- Год набора из названия группы (ПИ24-1 -> 24), NULL если не разобран.
    admission_year SMALLINT,
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX groups_name_idx ON groups (lower(name) text_pattern_ops);

-- Преподаватели. Ключ — только числовой lecturerOid: запрос расписания по
-- GUID возвращает чужие пары вместо ошибки, см. docs/06-api-findings.md.
CREATE TABLE lecturers (
    oid          BIGINT PRIMARY KEY,
    name         TEXT        NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX lecturers_name_idx ON lecturers (lower(name) text_pattern_ops);

-- Текущий слепок расписания.
--
-- Хранится именно текущее состояние, а не история всех выгрузок: архивировать
-- базу вуза целиком мы не имеем права (docs/03-legal-risks.md). История
-- ограничена журналом изменений ниже.
CREATE TABLE lessons (
    lesson_oid     BIGINT PRIMARY KEY,
    lesson_date    DATE        NOT NULL,
    begins_at      TIME        NOT NULL,
    ends_at        TIME        NOT NULL,

    -- Связь с справочником намеренно мягкая, без внешнего ключа: источник
    -- нам не подчиняется и может прислать пару в аудитории, которой в нашем
    -- справочнике ещё нет. Падать на этом сборщик не должен — справочник
    -- он пополняет сам, из того же слепка.
    auditorium_oid BIGINT,
    auditorium     TEXT        NOT NULL DEFAULT '',
    building       TEXT        NOT NULL DEFAULT '',

    discipline     TEXT        NOT NULL DEFAULT '',
    kind_of_work   TEXT        NOT NULL DEFAULT '',

    lecturer_oid   BIGINT,
    lecturer_name  TEXT        NOT NULL DEFAULT '',

    -- stream в источнике — строка "ПИ24-1; ПИ24-2"; groups — её разбор,
    -- по нему адресуются уведомления.
    stream         TEXT        NOT NULL DEFAULT '',
    group_names    TEXT[]      NOT NULL DEFAULT '{}',
    subgroup       TEXT        NOT NULL DEFAULT '',
    note           TEXT        NOT NULL DEFAULT '',

    -- Отпечаток значимых полей: сравнение слепков идёт по нему, а не
    -- пополю.
    fingerprint    TEXT        NOT NULL,
    -- Когда деканат последний раз правил пару (modifieddate источника).
    source_modified_at TIMESTAMPTZ,

    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Основной запрос «что занято в этой аудитории в этот день».
CREATE INDEX lessons_auditorium_date_idx ON lessons (auditorium_oid, lesson_date, begins_at);
-- Расписание преподавателя и ответ на «где преподаватель сейчас».
CREATE INDEX lessons_lecturer_date_idx ON lessons (lecturer_oid, lesson_date, begins_at);
-- Расписание группы: поиск по элементу массива.
CREATE INDEX lessons_groups_idx ON lessons USING GIN (group_names);
CREATE INDEX lessons_date_idx ON lessons (lesson_date);

-- Журнал изменений расписания.
--
-- Это главная функция, которой нет у источника: вуз правит расписание задним
-- числом и никого не уведомляет. Изменение обнаруживается сравнением нового
-- слепка со старым по lesson_oid.
CREATE TYPE lesson_change_kind AS ENUM ('added', 'removed', 'changed');

CREATE TABLE lesson_changes (
    id             BIGSERIAL PRIMARY KEY,
    lesson_oid     BIGINT      NOT NULL,
    kind           lesson_change_kind NOT NULL,
    detected_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Денормализованные поля для адресации уведомлений без join.
    lesson_date    DATE        NOT NULL,
    group_names    TEXT[]      NOT NULL DEFAULT '{}',
    lecturer_oid   BIGINT,
    auditorium_oid BIGINT,

    -- Состояние до и после. Для added before пуст, для removed — after.
    before         JSONB,
    after          JSONB
);

CREATE INDEX lesson_changes_detected_idx ON lesson_changes (detected_at DESC);
CREATE INDEX lesson_changes_groups_idx ON lesson_changes USING GIN (group_names);
CREATE INDEX lesson_changes_date_idx ON lesson_changes (lesson_date);

-- Журнал проходов сборщика: видно, когда данные обновлялись в последний раз
-- и насколько они свежи. Свежесть показывается пользователю.
CREATE TABLE collector_runs (
    id            BIGSERIAL PRIMARY KEY,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    period_from   DATE        NOT NULL,
    period_to     DATE        NOT NULL,
    requests      INTEGER     NOT NULL DEFAULT 0,
    errors        INTEGER     NOT NULL DEFAULT 0,
    lessons_seen  INTEGER     NOT NULL DEFAULT 0,
    changes_found INTEGER     NOT NULL DEFAULT 0,
    failure       TEXT
);

CREATE INDEX collector_runs_started_idx ON collector_runs (started_at DESC);
