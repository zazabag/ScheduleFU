package schedule

import (
	"context"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// windowRepo — площадка из трёх аудиторий и справочник для якоря.
type windowRepo struct {
	Repository
	days  []RoomDay
	asked string
}

func (r *windowRepo) Auditorium(_ context.Context, oid int64) (domain.Auditorium, bool, error) {
	for _, d := range r.days {
		if d.Auditorium.Oid == oid {
			return d.Auditorium, true, nil
		}
	}
	return domain.Auditorium{}, false, nil
}

func (r *windowRepo) SiteDay(_ context.Context, site string, _ time.Time) ([]RoomDay, error) {
	r.asked = site
	return r.days, nil
}

func TestOknoIshchetsyaNaPloshchadkeSleduyushcheyPary(t *testing.T) {
	floor := func(n int) *int { return &n }
	lp := domain.Site{Slug: "leningradsky", Label: "Ленинградский"}
	room := func(oid int64, f int, cap *int) domain.Auditorium {
		return domain.Auditorium{Oid: oid, Room: "x", Building: "ЛП49", Site: lp, Floor: floor(f), Capacity: cap, IsStudySpace: true}
	}
	repo := &windowRepo{days: []RoomDay{
		{Auditorium: room(1, 5, nil)},
		{Auditorium: room(2, 2, floor(20))},
		{Auditorium: room(3, 4, floor(80)), Lessons: []domain.Lesson{{BeginsAt: "12:00", EndsAt: "13:30"}}},
		{Auditorium: room(4, 4, nil)},
	}}
	moscow := time.FixedZone("MSK", 3*3600)
	clk := clock.Fixed(time.Date(2026, 9, 23, 11, 0, 0, 0, moscow))
	s := New(nil, repo, clk, nil, Options{})

	w, err := s.FreeWindow(context.Background(), "", clk.Today(), "11:40", "14:00", 3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if repo.asked != "leningradsky" {
		t.Errorf("площадка берётся у якоря, а спросили %q", repo.asked)
	}
	var got []int64
	for _, v := range w.Rooms {
		got = append(got, v.Auditorium.Oid)
	}
	// 3 занята посреди окна; 4 на этаже якоря, 1 на соседнем, 2 дальше всех.
	want := []int64{4, 1, 2}
	if len(got) != len(want) {
		t.Fatalf("аудитории %v, ожидались %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок %v, ожидался %v", got, want)
		}
	}

	w, _ = s.FreeWindow(context.Background(), "", clk.Today(), "11:40", "14:00", 3, 30)
	for _, v := range w.Rooms {
		if v.Auditorium.Oid == 2 {
			t.Error("аудитория на 20 мест не проходит фильтр «от 30»")
		}
	}
	if len(w.Rooms) != 2 {
		t.Errorf("неизвестная вместимость не отсекается: осталось %d", len(w.Rooms))
	}
}
