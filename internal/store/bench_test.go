package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// makeSnapshot строит слепок, близкий к настоящему: около одиннадцати
// тысяч пар за неделю — столько отдаёт источник по всему вузу.
func makeSnapshot(n int) []Lesson {
	out := make([]Lesson, 0, n)
	for i := 0; i < n; i++ {
		aud := int64(2000 + i%500)
		lec := int64(40000 + i%1500)
		out = append(out, Lesson{
			LessonOid:     int64(100000 + i),
			Date:          day(7 + i%6),
			BeginsAt:      "08:30",
			EndsAt:        "10:00",
			AuditoriumOid: &aud,
			Auditorium:    fmt.Sprintf("ЛП49/2/%d", 300+i%99),
			Building:      "Ленинградский проспект, 49/2",
			Discipline:    "Дисциплина",
			KindOfWork:    "Семинар",
			LecturerOid:   &lec,
			LecturerName:  "Преподаватель",
			Stream:        "ПИ24-1; ПИ24-2",
			GroupNames:    []string{"ПИ24-1", "ПИ24-2"},
		})
	}
	return out
}

// TestSkorostPovtornogoProhoda замеряет самый частый случай: сборщик
// принёс тот же слепок, что и в прошлый раз. Именно так проходит
// большинство обновлений, и именно он должен быть дешёвым.
func TestSkorostPovtornogoProhoda(t *testing.T) {
	if testing.Short() {
		t.Skip("замер пропускается в коротком режиме")
	}
	s := testStore(t)
	ctx := context.Background()
	snapshot := makeSnapshot(11000)

	start := time.Now()
	if _, err := s.ApplySnapshot(ctx, day(7), day(13), snapshot); err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)

	start = time.Now()
	res, err := s.ApplySnapshot(ctx, day(7), day(13), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	second := time.Since(start)

	if res.Changes() != 0 {
		t.Fatalf("повторный слепок дал изменения: %+v", res)
	}
	t.Logf("первый проход (всё новое): %s", first.Round(time.Millisecond))
	t.Logf("повторный проход (без изменений): %s", second.Round(time.Millisecond))
}
