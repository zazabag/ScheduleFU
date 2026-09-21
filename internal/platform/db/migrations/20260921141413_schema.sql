-- Схема ScheduleFU целиком. Один вуз = одна база: мультитенантности в одной
-- базе намеренно нет (ARCHITECTURE.md § 9), поэтому ключа вуза в таблицах нет.
--
-- Владельцы таблиц — по § 5 канона. Только владелец пишет.

-- ─── schedule ────────────────────────────────────────────────────────────────

CREATE TABLE auditoriums (
    oid            BIGINT PRIMARY KEY,
    name           TEXT        NOT NULL,
    room           TEXT        NOT NULL,
    building       TEXT        NOT NULL DEFAULT '',
    -- Площадка — ключ фильтрации. Ленинградские 49, 51 и 55 — одна площадка:
    -- физически одно место с переходами. Склейка живёт в source, не здесь.
    site           TEXT        NOT NULL,
    site_label     TEXT        NOT NULL DEFAULT '',
    site_order     SMALLINT    NOT NULL DEFAULT 99,
    kind           TEXT        NOT NULL DEFAULT '',
    -- NULL — «неизвестно», не «первый этаж»: где правило нумерации корпуса
    -- не подтверждено, этаж не угадывается.
    floor          SMALLINT,
    -- Приходит только вместе с парой; у пустующих аудиторий неизвестна.
    capacity       SMALLINT,
    is_study_space BOOLEAN     NOT NULL DEFAULT TRUE,
    first_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX auditoriums_site_idx ON auditoriums (site, floor, room) WHERE is_study_space;

CREATE TABLE groups (
    id             BIGINT PRIMARY KEY,
    name           TEXT        NOT NULL,
    -- Источник отдаёт только номер факультета; названия у него нет.
    faculty_oid    TEXT        NOT NULL DEFAULT '',
    admission_year SMALLINT,
    last_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX groups_name_idx ON groups (lower(name) text_pattern_ops);

-- Ключ — только числовой lecturerOid: запрос по GUID возвращает чужие пары.
CREATE TABLE lecturers (
    oid          BIGINT PRIMARY KEY,
    name         TEXT        NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lecturers_name_idx ON lecturers (lower(name) text_pattern_ops);

-- Текущее окно расписания. Не история: архивировать базу вуза мы не вправе.
CREATE TABLE lessons (
    lesson_oid         BIGINT PRIMARY KEY,
    lesson_date        DATE        NOT NULL,
    begins_at          TIME        NOT NULL,
    ends_at            TIME        NOT NULL,
    -- Связь со справочником мягкая: источник может прислать пару в
    -- аудитории, которой у нас ещё нет, и падать на этом нельзя.
    auditorium_oid     BIGINT,
    auditorium         TEXT        NOT NULL DEFAULT '',
    building           TEXT        NOT NULL DEFAULT '',
    discipline         TEXT        NOT NULL DEFAULT '',
    kind_of_work       TEXT        NOT NULL DEFAULT '',
    lecturer_oid       BIGINT,
    lecturer_name      TEXT        NOT NULL DEFAULT '',
    stream             TEXT        NOT NULL DEFAULT '',
    group_names        TEXT[]      NOT NULL DEFAULT '{}',
    subgroup           TEXT        NOT NULL DEFAULT '',
    note               TEXT        NOT NULL DEFAULT '',
    -- Отпечаток значимых полей: сравнение слепков идёт по нему.
    fingerprint        TEXT        NOT NULL,
    source_modified_at TIMESTAMPTZ,
    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX lessons_auditorium_date_idx ON lessons (auditorium_oid, lesson_date, begins_at);
CREATE INDEX lessons_lecturer_date_idx   ON lessons (lecturer_oid, lesson_date, begins_at);
CREATE INDEX lessons_groups_idx          ON lessons USING GIN (group_names);
CREATE INDEX lessons_date_idx            ON lessons (lesson_date);

CREATE TYPE lesson_change_kind AS ENUM ('added', 'removed', 'changed');

-- Журнал изменений: то, чего нет у источника. Живёт 30 дней.
CREATE TABLE lesson_changes (
    id             BIGSERIAL PRIMARY KEY,
    lesson_oid     BIGINT             NOT NULL,
    kind           lesson_change_kind NOT NULL,
    detected_at    TIMESTAMPTZ        NOT NULL DEFAULT now(),
    lesson_date    DATE               NOT NULL,
    -- Адресные поля дублируются: у удалённой пары брать их уже неоткуда.
    group_names    TEXT[]             NOT NULL DEFAULT '{}',
    lecturer_oid   BIGINT,
    auditorium_oid BIGINT,
    before         JSONB,
    after          JSONB
);
CREATE INDEX lesson_changes_detected_idx ON lesson_changes (detected_at DESC);
CREATE INDEX lesson_changes_groups_idx   ON lesson_changes USING GIN (group_names);

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

-- ─── notify ──────────────────────────────────────────────────────────────────

-- Подписка адресата на расписание. Транспорт — часть ключа: одно устройство
-- может слушать группу через push, а человек — ту же группу в Telegram.
-- Из данных адресата хранится только то, без чего доставка невозможна.
CREATE TABLE subscriptions (
    id           BIGSERIAL PRIMARY KEY,
    subject_key  TEXT        NOT NULL,          -- group:ПИ24-1 | lecturer:46674
    transport    TEXT        NOT NULL,          -- webpush | telegram | email
    target       TEXT        NOT NULL,          -- endpoint | chat_id | адрес
    -- Ключи шифрования push и прочее транспортное; никаких досье.
    credentials  JSONB       NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failures     SMALLINT    NOT NULL DEFAULT 0,
    UNIQUE (transport, target, subject_key)
);
CREATE INDEX subscriptions_subject_idx ON subscriptions (subject_key);

-- Очередь доставки. Отправка отделена от прохода сборщика: транспорт может
-- отвечать секундами, а проход держит транзакцию с тысячами пар.
CREATE TABLE outbox (
    id              BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT      NOT NULL REFERENCES subscriptions (id) ON DELETE CASCADE,
    payload         JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        SMALLINT    NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at    TIMESTAMPTZ,
    failed_reason   TEXT
);
CREATE INDEX outbox_pending_idx ON outbox (next_attempt_at)
    WHERE delivered_at IS NULL AND failed_reason IS NULL;
