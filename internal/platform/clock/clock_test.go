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
