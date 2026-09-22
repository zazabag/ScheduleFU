-- Модуль notes: запись пары, расшифровка, конспект, домашние задания.
--
-- Владелец таблиц — notes, пишет в них только он (ARCHITECTURE.md § 5).
--
-- Ключевое решение схемы: связи с lessons нет и быть не может. Таблица
-- lessons хранит текущее окно сбора и чистится при каждом проходе — через
-- неделю пара исчезает, и внешний ключ унёс бы конспект вместе с ней.
-- Поэтому каждая запись несёт собственный слепок пары: дисциплина, дата,
-- время, преподаватель, аудитория. Это заодно снимает юридический вопрос:
-- конспект — производное произведение пользователя, а не копия расписания
-- вуза (docs/03-legal-risks.md).

-- Владелец — анонимный ключ устройства из cookie. Ни почты, ни имени, ни
-- пароля: аккаунтов в проекте нет (ARCHITECTURE.md § 0), а конспект всё
-- равно должен кому-то принадлежать.
CREATE TABLE recordings (
    id            BIGSERIAL PRIMARY KEY,
    owner_key     TEXT        NOT NULL,
    -- Слепок пары. subject_key — чьё расписание открывали: group:ПИ24-1.
    subject_key   TEXT        NOT NULL,
    discipline    TEXT        NOT NULL,
    lesson_date   DATE        NOT NULL,
    begins_at     TIME,
    ends_at       TIME,
    lecturer_name TEXT        NOT NULL DEFAULT '',
    auditorium    TEXT        NOT NULL DEFAULT '',
    kind_of_work  TEXT        NOT NULL DEFAULT '',
    -- Справочно, на момент создания. Намеренно не внешний ключ.
    lesson_oid    BIGINT,

    -- record — писали в приложении, upload — принесли готовый файл.
    source        TEXT        NOT NULL DEFAULT 'record',
    -- uploading → queued → decoding → transcribing → summarizing → ready | failed
    status        TEXT        NOT NULL DEFAULT 'uploading',
    -- Кусок дозагрузки: клиент шлёт их по порядку, сервер дописывает в файл.
    -- Номер последнего принятого нужен, чтобы продолжить после обрыва.
    chunks        INTEGER     NOT NULL DEFAULT 0,
    bytes         BIGINT      NOT NULL DEFAULT 0,
    duration_sec  INTEGER     NOT NULL DEFAULT 0,
    -- Путь к файлу, пока он есть. Аудио удаляется сразу после успешной
    -- расшифровки: голос преподавателя на нашем диске не хранится.
    audio_path    TEXT        NOT NULL DEFAULT '',
    -- Расшифровка остаётся: по ней можно пересобрать конспект, не заставляя
    -- человека записывать пару заново.
    transcript    TEXT        NOT NULL DEFAULT '',

    attempts        SMALLINT    NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failure         TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX recordings_owner_idx ON recordings (owner_key, lesson_date DESC);
-- Очередь обработки: воркер берёт строки этим индексом.
CREATE INDEX recordings_queue_idx ON recordings (next_attempt_at)
    WHERE status IN ('queued', 'decoding', 'transcribing', 'summarizing');

-- Конспект. Пока saved_at пуст — это черновик, показанный человеку сразу
-- после обработки; он живёт до первого «Сохранить» и чистится по сроку.
CREATE TABLE notes (
    id            BIGSERIAL PRIMARY KEY,
    owner_key     TEXT        NOT NULL,
    recording_id  BIGINT      REFERENCES recordings (id) ON DELETE SET NULL,
    subject_key   TEXT        NOT NULL,
    discipline    TEXT        NOT NULL,
    lesson_date   DATE        NOT NULL,
    begins_at     TIME,
    lecturer_name TEXT        NOT NULL DEFAULT '',
    auditorium    TEXT        NOT NULL DEFAULT '',
    title         TEXT        NOT NULL DEFAULT '',
    -- Разметка нарочно простая: абзацы, списки и подзаголовки. Текст пришёл
    -- от модели и недоверенный — шаблон его экранирует, разметку рисуем сами.
    body          TEXT        NOT NULL DEFAULT '',
    theses        TEXT[]      NOT NULL DEFAULT '{}',
    saved_at      TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX notes_owner_idx ON notes (owner_key, subject_key, discipline, lesson_date DESC);

-- Домашние задания. Отдельная таблица, а не поле конспекта: у задания своя
-- жизнь — срок, отметка «сделано», и добавить его можно руками, без записи.
CREATE TABLE homeworks (
    id          BIGSERIAL PRIMARY KEY,
    owner_key   TEXT        NOT NULL,
    note_id     BIGINT      REFERENCES notes (id) ON DELETE SET NULL,
    subject_key TEXT        NOT NULL,
    discipline  TEXT        NOT NULL,
    -- Дата пары, на которой задали. Срок сдачи — отдельно и может быть пуст:
    -- «к следующему занятию» датой не является.
    lesson_date DATE        NOT NULL,
    due_date    DATE,
    due_note    TEXT        NOT NULL DEFAULT '',
    body        TEXT        NOT NULL,
    -- ai — выделено моделью из расшифровки, manual — вписано руками.
    origin      TEXT        NOT NULL DEFAULT 'ai',
    done_at     TIMESTAMPTZ,
    saved_at    TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX homeworks_owner_idx ON homeworks (owner_key, subject_key, discipline, lesson_date DESC);
CREATE INDEX homeworks_due_idx ON homeworks (owner_key, due_date) WHERE done_at IS NULL AND saved_at IS NOT NULL;
