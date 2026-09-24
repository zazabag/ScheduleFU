package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

type tallyRepo struct {
	Repository
	done     map[string]bool
	semester string
	tallies  map[string]domain.Tally
}

func (r *tallyRepo) DayAttributions(context.Context, time.Time) ([]domain.Attributed, error) {
	return []domain.Attributed{{SubjectKey: "group:ПИ24-1", Lesson: domain.Lesson{BeginsAt: "10:10", EndsAt: "11:40", Discipline: "Финансы"}}}, nil
}

func (r *tallyRepo) AddDayTally(_ context.Context, day time.Time, semester string, t map[string]domain.Tally) (bool, error) {
	k := day.Format("2006-01-02")
	if r.done[k] {
		return false, nil
	}
	r.done[k], r.semester, r.tallies = true, semester, t
	return true, nil
}

func TestItogiSemestraVecheromRazVDen(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	repo := &tallyRepo{done: map[string]bool{}}
	at := func(h, m int) *Service {
		return New(nil, repo, clock.Fixed(time.Date(2026, 9, 24, h, m, 0, 0, msk)), nil, Options{})
	}
	if n, _ := at(22, 0).TallyToday(context.Background()); n != 0 || len(repo.done) != 0 {
		t.Fatal("в 22:00 пары ещё идут — день не складываем")
	}
	n, err := at(23, 5).TallyToday(context.Background())
	if err != nil || n != 1 || repo.semester != "2026-1" || repo.tallies["group:ПИ24-1"].Minutes != 90 {
		t.Fatalf("вечером: %d %v %s %+v", n, err, repo.semester, repo.tallies)
	}
	if n, _ := at(23, 59).TallyToday(context.Background()); n != 0 {
		t.Error("второй вечерний проход сложил день ещё раз")
	}
}
