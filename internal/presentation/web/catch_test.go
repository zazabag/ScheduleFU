package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// lecturerRepo — неделя преподавателя: четверг с окном, пятница подряд.
type lecturerRepo struct{ windowRepo }

func (lecturerRepo) ScheduleFor(_ context.Context, s sched.Subject, from, _ time.Time) ([]sched.Lesson, error) {
	if s.Kind != sched.SubjectLecturer {
		return nil, nil
	}
	thu, fri := from, from.AddDate(0, 0, 1)
	b := "Ленинградский проспект, 51, корп. 1"
	mk := func(d time.Time, from, to, room string) sched.Lesson {
		return sched.Lesson{Date: d, BeginsAt: from, EndsAt: to, Building: b, Auditorium: "ЛП51_1/" + room}
	}
	return []sched.Lesson{
		mk(thu, "08:30", "10:00", "0412"), mk(thu, "14:00", "15:30", "0326"),
		mk(fri, "10:10", "11:40", "0611"), mk(fri, "11:50", "13:20", "0611"),
	}, nil
}

func TestKogdaZastatPrepodavatelyaNaNedele(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	clk := clock.Fixed(time.Date(2026, 9, 23, 9, 0, 0, 0, moscow))
	s, err := New(Deps{Schedule: schedule.New(nil, lecturerRepo{}, clk, nil, schedule.Options{}), Clock: clk,
		BuildingLabel: func(string) string { return "Ленинградский" }})
	if err != nil {
		t.Fatal(err)
	}
	days := s.catchDays(context.Background(), 46674, clk.Today())
	if len(days) != 2 {
		t.Fatalf("дней %d: %+v", len(days), days)
	}
	thu, fri := days[0], days[1]
	if thu.Span != "08:30—15:30 · 2 пары" || thu.Windows != "10:00—14:00" || !strings.Contains(thu.Where, "0326, 0412") {
		t.Errorf("четверг: %+v", thu)
	}
	if fri.Windows != "" || fri.Where != "Ленинградский · 0611" {
		t.Errorf("пятница без окон, одна аудитория: %+v", fri)
	}
	if !strings.Contains(string(thu.Href), "date=2026-09-24") {
		t.Errorf("день ведёт на расписание дня: %s", thu.Href)
	}
}
