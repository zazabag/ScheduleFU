package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/platform/db"
)

func testStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	pool := db.TestPool(t, "TEST_DATABASE_URL_OPS", "schedulefu_test_ops", "collector_runs", "recordings", "llm_calls")
	return New(pool), context.Background()
}

// Последний проход упал, но удачный был раньше: «обновлено» считается от
// удачного, а ошибка последнего видна отдельно.
func TestSborUpalPosleUdachnogo(t *testing.T) {
	s, ctx := testStore(t)
	now := time.Now()
	_, err := s.pool.Exec(ctx, `INSERT INTO collector_runs (started_at, finished_at, period_from, period_to, lessons_seen, requests, failure)
		VALUES ($1, $1, current_date, current_date, 11000, 541, NULL),
		       ($2, $2, current_date, current_date, 0, 12, 'проход прерван: источник отказал с кодом 429')`,
		now.Add(-2*time.Hour), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Collector(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Found || c.Lessons != 11000 || c.LastFailure == "" || now.Sub(c.LastOK) < 90*time.Minute {
		t.Errorf("сбор: %+v", c)
	}
}

func TestRashodIOtkazyModeli(t *testing.T) {
	s, ctx := testStore(t)
	now := time.Now()
	_, err := s.pool.Exec(ctx, `INSERT INTO llm_calls (at, kind, model, ok, prompt_tokens, completion_tokens, code, message) VALUES
		($1, 'summary', 'm', true, 30000, 2000, '', ''),
		($2, 'probe', 'm', false, 0, 0, '1304', 'лимит'),
		($3, 'summary', 'm', true, 5000, 500, '', '')`,
		now.Add(-3*time.Hour), now.Add(-time.Hour), now.Add(-3*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.LLM(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if l.Calls24h != 2 || l.Errors24h != 1 || l.Tokens24h != 32000 || l.Tokens7d != 37500 || l.LastErrCode != "1304" {
		t.Errorf("модель: %+v", l)
	}
	if !l.LastErrAt.After(l.LastOKAt) {
		t.Error("отказ после удачного вызова не виден")
	}
}

func TestOcheredZapisey(t *testing.T) {
	s, ctx := testStore(t)
	now := time.Now()
	_, err := s.pool.Exec(ctx, `INSERT INTO recordings (owner_key, subject_key, discipline, lesson_date, status, updated_at, failure) VALUES
		('o','g','d',current_date,'queued',$1,''),
		('o','g','d',current_date,'transcribing',$2,''),
		('o','g','d',current_date,'failed',$1,'микрофон писал тишину'),
		('o','g','d',current_date,'ready',$1,'')`, now, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.Notes(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if n.Queued != 1 || n.Working != 1 || n.Stuck != 1 || n.Failed24h != 1 || n.Ready24h != 1 || n.LastFailure != "микрофон писал тишину" {
		t.Errorf("очередь: %+v", n)
	}
}
