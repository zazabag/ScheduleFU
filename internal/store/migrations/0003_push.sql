-- Подписки на уведомления.
--
-- Подписка привязана к тому, чьё расписание отслеживают: группе или
-- преподавателю. Одно устройство может следить за несколькими — студент за
-- своей группой, преподаватель за собой, — поэтому пара (устройство, кого
-- слушаем) уникальна, а не одно устройство.
--
-- Из данных устройства хранится только то, без чего отправка невозможна:
-- адрес доставки и ключи шифрования. Ни адреса, ни браузера, ни чего-либо
-- ещё: сервис работает без входа, и заводить досье на посетителя незачем.
CREATE TABLE push_subscriptions (
    id           BIGSERIAL PRIMARY KEY,
    subject_key  TEXT        NOT NULL,
    endpoint     TEXT        NOT NULL,
    p256dh       TEXT        NOT NULL,
    auth         TEXT        NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Подряд идущие отказы. Push-сервис отвечает 404 или 410, когда
    -- подписка мертва; такие удаляются сразу, а временные сбои копятся.
    failures     SMALLINT    NOT NULL DEFAULT 0,
    UNIQUE (endpoint, subject_key)
);

CREATE INDEX push_subscriptions_subject_idx ON push_subscriptions (subject_key);

-- Очередь отправки.
--
-- Отправлять прямо во время прохода сборщика нельзя: push-сервис может
-- отвечать секундами, а проход держит транзакцию с одиннадцатью тысячами
-- пар. Очередь разделяет «заметили изменение» и «доставили» — и позволяет
-- повторить, когда сеть подвела.
CREATE TABLE push_outbox (
    id              BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT      NOT NULL REFERENCES push_subscriptions (id) ON DELETE CASCADE,
    payload         JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts        SMALLINT    NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    delivered_at    TIMESTAMPTZ,
    failed_reason   TEXT
);

-- Индекс по неотправленным: очередь разбирается только по ним, а доставленные
-- остаются лежать для истории и попадать в выборку не должны.
CREATE INDEX push_outbox_pending_idx ON push_outbox (next_attempt_at)
    WHERE delivered_at IS NULL AND failed_reason IS NULL;
