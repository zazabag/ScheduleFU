package domain

import (
	"testing"
	"time"
)

func TestDlitelnostPoRusski(t *testing.T) {
	cases := map[int]string{0: "", 59: "1 мин", 720: "12 мин", 3600: "1 ч", 5520: "1 ч 32 мин", 5400: "1 ч 30 мин"}
	for sec, want := range cases {
		if got := HumanDuration(sec); got != want {
			t.Errorf("%d секунд: %q, ожидалось %q", sec, got, want)
		}
	}
}

func TestZapisBezPredmetaNeProhodit(t *testing.T) {
	ref := LessonRef{SubjectKey: "group:ПИ24-1", Date: time.Now()}
	if err := ref.Validate(); err == nil {
		t.Fatal("запись без предмета принята, а её не к чему привязать")
	}
	ref.Discipline = "История"
	if err := ref.Validate(); err != nil {
		t.Fatalf("полная запись отвергнута: %v", err)
	}
}

func TestPustyeKuskiOtvetaModeliVybrasyvayutsya(t *testing.T) {
	r := Recap{
		Title:    "  Тема занятия  ",
		Body:     "\n\nтекст\n",
		Theses:   []string{"первый", "   ", ""},
		Homework: []RecapHomework{{Text: "  прочитать главу 3  ", DueNote: " к четвергу "}, {Text: "   "}},
	}.Clean()

	if r.Title != "Тема занятия" || r.Body != "текст" {
		t.Errorf("заголовок %q, тело %q", r.Title, r.Body)
	}
	if len(r.Theses) != 1 || r.Theses[0] != "первый" {
		t.Errorf("тезисы: %q", r.Theses)
	}
	if len(r.Homework) != 1 || r.Homework[0].Text != "прочитать главу 3" || r.Homework[0].DueNote != "к четвергу" {
		t.Errorf("задания: %+v", r.Homework)
	}
}

// Заголовок в сто двадцать с лишним букв ломает строку карточки, а модель
// регулярно отвечает на него целым предложением.
func TestDlinnyyZagolovokObrezaetsyaPoBukvam(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "я"
	}
	got := []rune(Recap{Title: long, Body: "текст"}.Clean().Title)
	if len(got) != 121 || got[120] != '…' {
		t.Fatalf("длина %d, последний знак %q", len(got), string(got[len(got)-1]))
	}
}

func TestRasshifrovkaSkleivaetsyaBezPustyh(t *testing.T) {
	got := Transcript([]Segment{{Text: "первое "}, {Text: "  "}, {Text: "второе"}})
	if got != "первое второе" {
		t.Errorf("склейка дала %q", got)
	}
}

func TestSostoyaniyaObrabotki(t *testing.T) {
	if !StatusTranscribing.Working() || StatusReady.Working() {
		t.Error("в работе должны быть только промежуточные состояния")
	}
	if !StatusFailed.Done() || StatusQueued.Done() {
		t.Error("законченные — только готово и неудача")
	}
}
