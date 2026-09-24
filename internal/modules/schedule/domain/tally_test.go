package domain

import (
	"testing"
	"time"
)

func TestSemestrPoDate(t *testing.T) {
	d := func(y int, m time.Month) time.Time { return time.Date(y, m, 10, 0, 0, 0, 0, time.UTC) }
	for _, c := range []struct {
		at   time.Time
		want string
	}{{d(2026, 9), "2026-1"}, {d(2026, 12), "2026-1"}, {d(2027, 1), "2026-1"}, {d(2027, 2), "2026-2"}, {d(2027, 6), "2026-2"}} {
		if got := SemesterOf(c.at); got != c.want {
			t.Errorf("%s: %s, ожидался %s", c.at.Format("01.2006"), got, c.want)
		}
	}
}

func TestItogDnyaPodgruppyOdnaPara(t *testing.T) {
	mk := func(key, from, to, disc, aud string) Attributed {
		return Attributed{SubjectKey: key, Lesson: Lesson{BeginsAt: from, EndsAt: to, Discipline: disc, Auditorium: aud, Building: "ЛП51"}}
	}
	day := TallyDay([]Attributed{
		mk("group:ПИ24-1", "10:10", "11:40", "Английский", "0412"),
		mk("group:ПИ24-1", "10:10", "11:40", "Английский", "0413"), // вторая подгруппа — та же пара
		mk("group:ПИ24-1", "11:50", "13:20", "Финансы", "0412"),
		mk("lecturer:5", "11:50", "13:20", "Финансы", "0412"),
	})
	g := day["group:ПИ24-1"]
	if g.Lessons != 2 || g.Minutes != 180 || g.Days != 1 || g.Disciplines["Английский"] != 90 || g.Rooms["0412"] != 2 {
		t.Errorf("группа за день: %+v", g)
	}
	if day["lecturer:5"].Lessons != 1 {
		t.Errorf("преподаватель: %+v", day["lecturer:5"])
	}
	sum := g.Add(g)
	if sum.Lessons != 4 || sum.Days != 2 || sum.Disciplines["Финансы"] != 180 || sum.Buildings["ЛП51"] != 4 {
		t.Errorf("сумма двух дней: %+v", sum)
	}
}
