-- Утренняя сводка дня: сколько пар, где первая.
--
-- Владелец — notify. Сводка — по желанию: человек подписывался на
-- изменения расписания, и ежедневное уведомление без спроса было бы
-- спамом. Флажок живёт у подписки — у пары «устройство + расписание».
ALTER TABLE subscriptions ADD COLUMN morning BOOLEAN NOT NULL DEFAULT false;

-- День, за который сводки уже поставлены: перезапуск службы утром второго
-- письма не даст.
CREATE TABLE morning_days (
    day        DATE        PRIMARY KEY,
    planned_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
