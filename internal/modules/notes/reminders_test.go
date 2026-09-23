package notes

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Напоминания fakeRepo не трогают: заглушка держит интерфейс.
func (f *fakeRepo) PendingHomeworks(context.Context, time.Time, time.Time) ([]domain.Homework, error) {
	return nil, nil
}

// pendingRepo — несделанные задания двух устройств.
type pendingRepo struct {
	Repository
	hws           []domain.Homework
	since, before time.Time
}

func (r *pendingRepo) PendingHomeworks(_ context.Context, since, before time.Time) ([]domain.Homework, error) {
	r.since, r.before = since, before
	return r.hws, nil
}

// plan — завтра у ПИ24-1 финансы, у ПИ24-2 ничего.
type plan struct{ asked map[string]int }

func (p *plan) Disciplines(_ context.Context, key string, _ time.Time) (map[string]bool, error) {
	p.asked[key]++
	if key == "group:ПИ24-1" {
		return map[string]bool{"Финансы": true}, nil
	}
	return nil, nil
}

func TestNapominaniyaKZavtrashnimParam(t *testing.T) {
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	mk := func(owner, key, disc, body string, due *time.Time) domain.Homework {
		return domain.Homework{OwnerKey: owner, Body: body, DueDate: due,
			Lesson: domain.LessonRef{SubjectKey: key, Discipline: disc}}
	}
	repo := &pendingRepo{hws: []domain.Homework{
		mk("a", "group:ПИ24-1", "Финансы", "задача 5", nil), // пара завтра — да
		mk("a", "group:ПИ24-1", "История", "эссе", nil),     // пары завтра нет — нет
		mk("b", "group:ПИ24-2", "Право", "кейс", &day),      // срок датой на завтра — да
		mk("b", "group:ПИ24-1", "Финансы", "параграф", nil), // та же группа — расписание уже прочитано
	}}
	p := &plan{asked: map[string]int{}}
	clk, _ := clock.New("Europe/Moscow")
	s := New(repo, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{})
	s.Plan = p

	rs, err := s.Reminders(context.Background(), day)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].OwnerKey != "a" || rs[0].Body != "Финансы — задача 5" {
		t.Fatalf("напоминания: %+v", rs)
	}
	if rs[1].Title != "Завтра: не сделано 2 задания к 2 парам" {
		t.Errorf("у второго устройства: %+v", rs[1])
	}
	if p.asked["group:ПИ24-1"] != 1 {
		t.Errorf("расписание группы читается один раз, а прочитано %d", p.asked["group:ПИ24-1"])
	}
	if repo.before != day || repo.since != day.AddDate(0, 0, -30) {
		t.Errorf("окно заданий: %v — %v", repo.since, repo.before)
	}
}
