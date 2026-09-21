package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPool даёт пакету тестов чистую базу.
//
// База — своя на пакет (envKey задаёт переменную с DSN, fallback — имя базы):
// go test гоняет пакеты параллельно, и на общей базе тесты одного пакета
// вычищают таблицы под ногами у другого. Падало это только в полном прогоне
// и выглядело случайным.
func TestPool(t *testing.T, envKey, fallbackDB string, tables ...string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(envKey)
	if dsn == "" {
		dsn = "postgres://localhost:5432/" + fallbackDB + "?sslmode=disable"
	}
	ctx := context.Background()
	pool, err := Open(ctx, dsn)
	if err != nil {
		t.Skipf("тестовая база недоступна (%v)", err)
	}
	if len(tables) > 0 {
		q := "TRUNCATE "
		for i, tb := range tables {
			if i > 0 {
				q += ", "
			}
			q += tb
		}
		if _, err := pool.Exec(ctx, q+" RESTART IDENTITY CASCADE"); err != nil {
			t.Fatalf("очистка базы: %v", err)
		}
	}
	t.Cleanup(pool.Close)
	return pool
}
