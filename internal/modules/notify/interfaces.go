// Package notify — подписки на изменения расписания и их доставка.
//
// Владеет таблицами subscriptions и outbox. Изменения читает у schedule
// через порт ChangeReader; транспорт доставки — порт Transport, реализаций
// может быть несколько (web push сейчас, Telegram и почта потом) без правок
// в планировщике и очереди.
package notify

import (
	"context"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// ChangeReader — то, что notify хочет от schedule.
type ChangeReader interface {
	ChangesSince(ctx context.Context, since time.Time, limit int) ([]sched.Change, error)
}

// Transport доставляет одно письмо одному адресату.
type Transport interface {
	Name() string
	Send(ctx context.Context, d domain.Delivery) (domain.Outcome, error)
}

// Reminder — напоминание одному устройству.
type Reminder struct {
	OwnerKey     string
	Notification domain.Notification
}

// ReminderSource — кто знает, о чём напомнить на день. Реализует модуль
// заданий (notes) через переходник в cmd: notify не знает, что такое
// домашнее задание, а notes — что такое подписка.
type ReminderSource interface {
	Reminders(ctx context.Context, day time.Time) ([]Reminder, error)
}

// DayLessons — пары расписания в день. Реализует schedule через
// переходник в cmd; notify уже знает домен schedule (ChangeReader), поэтому
// пары приходят как есть.
type DayLessons interface {
	LessonsOn(ctx context.Context, subjectKey string, day time.Time) ([]sched.Lesson, error)
}

// Repository — хранилище модуля.
type Repository interface {
	Save(ctx context.Context, s domain.Subscription) error
	Delete(ctx context.Context, transport, target, subjectKey string) error
	For(ctx context.Context, subjectKeys []string) ([]domain.Subscription, error)
	// SetMorning включает или выключает утреннюю сводку подписки; false —
	// такой подписки нет.
	SetMorning(ctx context.Context, transport, target, subjectKey string, on bool) (bool, error)
	Morning(ctx context.Context, transport, target, subjectKey string) (bool, error)
	// MorningSubscriptions — подписки с включённой утренней сводкой.
	MorningSubscriptions(ctx context.Context) ([]domain.Subscription, error)
	// ClaimMorningDay отмечает день сводок; false — уже отмечен.
	ClaimMorningDay(ctx context.Context, day time.Time) (bool, error)
	// ForOwners — подписки устройств по ключам владельцев.
	ForOwners(ctx context.Context, owners []string) ([]domain.Subscription, error)
	// ClaimReminderDay отмечает день напоминаний; false — день уже отмечен
	// (этим или другим процессом), ставить в очередь не нужно.
	ClaimReminderDay(ctx context.Context, day time.Time) (bool, error)

	Enqueue(ctx context.Context, subscriptionIDs []int64, n domain.Notification) error
	// Take забирает пачку писем к отправке с блокировкой строк: два
	// отправщика не должны доставить одно письмо дважды.
	Take(ctx context.Context, limit int) ([]domain.Delivery, error)
	MarkDelivered(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, reason string) error
	NoteFailure(ctx context.Context, subscriptionID int64) error
	DropSubscription(ctx context.Context, id int64) error
	CleanupOutbox(ctx context.Context, olderThan time.Duration) (int64, error)
}
