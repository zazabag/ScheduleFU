package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Конспект приходит от модели, то есть снаружи. Разметку разбирает сервер
// в три вида блоков, и шаблон печатает их текстом: пускать ответ модели в
// страницу как HTML — то же самое, что доверять источнику.
func TestRazmetkaKonspektaRazbiraetsyaVBloki(t *testing.T) {
	body := "## Реформы\nПётр начал с армии.\nЗатем флот.\n\n- рекрутская повинность\n- Табель о рангах\n\nИтог."
	blocks := parseNoteBody(body)

	if len(blocks) != 4 {
		t.Fatalf("блоков %d вместо четырёх: %+v", len(blocks), blocks)
	}
	if blocks[0].Kind != "head" || blocks[0].Text != "Реформы" {
		t.Errorf("подзаголовок: %+v", blocks[0])
	}
	// Соседние строки — один абзац: перенос внутри абзаца модель ставит где
	// придётся, и разрывать по нему значит рвать предложение пополам.
	if blocks[1].Kind != "text" || blocks[1].Text != "Пётр начал с армии. Затем флот." {
		t.Errorf("абзац: %+v", blocks[1])
	}
	if blocks[2].Kind != "list" || len(blocks[2].Items) != 2 || blocks[2].Items[1] != "Табель о рангах" {
		t.Errorf("список: %+v", blocks[2])
	}
	if blocks[3].Text != "Итог." {
		t.Errorf("последний абзац: %+v", blocks[3])
	}
}

func TestRazmetkaPonimaetTireIZvyozdochku(t *testing.T) {
	blocks := parseNoteBody("* звёздочка\n— тире\n- дефис")
	if len(blocks) != 1 || blocks[0].Kind != "list" || len(blocks[0].Items) != 3 {
		t.Fatalf("три вида маркеров дали: %+v", blocks)
	}
	if blocks[0].Items[1] != "тире" {
		t.Errorf("маркер не срезан: %q", blocks[0].Items[1])
	}
}

// По умолчанию предлагается идущая пара, а не первая в списке: записывают
// почти всегда ту, что идёт прямо сейчас.
func TestPoUmolchaniyuVybiraetsyaIdushchayaPara(t *testing.T) {
	opts := []lessonOption{
		{Value: "2026-09-21T10:10"},
		{Value: "2026-09-22T08:30"},
		{Value: "2026-09-22T14:00"},
		{Value: "2026-09-24T10:10"},
	}
	if got := defaultOption(opts, "2026-09-22", "09:00"); got != "2026-09-22T08:30" {
		t.Errorf("в 09:00 выбрано %q, а идёт пара 08:30", got)
	}
	if got := defaultOption(opts, "2026-09-23", "09:00"); got != "2026-09-24T10:10" {
		t.Errorf("в день без пар выбрано %q, а ближайшая — 24-го", got)
	}
	// Все пары позади: предлагаем последнюю, а не пустоту — конспект часто
	// заливают вечером того же дня.
	if got := defaultOption(opts, "2026-09-30", "09:00"); got != "2026-09-24T10:10" {
		t.Errorf("после всех пар выбрано %q", got)
	}
	if got := defaultOption(nil, "2026-09-22", "09:00"); got != "" {
		t.Errorf("без пар выбрано %q", got)
	}
}

func TestPodpisIzMnozhestvaObrezaetsya(t *testing.T) {
	set := map[string]bool{"Иванов": true, "Петров": true, "Сидоров": true, "Кузнецов": true}
	got := joinSet(set, 2)
	if got != "Иванов · Кузнецов и ещё 2" {
		t.Errorf("подпись: %q", got)
	}
	if joinSet(nil, 3) != "" {
		t.Error("пустое множество дало непустую подпись")
	}
}

// Шаблон раздела ломается молча: ошибка в нём видна только при запуске
// сервера, а на экране — пустая страница. Тест поднимает разбор всех
// шаблонов и рисует раздел на правдоподобных данных.
func TestShablonRazdelaParyRisuetsya(t *testing.T) {
	s, err := New(Deps{})
	if err != nil {
		t.Fatalf("шаблоны не разобрались: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/lessons", nil)
	note := noteView{ID: 1, Date: "22 сентября", Title: "Реформы Петра",
		Blocks: parseNoteBody("## Армия\nтекст\n\n- рекруты"), Theses: []string{"тезис"},
		Homeworks: []homeworkView{{ID: 2, Body: "глава 3", Due: "к четвергу"}}}
	data := map[string]any{
		"Title": "История", "Tab": "lessons", "Look": lookFrom(r), "Onboard": onboardState{},
		"Group": "ПИ24-1", "SubjectQuery": template.URL("group=%D0%9F%D0%98241"), "CanRecord": true,
		"Rec":     recordingView{ID: 3, Date: "22 сентября", Status: "transcribing", Label: "расшифровываем", Working: true},
		"RecNote": note,
		"One": map[string]any{
			"Name": "История", "Lecturer": "Иванов И.И.", "Kinds": "лекция", "Rooms": "313",
			"Soon": "сегодня в 10:10", "Today": "2026-09-22", "Base": template.URL("/lessons?group=x&d=%D0%98"),
			"Options":    []lessonOption{{Value: "2026-09-22T10:10", Label: "вт, 22 сентября, 10:10", On: true}},
			"Notes":      []noteView{note},
			"Homeworks":  []homeworkView{{ID: 2, Body: "глава 3", Due: "к четвергу", Date: "22 сентября", Saved: true}},
			"Recordings": []recordingView{{ID: 3, Date: "22 сентября", Status: "queued", Label: "в очереди"}},
		},
	}
	var out strings.Builder
	if err := s.pages["lessons"].ExecuteTemplate(&out, "base", data); err != nil {
		t.Fatalf("отрисовка предмета: %v", err)
	}
	for _, want := range []string{"История", "Иванов И.И.", "Сохранить конспект", "глава 3", "Реформы Петра", "record.js"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("на странице нет %q", want)
		}
	}

	// Список предметов — второй режим того же шаблона.
	list := map[string]any{"Title": "Пары", "Tab": "lessons", "Look": lookFrom(r), "Onboard": onboardState{},
		"Group": "ПИ24-1", "SubjectQuery": template.URL("group=x"),
		"Subjects": []*subjectCard{{Name: "История", Href: "/lessons?group=x&d=%D0%98", Notes: 2, Homework: 1, Soon: "сегодня в 10:10"}}}
	out.Reset()
	if err := s.pages["lessons"].ExecuteTemplate(&out, "base", list); err != nil {
		t.Fatalf("отрисовка списка: %v", err)
	}
	if !strings.Contains(out.String(), "2 дз") && !strings.Contains(out.String(), "1 дз") {
		t.Error("в списке не видно домашних заданий")
	}
}
