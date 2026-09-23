// Package postgres — отчётные запросы присмотра к чужим таблицам.
//
// Только чтение: collector_runs принадлежит schedule, recordings и
// llm_calls — notes. Договорённость записана в ops/interfaces.go.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zazabag/schedulefu/internal/modules/ops"
	"github.com/zazabag/schedulefu/internal/modules/ops/domain"
)

// Store — отчётные запросы.
type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Collector — последний проход и последний удачный.
func (s *Store) Collector(ctx context.Context) (domain.Collector, error) {
	var c domain.Collector
	var finished *time.Time
	var failure *string
	err := s.pool.QueryRow(ctx, `SELECT started_at, finished_at, failure, lessons_seen, requests, errors
		FROM collector_runs ORDER BY started_at DESC LIMIT 1`).
		Scan(&c.Last, &finished, &failure, &c.Lessons, &c.Requests, &c.Errors)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("проходы сбора: %w", err)
	}
	c.Found = true
	if failure != nil {
		c.LastFailure = *failure
	}
	// Удачный проход — не обязательно последний: последний мог упасть.
	err = s.pool.QueryRow(ctx, `SELECT finished_at, lessons_seen, requests FROM collector_runs
		WHERE finished_at IS NOT NULL AND failure IS NULL ORDER BY finished_at DESC LIMIT 1`).
		Scan(&c.LastOK, &c.Lessons, &c.Requests)
	if errors.Is(err, pgx.ErrNoRows) {
		c.Found = false
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("удачный проход: %w", err)
	}
	return c, nil
}

// Notes — очередь записей: что ждёт, что обрабатывается, что зависло и чем
// кончились сутки.
func (s *Store) Notes(ctx context.Context, now time.Time) (domain.Notes, error) {
	var n domain.Notes
	err := s.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE status = 'queued'),
		count(*) FILTER (WHERE status IN ('decoding','transcribing','summarizing')),
		count(*) FILTER (WHERE status IN ('decoding','transcribing','summarizing') AND updated_at < $1::timestamptz - interval '1 hour'),
		count(*) FILTER (WHERE status = 'ready' AND updated_at > $1::timestamptz - interval '24 hours'),
		count(*) FILTER (WHERE status = 'failed' AND updated_at > $1::timestamptz - interval '24 hours')
		FROM recordings`, now).Scan(&n.Queued, &n.Working, &n.Stuck, &n.Ready24h, &n.Failed24h)
	if err != nil {
		return n, fmt.Errorf("очередь записей: %w", err)
	}
	err = s.pool.QueryRow(ctx, `SELECT failure FROM recordings
		WHERE failure <> '' AND updated_at > $1::timestamptz - interval '24 hours' ORDER BY updated_at DESC LIMIT 1`, now).
		Scan(&n.LastFailure)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return n, fmt.Errorf("последняя ошибка записи: %w", err)
	}
	return n, nil
}

// LLM — расход модели и последний отказ.
func (s *Store) LLM(ctx context.Context, now time.Time) (domain.LLM, error) {
	var l domain.LLM
	var lastOK *time.Time
	err := s.pool.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE at > $1::timestamptz - interval '24 hours'),
		count(*) FILTER (WHERE at > $1::timestamptz - interval '24 hours' AND NOT ok),
		COALESCE(sum(prompt_tokens + completion_tokens) FILTER (WHERE at > $1::timestamptz - interval '24 hours'), 0),
		COALESCE(sum(prompt_tokens + completion_tokens) FILTER (WHERE at > $1::timestamptz - interval '7 days'), 0),
		max(at) FILTER (WHERE ok)
		FROM llm_calls`, now).Scan(&l.Calls24h, &l.Errors24h, &l.Tokens24h, &l.Tokens7d, &lastOK)
	if err != nil {
		return l, fmt.Errorf("расход модели: %w", err)
	}
	if lastOK != nil {
		l.LastOKAt = *lastOK
	}
	err = s.pool.QueryRow(ctx, `SELECT at, code, message FROM llm_calls WHERE NOT ok ORDER BY at DESC LIMIT 1`).
		Scan(&l.LastErrAt, &l.LastErrCode, &l.LastErr)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return l, fmt.Errorf("последний отказ модели: %w", err)
	}
	return l, nil
}

var _ ops.Store = (*Store)(nil)
