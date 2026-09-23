// Package postgres — хранилище подписок и очереди.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zazabag/schedulefu/internal/modules/notify"
	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

type Repo struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Save: повторная подписка не создаёт вторую запись, а обновляет ключи —
// браузер меняет их при переустановке, и без обновления отправка молча
// перестала бы работать.
func (r *Repo) Save(ctx context.Context, s domain.Subscription) error {
	creds, err := json.Marshal(s.Credentials)
	if err != nil {
		return err
	}
	// Ключ устройства не затирается пустым: подписку могут подтвердить из
	// вкладки, где cookie ещё не выдан.
	_, err = r.pool.Exec(ctx, `INSERT INTO subscriptions (subject_key, transport, target, credentials, owner_key)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')) ON CONFLICT (transport, target, subject_key) DO UPDATE SET
		credentials=EXCLUDED.credentials, last_seen_at=now(), failures=0,
		owner_key=COALESCE(EXCLUDED.owner_key, subscriptions.owner_key)`,
		s.SubjectKey, s.Transport, s.Target, creds, s.OwnerKey)
	if err != nil {
		return fmt.Errorf("сохранение подписки: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, transport, target, subjectKey string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM subscriptions WHERE transport=$1 AND target=$2 AND ($3='' OR subject_key=$3)`,
		transport, target, subjectKey)
	if err != nil {
		return fmt.Errorf("удаление подписки: %w", err)
	}
	return nil
}

func (r *Repo) ForOwners(ctx context.Context, owners []string) ([]domain.Subscription, error) {
	if len(owners) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, subject_key, transport, target, credentials, owner_key
		FROM subscriptions WHERE owner_key = ANY($1) ORDER BY id`, owners)
	if err != nil {
		return nil, fmt.Errorf("подписки владельцев: %w", err)
	}
	defer rows.Close()
	var out []domain.Subscription
	for rows.Next() {
		var s domain.Subscription
		var creds []byte
		if err := rows.Scan(&s.ID, &s.SubjectKey, &s.Transport, &s.Target, &creds, &s.OwnerKey); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(creds, &s.Credentials)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) ClaimReminderDay(ctx context.Context, day time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO reminder_days (day) VALUES ($1) ON CONFLICT DO NOTHING`, day.Format("2006-01-02"))
	if err != nil {
		return false, fmt.Errorf("день напоминаний: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repo) For(ctx context.Context, keys []string) ([]domain.Subscription, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, subject_key, transport, target, credentials
		FROM subscriptions WHERE subject_key = ANY($1)`, keys)
	if err != nil {
		return nil, fmt.Errorf("чтение подписок: %w", err)
	}
	defer rows.Close()
	var out []domain.Subscription
	for rows.Next() {
		var s domain.Subscription
		var creds []byte
		if err := rows.Scan(&s.ID, &s.SubjectKey, &s.Transport, &s.Target, &creds); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(creds, &s.Credentials)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) Enqueue(ctx context.Context, ids []int64, n domain.Notification) error {
	if len(ids) == 0 {
		return nil
	}
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `INSERT INTO outbox (subscription_id, payload) SELECT unnest($1::bigint[]), $2::jsonb`, ids, body)
	if err != nil {
		return fmt.Errorf("постановка в очередь: %w", err)
	}
	return nil
}

// Take забирает письма с SKIP LOCKED и сразу отодвигает следующую попытку:
// упавший на полуслове отправщик не оставит письмо в горячем цикле.
func (r *Repo) Take(ctx context.Context, limit int) ([]domain.Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		WITH picked AS (
			SELECT id FROM outbox
			 WHERE delivered_at IS NULL AND failed_reason IS NULL AND next_attempt_at <= now()
			 ORDER BY next_attempt_at LIMIT $1 FOR UPDATE SKIP LOCKED)
		UPDATE outbox o SET attempts = o.attempts + 1,
		       next_attempt_at = now() + make_interval(secs => least(300, 30 * (o.attempts + 1)))
		  FROM picked p, subscriptions s
		 WHERE o.id = p.id AND s.id = o.subscription_id
		RETURNING o.id, o.payload, o.attempts, s.id, s.subject_key, s.transport, s.target, s.credentials`, limit)
	if err != nil {
		return nil, fmt.Errorf("выборка очереди: %w", err)
	}
	defer rows.Close()
	var out []domain.Delivery
	for rows.Next() {
		var d domain.Delivery
		var creds []byte
		if err := rows.Scan(&d.ID, &d.Payload, &d.Attempts, &d.Subscription.ID, &d.Subscription.SubjectKey,
			&d.Subscription.Transport, &d.Subscription.Target, &creds); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(creds, &d.Subscription.Credentials)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) MarkDelivered(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET delivered_at=now() WHERE id=$1`, id)
	return err
}

func (r *Repo) MarkFailed(ctx context.Context, id int64, reason string) error {
	_, err := r.pool.Exec(ctx, `UPDATE outbox SET failed_reason=$2 WHERE id=$1`, id, reason)
	return err
}

func (r *Repo) NoteFailure(ctx context.Context, subscriptionID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE subscriptions SET failures=failures+1 WHERE id=$1`, subscriptionID)
	return err
}

func (r *Repo) DropSubscription(ctx context.Context, id int64) error {
	return db.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM outbox WHERE subscription_id=$1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM subscriptions WHERE id=$1`, id)
		return err
	})
}

func (r *Repo) CleanupOutbox(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM outbox WHERE (delivered_at IS NOT NULL OR failed_reason IS NOT NULL)
		AND created_at < now() - $1::interval`, fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

var _ notify.Repository = (*Repo)(nil)
