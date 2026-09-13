package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PushSubscription — подписка устройства на изменения одного расписания.
type PushSubscription struct {
	ID         int64
	SubjectKey string
	Endpoint   string
	P256dh     string
	Auth       string
}

// SaveSubscription сохраняет подписку.
//
// Повторная подписка тем же устройством на то же расписание не создаёт
// вторую запись, а обновляет ключи: браузер меняет их при переустановке
// приложения, и без обновления отправка молча перестала бы работать.
func (s *Store) SaveSubscription(ctx context.Context, sub PushSubscription) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO push_subscriptions (subject_key, endpoint, p256dh, auth)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (endpoint, subject_key) DO UPDATE SET
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			last_seen_at = now(),
			failures = 0`,
		sub.SubjectKey, sub.Endpoint, sub.P256dh, sub.Auth)
	if err != nil {
		return fmt.Errorf("сохранение подписки: %w", err)
	}
	return nil
}

// DeleteSubscription снимает подписку устройства.
//
// Пустой subjectKey снимает все подписки этого устройства: так работает
// кнопка «отключить уведомления» на самом устройстве.
func (s *Store) DeleteSubscription(ctx context.Context, endpoint, subjectKey string) error {
	var err error
	if subjectKey == "" {
		_, err = s.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE endpoint = $1`, endpoint)
	} else {
		_, err = s.pool.Exec(ctx,
			`DELETE FROM push_subscriptions WHERE endpoint = $1 AND subject_key = $2`,
			endpoint, subjectKey)
	}
	if err != nil {
		return fmt.Errorf("удаление подписки: %w", err)
	}
	return nil
}

// SubscriptionsFor возвращает подписки на указанные расписания.
func (s *Store) SubscriptionsFor(ctx context.Context, subjectKeys []string) ([]PushSubscription, error) {
	if len(subjectKeys) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, subject_key, endpoint, p256dh, auth
		  FROM push_subscriptions
		 WHERE subject_key = ANY($1)`, subjectKeys)
	if err != nil {
		return nil, fmt.Errorf("чтение подписок: %w", err)
	}
	defer rows.Close()

	var out []PushSubscription
	for rows.Next() {
		var p PushSubscription
		if err := rows.Scan(&p.ID, &p.SubjectKey, &p.Endpoint, &p.P256dh, &p.Auth); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OutboxItem — письмо в очереди отправки.
type OutboxItem struct {
	ID           int64
	Subscription PushSubscription
	Payload      []byte
	Attempts     int
}

// Enqueue кладёт уведомления в очередь.
func (s *Store) Enqueue(ctx context.Context, subscriptionIDs []int64, payload any) error {
	if len(subscriptionIDs) == 0 {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO push_outbox (subscription_id, payload)
		SELECT unnest($1::bigint[]), $2::jsonb`, subscriptionIDs, body)
	if err != nil {
		return fmt.Errorf("постановка в очередь: %w", err)
	}
	return nil
}

// TakeOutbox забирает пачку писем, готовых к отправке.
//
// Строки блокируются с пропуском занятых, чтобы несколько отправщиков
// могли работать одновременно и не брать одно и то же письмо дважды.
func (s *Store) TakeOutbox(ctx context.Context, limit int) ([]OutboxItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		WITH picked AS (
			SELECT id FROM push_outbox
			 WHERE delivered_at IS NULL AND failed_reason IS NULL
			   AND next_attempt_at <= now()
			 ORDER BY next_attempt_at
			 LIMIT $1
			 FOR UPDATE SKIP LOCKED
		)
		UPDATE push_outbox o
		   SET attempts = o.attempts + 1,
		       -- Сразу отодвигаем следующую попытку: если отправщик умрёт
		       -- на полуслове, письмо не будет крутиться в горячем цикле.
		       next_attempt_at = now() + make_interval(secs => least(300, 30 * (o.attempts + 1)))
		  FROM picked p, push_subscriptions s
		 WHERE o.id = p.id AND s.id = o.subscription_id
		RETURNING o.id, o.payload, o.attempts,
		          s.id, s.subject_key, s.endpoint, s.p256dh, s.auth`, limit)
	if err != nil {
		return nil, fmt.Errorf("выборка очереди: %w", err)
	}
	defer rows.Close()

	var out []OutboxItem
	for rows.Next() {
		var it OutboxItem
		if err := rows.Scan(&it.ID, &it.Payload, &it.Attempts,
			&it.Subscription.ID, &it.Subscription.SubjectKey,
			&it.Subscription.Endpoint, &it.Subscription.P256dh, &it.Subscription.Auth); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// MarkDelivered отмечает письмо доставленным.
func (s *Store) MarkDelivered(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE push_outbox SET delivered_at = now() WHERE id = $1`, id)
	return err
}

// MarkFailed отмечает письмо неотправленным насовсем.
func (s *Store) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE push_outbox SET failed_reason = $2 WHERE id = $1`, id, reason)
	return err
}

// DropSubscription удаляет мёртвую подписку вместе с её очередью.
//
// Push-сервис отвечает 404 или 410, когда устройство отписалось или
// приложение удалили. Держать такую подписку бессмысленно: каждая попытка
// будет стоить запроса и закончится тем же.
func (s *Store) DropSubscription(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM push_outbox WHERE subscription_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
		return err
	})
}

// NoteFailure отмечает временную неудачу доставки.
func (s *Store) NoteFailure(ctx context.Context, subscriptionID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE push_subscriptions SET failures = failures + 1 WHERE id = $1`, subscriptionID)
	return err
}

// CleanupOutbox убирает старые доставленные и окончательно неудачные письма.
func (s *Store) CleanupOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM push_outbox
		 WHERE (delivered_at IS NOT NULL OR failed_reason IS NOT NULL)
		   AND created_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
