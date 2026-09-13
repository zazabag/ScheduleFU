package push

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/zazabag/schedulefu/internal/store"
)

// Keys — пара ключей VAPID, которой push-сервисы удостоверяют отправителя.
type Keys struct {
	Public  string
	Private string
	// Subject — контакт отправителя: push-сервисы требуют mailto: или
	// адрес сайта, чтобы было к кому обратиться при злоупотреблении.
	Subject string
}

// GenerateKeys создаёт новую пару ключей.
func GenerateKeys() (Keys, error) {
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("генерация ключей VAPID: %w", err)
	}
	return Keys{Public: public, Private: private}, nil
}

// Valid сообщает, что ключи заданы и отправка возможна.
func (k Keys) Valid() bool { return k.Public != "" && k.Private != "" }

// Sender разбирает очередь и доставляет уведомления.
type Sender struct {
	store *store.Store
	keys  Keys
	log   *slog.Logger

	// Batch — сколько писем брать за раз.
	Batch int
	// MaxAttempts — после скольких неудач письмо признаётся недоставленным.
	// Push-сервис может лежать, но держать письмо вечно бессмысленно:
	// расписание к тому времени успеет измениться ещё раз.
	MaxAttempts int
}

// NewSender создаёт отправщик.
func NewSender(s *store.Store, keys Keys, log *slog.Logger) *Sender {
	if log == nil {
		log = slog.Default()
	}
	return &Sender{store: s, keys: keys, log: log, Batch: 50, MaxAttempts: 5}
}

// Result — итог разбора очереди.
type Result struct {
	Delivered int
	Dropped   int // подписок удалено как мёртвые
	Failed    int
	Retry     int
}

// Deliver отправляет одну пачку уведомлений.
func (s *Sender) Deliver(ctx context.Context) (Result, error) {
	var res Result
	if !s.keys.Valid() {
		return res, errors.New("push: ключи VAPID не заданы")
	}

	items, err := s.store.TakeOutbox(ctx, s.Batch)
	if err != nil {
		return res, err
	}
	for _, item := range items {
		status, err := s.sendOne(ctx, item)
		switch {
		case err == nil && status >= 200 && status < 300:
			if err := s.store.MarkDelivered(ctx, item.ID); err != nil {
				s.log.Warn("не отмечено доставленным", "письмо", item.ID, "ошибка", err)
			}
			res.Delivered++

		case status == http.StatusNotFound || status == http.StatusGone:
			// Устройство отписалось или приложение удалили. Подписка мертва
			// навсегда: каждая следующая попытка закончится тем же.
			if err := s.store.DropSubscription(ctx, item.Subscription.ID); err != nil {
				s.log.Warn("мёртвая подписка не удалена", "подписка", item.Subscription.ID, "ошибка", err)
			}
			res.Dropped++

		case item.Attempts >= s.MaxAttempts:
			reason := fmt.Sprintf("статус %d", status)
			if err != nil {
				reason = err.Error()
			}
			if err := s.store.MarkFailed(ctx, item.ID, reason); err != nil {
				s.log.Warn("не отмечено неудачным", "письмо", item.ID, "ошибка", err)
			}
			res.Failed++

		default:
			// Временная неудача: письмо уже отодвинуто на следующую попытку
			// при выборке из очереди.
			if err := s.store.NoteFailure(ctx, item.Subscription.ID); err != nil {
				s.log.Warn("не записана неудача", "подписка", item.Subscription.ID, "ошибка", err)
			}
			res.Retry++
		}
	}
	return res, nil
}

func (s *Sender) sendOne(ctx context.Context, item store.OutboxItem) (int, error) {
	sub := &webpush.Subscription{
		Endpoint: item.Subscription.Endpoint,
		Keys: webpush.Keys{
			P256dh: item.Subscription.P256dh,
			Auth:   item.Subscription.Auth,
		},
	}
	resp, err := webpush.SendNotificationWithContext(ctx, item.Payload, sub, &webpush.Options{
		Subscriber:      s.keys.Subject,
		VAPIDPublicKey:  s.keys.Public,
		VAPIDPrivateKey: s.keys.Private,
		// Сутки: расписание, изменившееся вчера, уведомлением уже не новость.
		TTL: 86400,
		// Уведомление об изменении расписания срочным не является:
		// пусть система доставит его, когда телефон всё равно проснётся.
		Urgency: webpush.UrgencyNormal,
	})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Тело ответа читаем и выбрасываем: без этого соединение не вернётся
	// в пул и каждое уведомление будет открывать новое.
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// Run разбирает очередь по кругу, пока не отменят контекст.
func (s *Sender) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			res, err := s.Deliver(ctx)
			if err != nil {
				s.log.Warn("очередь уведомлений не разобрана", "ошибка", err)
				continue
			}
			if res.Delivered+res.Dropped+res.Failed+res.Retry > 0 {
				s.log.Info("уведомления отправлены",
					"доставлено", res.Delivered, "отписалось", res.Dropped,
					"неудачно", res.Failed, "повторим", res.Retry)
			}
		}
	}
}
