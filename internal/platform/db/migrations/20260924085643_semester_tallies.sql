-- Итоги семестра: суммы по расписанию, без сырых пар.
--
-- Владелец — schedule. Для «Моего семестра в Финашке» нужна история, а
-- хранить пары за прошлое мы не будем (ARCHITECTURE.md § 5, риск по
-- ст. 1334 ГК). Поэтому копятся только суммы: сколько пар и часов, где и
-- чего больше всего. По ним нельзя восстановить, что было в какой день.
CREATE TABLE semester_tallies (
    subject_key TEXT        NOT NULL,          -- group:ПИ24-1 | lecturer:46674
    semester    TEXT        NOT NULL,          -- 2026-1 — осень, 2026-2 — весна
    lessons     INTEGER     NOT NULL DEFAULT 0,
    minutes     INTEGER     NOT NULL DEFAULT 0,
    days        INTEGER     NOT NULL DEFAULT 0,
    buildings   JSONB       NOT NULL DEFAULT '{}',  -- адрес → пар
    rooms       JSONB       NOT NULL DEFAULT '{}',  -- аудитория → пар
    disciplines JSONB       NOT NULL DEFAULT '{}',  -- дисциплина → минут
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (subject_key, semester)
);

-- День, уже добавленный в итоги: вечерних проходов несколько, а день
-- считается один раз.
CREATE TABLE tally_days (
    day      DATE        PRIMARY KEY,
    subjects INTEGER     NOT NULL DEFAULT 0,
    done_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
