package domain

import (
	"strings"
	"testing"
	"time"
)

func hw(owner, disc, body string, due *time.Time) Homework {
	return Homework{OwnerKey: owner, Lesson: LessonRef{Discipline: disc}, Body: body, DueDate: due}
}

func TestZadanieKZavtrashneyPare(t *testing.T) {
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	other := day.AddDate(0, 0, 2)
	if !hw("a", "Финансы", "задача", &day).DueOn(day, false) {
		t.Error("срок датой на завтра — напоминаем, даже если пары нет")
	}
	if hw("a", "Финансы", "задача", &other).DueOn(day, true) {
		t.Error("срок датой на послезавтра — завтра не напоминаем, хоть пара и есть")
	}
	if !hw("a", "Финансы", "задача", nil).DueOn(day, true) || hw("a", "Финансы", "задача", nil).DueOn(day, false) {
		t.Error("без даты — к ближайшей паре предмета")
	}
}

func TestOdnoPismoNaUstroystvo(t *testing.T) {
	rs := BuildReminders([]Homework{
		hw("a", "История", "эссе", nil), hw("a", "Финансы", "задача 5", nil), hw("a", "Финансы", "параграф 3", nil),
		hw("b", "Право", "кейс", nil),
	})
	if len(rs) != 2 {
		t.Fatalf("писем %d", len(rs))
	}
	if rs[0].Title != "Завтра: не сделано 3 задания к 2 парам" || !strings.HasPrefix(rs[0].Body, "История — эссе; Финансы — задача 5") {
		t.Errorf("письмо: %+v", rs[0])
	}
	if rs[1].Title != "Завтра: не сделано задание к паре «Право»" {
		t.Errorf("одно задание: %q", rs[1].Title)
	}
}

func TestDlinnyySpisokObrezaetsyaPoSlovu(t *testing.T) {
	var many []Homework
	for i := 0; i < 20; i++ {
		many = append(many, hw("a", "Финансы", "прочитать главу про дюрацию облигаций", nil))
	}
	b := BuildReminders(many)[0].Body
	if len([]rune(b)) > reminderBody || !strings.HasSuffix(b, "…") {
		t.Errorf("тело %d символов: %q", len([]rune(b)), b)
	}
}
