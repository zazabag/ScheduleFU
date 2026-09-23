package clock

import (
	"testing"
	"time"
)

func TestRusskieDaty(t *testing.T) {
	d := time.Date(2026, 9, 14, 15, 4, 0, 0, time.UTC)
	cases := map[string]string{
		DateRu(d): "14 сентября", DateTimeRu(d): "14 сентября в 15:04",
		WeekdayRu(d): "понедельник", WeekdayShortRu(d): "пн",
		// То, из-за чего таблица появилась: time.Format печатал «января» всегда.
		DateRu(time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)):   "3 января",
		DateRu(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)): "31 декабря",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("получено %q, ожидалось %q", got, want)
		}
	}
}

func TestFixedIStartOfWeek(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	c := Fixed(time.Date(2026, 9, 17, 12, 30, 0, 0, msk)) // четверг
	if c.HHMM() != "12:30" {
		t.Errorf("HHMM = %q", c.HHMM())
	}
	if got := StartOfWeek(c.Now()); got.Day() != 14 || got.Weekday() != time.Monday {
		t.Errorf("начало недели = %s", got)
	}
	if c.Today().Hour() != 0 {
		t.Error("Today должен быть началом дня")
	}
}

// Учебный год по закону начинается 1 сентября: неделя, в которую оно
// попало, — первая. Летом учебных недель нет.
func TestUchebnayaNedelya(t *testing.T) {
	d := func(y int, m time.Month, day int) time.Time { return time.Date(y, m, day, 12, 0, 0, 0, time.UTC) }
	cases := []struct {
		at   time.Time
		want int
	}{
		{d(2026, 9, 1), 1},  // вторник, 1 сентября
		{d(2026, 8, 31), 0}, // понедельник той же недели — ещё лето
		{d(2026, 9, 6), 1},  // воскресенье первой недели
		{d(2026, 9, 7), 2},  // понедельник второй
		{d(2026, 9, 24), 4},
		{d(2027, 2, 10), 24}, // весна — тот же учебный год
		{d(2027, 7, 15), 0},  // лето
	}
	for _, c := range cases {
		if got := StudyWeek(c.at); got != c.want {
			t.Errorf("%s: неделя %d, ожидалась %d", c.at.Format("02.01.2006"), got, c.want)
		}
	}
}
