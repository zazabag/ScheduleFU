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

// Repository — хранилище модуля.
type Repository interface {
	Save(ctx context.Context, s domain.Subscription) error
	Delete(ctx context.Context, transport, target, subjectKey string) error
	For(ctx context.Context, subjectKeys []string) ([]domain.Subscription, error)

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
