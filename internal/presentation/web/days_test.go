package web

import (
	"testing"
	"time"

	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

func ref(day int, begins string) ndom.LessonRef {
	return ndom.LessonRef{SubjectKey: "group:УПП26-2", Discipline: "Менеджмент",
		Date: time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC), BeginsAt: begins}
}

// Раздел предмета устроен по парам: студент вспоминает «что было во
// вторник», а не «где у меня конспекты». В ленте — только пары, по которым
// что-то есть, свежие сверху, и по строке видно, что внутри.
func TestLentaParSkleivaetVsyoPoDatam(t *testing.T) {
	now := time.Now()
	saved := []ndom.Note{{ID: 1, Lesson: ref(22, "10:10"), Title: "Реформы", SavedAt: &now}}
	drafts := []ndom.Note{{ID: 2, Lesson: ref(29, ""), Title: "Падл"}}
	hws := []ndom.Homework{
		{ID: 1, Lesson: ref(22, "10:10"), Body: "глава 3", SavedAt: &now},
		{ID: 2, Lesson: ref(22, "10:10"), Body: "эссе", SavedAt: &now, DoneAt: &now},
		{ID: 3, Lesson: ref(25, "12:00"), Body: "вписано", SavedAt: &now, Origin: "manual"},
	}
	recs := []ndom.Recording{
		{ID: 10, Lesson: ref(22, "10:10"), Status: ndom.StatusReady}, // живёт конспектом
		{ID: 11, Lesson: ref(23, "10:10"), Status: ndom.StatusTranscribing},
		{ID: 12, Lesson: ref(24, "08:30"), Status: ndom.StatusFailed},
	}
	days := buildDays(saved, drafts, hws, recs, "/lessons?group=x&d=m")

	var keys []string
	for _, d := range days {
		keys = append(keys, d.Key)
	}
	want := []string{"2026-09-29T", "2026-09-25T12:00", "2026-09-24T08:30", "2026-09-23T10:10", "2026-09-22T10:10"}
	if len(keys) != len(want) {
		t.Fatalf("пары: %q", keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("порядок пар: %q, ожидалось %q", keys, want)
		}
	}
	byKey := map[string]dayRow{}
	for _, d := range days {
		byKey[d.Key] = d
	}
	if d := byKey["2026-09-22T10:10"]; !d.Note || d.Draft || d.Homework != 2 || d.State != "" {
		t.Errorf("22-е: %+v", d)
	}
	if d := byKey["2026-09-29T"]; !d.Draft || d.Note {
		t.Errorf("черновик: %+v", d)
	}
	if d := byKey["2026-09-23T10:10"]; d.State == "" || d.StateClass != "transcribing" {
		t.Errorf("в обработке: %+v", d)
	}
	if d := byKey["2026-09-24T08:30"]; d.StateClass != "failed" {
		t.Errorf("ошибка: %+v", d)
	}
	if d := byKey["2026-09-22T10:10"]; string(d.Href) != "/lessons?group=x&d=m&day=2026-09-22T10%3A10" {
		t.Errorf("ссылка: %s", d.Href)
	}
}

// «Не сделано» — задания всех пар, которые ещё не выполнены; ближайшие к
// сроку, то есть более ранние пары, сверху.
func TestNeSdelanoTolkoNevypolnennye(t *testing.T) {
	now := time.Now()
	hws := []ndom.Homework{
		{ID: 1, Lesson: ref(25, "12:00"), Body: "позднее", SavedAt: &now},
		{ID: 2, Lesson: ref(22, "10:10"), Body: "сделано", SavedAt: &now, DoneAt: &now},
		{ID: 3, Lesson: ref(22, "10:10"), Body: "раннее", SavedAt: &now},
	}
	got := pendingHomework(hws, "/lessons?group=x&d=m")
	if len(got) != 2 || got[0].Body != "раннее" || got[1].Body != "позднее" {
		t.Fatalf("не сделано: %+v", got)
	}
	if string(got[0].DayHref) != "/lessons?group=x&d=m&day=2026-09-22T10%3A10" {
		t.Errorf("ссылка на пару: %s", got[0].DayHref)
	}
}
