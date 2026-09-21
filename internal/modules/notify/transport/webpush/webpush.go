// Package webpush — доставка через Web Push.
package webpush

import (
	"context"
	"fmt"
	"io"
	"net/http"

	wp "github.com/SherClockHolmes/webpush-go"

	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
)

// Name — имя транспорта в подписках.
const Name = "webpush"

// Keys — пара VAPID. Создаётся один раз и живёт с сервисом: при смене все
// подписки перестают работать.
type Keys struct{ Public, Private, Subject string }

// Generate создаёт новую пару.
func Generate() (Keys, error) {
	priv, pub, err := wp.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("генерация ключей VAPID: %w", err)
	}
	return Keys{Public: pub, Private: priv}, nil
}

// Transport — реализация notify.Transport.
type Transport struct{ keys Keys }

func New(keys Keys) *Transport { return &Transport{keys: keys} }

func (t *Transport) Name() string { return Name }

func (t *Transport) Send(ctx context.Context, d domain.Delivery) (domain.Outcome, error) {
	sub := &wp.Subscription{
		Endpoint: d.Subscription.Target,
		Keys:     wp.Keys{P256dh: d.Subscription.Credentials["p256dh"], Auth: d.Subscription.Credentials["auth"]},
	}
	resp, err := wp.SendNotificationWithContext(ctx, d.Payload, sub, &wp.Options{
		Subscriber: t.keys.Subject, VAPIDPublicKey: t.keys.Public, VAPIDPrivateKey: t.keys.Private,
		TTL: 86400, Urgency: wp.UrgencyNormal, // сутки: вчерашнее изменение — не новость
	})
	if err != nil {
		return domain.Retry, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) // иначе соединение не вернётся в пул
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return domain.Delivered, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return domain.Dead, nil
	default:
		return domain.Retry, fmt.Errorf("push-сервис ответил %d", resp.StatusCode)
	}
}
