-- Вызовы модели конспекта: расход и отказы. bigmodel.cn остатка лимита не
-- сообщает, поэтому считаем сами — по этой таблице бот в Telegram
-- показывает расход и замечает отказы раньше студента.
-- Писатель — модуль notes (адаптер llm); читатель для отчёта — ops.
-- Живёт 90 дней: для «сколько тратим в неделю» больше не нужно.
CREATE TABLE llm_calls (
    id                BIGSERIAL PRIMARY KEY,
    at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind              TEXT        NOT NULL,          -- summary | part | probe
    model             TEXT        NOT NULL,
    ok                BOOLEAN     NOT NULL,
    prompt_tokens     INTEGER     NOT NULL DEFAULT 0,
    completion_tokens INTEGER     NOT NULL DEFAULT 0,
    duration_ms       INTEGER     NOT NULL DEFAULT 0,
    code              TEXT        NOT NULL DEFAULT '',
    message           TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX llm_calls_at_idx ON llm_calls (at DESC);
