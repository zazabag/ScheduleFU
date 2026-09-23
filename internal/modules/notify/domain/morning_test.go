package domain

import (
	"strings"
	"testing"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

func TestUtrennyayaSvodka(t *testing.T) {
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	mk := func(from, to, disc, aud string) sched.Lesson {
		return sched.Lesson{Date: day, BeginsAt: from, EndsAt: to, Discipline: disc, Auditorium: aud, Building: "Ленинградский проспект, 51, корп. 1"}
	}
	n, ok := BuildMorning([]sched.Lesson{
		mk("14:00", "15:30", "История", "ЛП51_1/0326"),
		mk("10:10", "11:40", "Финансы", "ЛП51_1/0412"),
		mk("10:10", "11:40", "Финансы", "ЛП51_1/0413"), // вторая подгруппа — та же пара
	}, func(string) string { return "Ленинградский" }, "/schedule?group=x")
	if !ok || n.Title != "Сегодня 2 пары" {
		t.Fatalf("сводка: %+v", n)
	}
	if !strings.HasPrefix(n.Body, "Первая в 10:10 — Финансы, 0412 · Ленинградский") || !strings.Contains(n.Body, "Последняя до 15:30") {
		t.Errorf("текст: %q", n.Body)
	}
	if _, ok := BuildMorning(nil, func(string) string { return "" }, ""); ok {
		t.Error("без пар сводки нет")
	}
}
