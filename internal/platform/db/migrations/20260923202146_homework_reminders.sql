-- Напоминания о домашних заданиях накануне пары.
--
-- Владелец таблиц — notify. Задания принадлежат ключу устройства (notes),
-- а подписка на уведомления — адресу доставки и расписанию. Чтобы
-- напомнить «завтра к паре задали вот это», подписке нужен тот же ключ
-- устройства. Он не личность: случайные 16 байт из cookie, по нему никого
-- не узнать — досье по-прежнему нет (ARCHITECTURE.md § 5).
ALTER TABLE subscriptions ADD COLUMN owner_key TEXT;
CREATE INDEX subscriptions_owner_idx ON subscriptions (owner_key) WHERE owner_key IS NOT NULL;

-- День, за который напоминания уже поставлены в очередь. Служба может
-- перезапуститься вечером хоть трижды — напоминание всё равно одно.
CREATE TABLE reminder_days (
    day        DATE        PRIMARY KEY,
    planned_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
