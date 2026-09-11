// Package store — хранилище слепков расписания в PostgreSQL.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store — пул соединений с базой.
type Store struct {
	pool *pgxpool.Pool
}

// Open подключается к базе и применяет миграции.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: разбор DSN: %w", err)
	}
	// Сборщик пишет пачками, API читает — небольшого пула достаточно.
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: подключение: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: база не отвечает: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

// Close закрывает пул.
func (s *Store) Close() { s.pool.Close() }

// Pool даёт доступ к пулу для запросов вне пакета.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// migrate применяет неприменённые миграции по порядку имён.
//
// Своя реализация вместо внешней библиотеки: миграций в проекте немного,
// а лишняя зависимость с собственным CLI и форматом версий тут ничего не
// экономит. Каждая миграция идёт в отдельной транзакции.
func (s *Store) migrate(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name       TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("store: таблица миграций: %w", err)
	}

	applied := map[string]bool{}
	rows, err := s.pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("store: чтение применённых миграций: %w", err)
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(entries)

	for _, path := range entries {
		name := path[len("migrations/"):]
		if applied[name] {
			continue
		}
		body, err := migrationFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := s.applyOne(ctx, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyOne(ctx context.Context, name, body string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: миграция %s: %w", name, err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("store: миграция %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
		return fmt.Errorf("store: отметка миграции %s: %w", name, err)
	}
	return tx.Commit(ctx)
}

// inTx выполняет fn в транзакции, откатывая её при ошибке.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
