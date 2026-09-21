package ruz

import (
	"context"
	"os"
	"testing"
	"time"
)

// Живые тесты ходят в ruz.fa.ru и по умолчанию пропускаются, чтобы обычный
// прогон не зависел от чужого сервера и не создавал ему нагрузку.
// Запуск: RUZ_LIVE=1 go test ./internal/ruz/ -run Live -v
func liveOrSkip(t *testing.T) *Client {
	t.Helper()
	if os.Getenv("RUZ_LIVE") != "1" {
		t.Skip("живой тест: задайте RUZ_LIVE=1")
	}
	return New(Options{RPS: 4})
}

func TestLiveSearchIRaspisanieGruppy(t *testing.T) {
	c := liveOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	found, err := c.Search(ctx, SearchGroup, "ПИ24")
	if err != nil {
		t.Fatalf("поиск группы: %v", err)
	}
	if len(found) == 0 {
		t.Fatal("поиск группы ничего не вернул")
	}
	t.Logf("найдено групп: %d, первая: %s", len(found), found[0].Label)
}

func TestLiveKorotkayaStrokaOtsekaetsyaDoZaprosa(t *testing.T) {
	c := New(Options{})
	_, err := c.Search(context.Background(), SearchGroup, "ПИ")
	if err != ErrShortTerm {
		t.Fatalf("ожидалась ErrShortTerm, получено: %v", err)
	}
}

// TestLiveLecturerOidProtivGUID закрепляет ловушку источника: числовой
// lecturerOid даёт расписание одного преподавателя, а GUID — сотни чужих
// пар с пустым полем Lecturer, причём без всякой ошибки.
func TestLiveLecturerOidProtivGUID(t *testing.T) {
	c := liveOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 6)

	lessons, err := c.Schedule(ctx, KindLecturer, 46674, from, to)
	if err != nil {
		t.Fatalf("расписание преподавателя: %v", err)
	}
	if len(lessons) == 0 {
		t.Skip("у преподавателя нет пар в этот период")
	}
	for _, l := range lessons {
		if l.Lecturer == "" {
			t.Fatalf("пустое поле Lecturer при запросе по oid — признак того, "+
				"что источник отдал чужие пары (получено %d пар)", len(lessons))
		}
	}
	t.Logf("по lecturerOid=46674 получено %d пар, все с заполненным преподавателем", len(lessons))
}

func TestLiveRaspisanieAuditorii(t *testing.T) {
	c := liveOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	lessons, err := c.Schedule(ctx, KindAuditorium, 2851, from, from.AddDate(0, 0, 6))
	if err != nil {
		t.Fatalf("расписание аудитории: %v", err)
	}
	t.Logf("аудитория 2851: %d пар за неделю", len(lessons))
	if len(lessons) > 0 {
		l := lessons[0]
		if l.LessonOid == 0 {
			t.Error("LessonOid пуст — по нему строится поиск изменений")
		}
		a := ParseAuditorium(l.Auditorium, l.Building)
		t.Logf("первая пара: %s %s-%s %q в %q (кампус %s, вместимость %d)",
			l.Date, l.BeginLesson, l.EndLesson, l.Discipline, a.Room, a.Campus, l.AuditoriumAmount)
	}
}

func TestLiveNekorrektnyyIdentifikator(t *testing.T) {
	c := New(Options{})
	_, err := c.Schedule(context.Background(), KindLecturer, 0, time.Now(), time.Now())
	if err == nil {
		t.Fatal("нулевой идентификатор должен отвергаться до запроса")
	}
}
