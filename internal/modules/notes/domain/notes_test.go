package domain

import (
	"strings"
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

// Айфон выключает микрофон свёрнутому приложению. Пропуск должен стоять в
// расшифровке там, где он случился, иначе модель склеит «до» и «после» в
// одну мысль и досочинит связку, которой на паре не было.
func TestPropuskVstaetVRasshifrovkuNaSvoyoMesto(t *testing.T) {
	segs := []Segment{
		{Start: 0, Text: "реформы Петра"},
		{Start: 60 * time.Second, Text: "итак табель о рангах"},
		{Start: 20 * time.Minute, Text: "в итоге"},
	}
	got := TranscriptWithGaps(segs, []Gap{{AtSec: 30, DurSec: 240}})
	want := "реформы Петра [пропуск в записи ~4 мин] итак табель о рангах в итоге"
	if got != want {
		t.Errorf("расшифровка:\n%q\nожидалось\n%q", got, want)
	}
	// Пропуск после последней фразы — в конце, а не потерян.
	if got := TranscriptWithGaps(segs, []Gap{{AtSec: 3600, DurSec: 60}}); !strings.HasSuffix(got, "[пропуск в записи ~1 мин]") {
		t.Errorf("пропуск в конце: %q", got)
	}
	if TranscriptWithGaps(segs, nil) != Transcript(segs) {
		t.Error("без пропусков расшифровка должна совпадать с обычной")
	}
}

// Пропуски приходят из браузера — это недоверенные данные: отрицательные,
// крошечные и бесконечные списки отбрасываются, порядок наводится.
func TestPropuskiIzBrauzeraChistyatsya(t *testing.T) {
	in := []Gap{{AtSec: 600, DurSec: 120}, {AtSec: -5, DurSec: 60}, {AtSec: 10, DurSec: 2}, {AtSec: 100, DurSec: 30}}
	got := CleanGaps(in)
	if len(got) != 2 || got[0].AtSec != 100 || got[1].AtSec != 600 {
		t.Errorf("пропуски: %+v", got)
	}
	many := make([]Gap, 500)
	for i := range many {
		many[i] = Gap{AtSec: i * 100, DurSec: 10}
	}
	if n := len(CleanGaps(many)); n != MaxGaps {
		t.Errorf("пропусков после чистки: %d", n)
	}
}

func TestPropuskPoRusski(t *testing.T) {
	g := Gap{AtSec: 23*60 + 10, DurSec: 250}
	if got := g.Label(); got != "на 23-й минуте — около 4 мин" {
		t.Errorf("подпись: %q", got)
	}
	if got := (Gap{AtSec: 20, DurSec: 30}).Label(); got != "в самом начале — около 1 мин" {
		t.Errorf("подпись в начале: %q", got)
	}
}

// Коды отказов bigmodel.cn переводятся на человеческий: в чат бота уходит
// не «1113», а что делать.
func TestKodyOtkazaNeyroseti(t *testing.T) {
	cases := map[string]string{
		"1113": "баланс", "1302": "частот", "1303": "частот", "1304": "дневной лимит",
		"1305": "частот", "1002": "ключ", "1211": "модел", "1301": "фильтр",
	}
	for code, want := range cases {
		if got := LLMHint(code); !strings.Contains(got, want) {
			t.Errorf("%s: %q, ожидалось со словом %q", code, got, want)
		}
	}
	if LLMHint("9999") != "" {
		t.Error("неизвестный код получил выдуманное объяснение")
	}
}
