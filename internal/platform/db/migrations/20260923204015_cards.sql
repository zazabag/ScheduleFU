-- Карточки для повторения и словарь терминов.
--
-- Владелец — notes. Карточки делает модель по сохранённому конспекту, по
-- кнопке и в очереди: сервер отвечает за тридцать секунд, а модели на
-- конспект может понадобиться больше. Очередь — в самих конспектах:
-- cards_status '' → queued → working → ready | failed.
ALTER TABLE notes ADD COLUMN cards_status  TEXT        NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN cards_failure TEXT        NOT NULL DEFAULT '';
ALTER TABLE notes ADD COLUMN cards_at      TIMESTAMPTZ;
CREATE INDEX notes_cards_queue_idx ON notes (cards_at) WHERE cards_status IN ('queued', 'working');

-- Карточка живёт у предмета, а не у конспекта: конспект удалят, а
-- выученное остаться должно. Повторение — по коробкам Лейтнера.
CREATE TABLE cards (
    id          BIGSERIAL PRIMARY KEY,
    owner_key   TEXT        NOT NULL,
    note_id     BIGINT      REFERENCES notes (id) ON DELETE SET NULL,
    subject_key TEXT        NOT NULL,
    discipline  TEXT        NOT NULL,
    lesson_date DATE        NOT NULL,
    kind        TEXT        NOT NULL,          -- card | term
    front       TEXT        NOT NULL,
    back        TEXT        NOT NULL,
    box         SMALLINT    NOT NULL DEFAULT 1,
    due_on      DATE        NOT NULL,
    reviewed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Повторная генерация по тому же предмету не плодит одинаковых карточек.
    UNIQUE (owner_key, subject_key, discipline, kind, front)
);
CREATE INDEX cards_due_idx ON cards (owner_key, subject_key, discipline, due_on);
