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
	base := template.URL("/lessons?group=x&d=%D0%98")
	note := noteView{ID: 1, Date: "22 сентября", Title: "Реформы Петра",
		Blocks: parseNoteBody("## Армия\nтекст\n\n- рекруты"), Theses: []string{"тезис"},
		AskHW: true, SaveBack: base + "&day=2026-09-22T10%3A10", DeleteBack: base}
	one := func() map[string]any {
		return map[string]any{
			"Name": "История", "Lecturer": "Иванов И.И.", "Kinds": "лекция", "Rooms": "313",
			"Soon": "сегодня в 10:10", "Today": "2026-09-22", "Base": base,
			"Options": []lessonOption{{Value: "2026-09-22T10:10", Label: "вт, 22 сентября, 10:10", On: true}},
			"Days": []dayRow{{Key: "2026-09-22T10:10", Label: "вт, 22 сентября · 10:10", Href: base + "&day=2026-09-22T10%3A10",
				Draft: true, Homework: 1}},
			"Pending": []homeworkView{{ID: 2, Body: "глава 3", Due: "к четвергу", DayLabel: "вт, 22 сентября · 10:10",
				DayHref: base + "&day=2026-09-22T10%3A10"}},
		}
	}
	page := func(extra map[string]any) string {
		t.Helper()
		data := map[string]any{"Title": "История", "Tab": "lessons", "Look": lookFrom(r), "Onboard": onboardState{},
			"Group": "ПИ24-1", "SubjectQuery": template.URL("group=x"), "CanRecord": true, "One": one()}
		for k, v := range extra {
			data[k] = v
		}
		var out strings.Builder
		if err := s.pages["lessons"].ExecuteTemplate(&out, "base", data); err != nil {
			t.Fatalf("отрисовка: %v", err)
		}
		return out.String()
	}
	has := func(screen, html string, want ...string) {
		t.Helper()
		for _, w := range want {
			if !strings.Contains(html, w) {
				t.Errorf("%s: нет %q", screen, w)
			}
		}
	}

	// Экран предмета: запись, «Не сделано», лента пар.
	has("предмет", page(nil), "Иванов И.И.", "record.js", "Не сделано", "глава 3",
		"вт, 22 сентября · 10:10", "не сохранён", "1 дз", `href="/lessons?group=x&amp;d=%D0%98&amp;day=2026-09-22T10%3A10"`)

	// Экран записи: готовый конспект с вопросом про задание.
	has("запись", page(map[string]any{
		"Rec":     recordingView{ID: 3, Date: "22 сентября", Status: "ready", Label: "готово", Ready: true},
		"RecNote": note,
	}), "Реформы Петра", "Сохранить конспект", "что-то задали", `name="hw"`)

	// Экран пары: конспект, задания пары, форма с этой парой.
	saved := note
	saved.Saved, saved.AskHW = true, false
	day := page(map[string]any{"Day": dayView{Key: "2026-09-22T10:10", Label: "вт, 22 сентября · 10:10",
		Notes: []noteView{saved}, Homeworks: []homeworkView{{ID: 2, Body: "глава 3", Saved: true}},
		Back: base + "&day=2026-09-22T10%3A10"}})
	has("пара", day, "Реформы Петра", "✓ сохранён", "глава 3", "Задали на дом", `value="2026-09-22T10:10"`)
	if strings.Contains(day, `name="hw"`) {
		t.Error("пара: сохранённый конспект снова спрашивает про задание")
	}

	// Список предметов — второй режим того же шаблона.
	list := map[string]any{"Title": "Пары", "Tab": "lessons", "Look": lookFrom(r), "Onboard": onboardState{},
		"Group": "ПИ24-1", "SubjectQuery": template.URL("group=x"),
		"Subjects": []*subjectCard{{Name: "История", Href: "/lessons?group=x&d=%D0%98", Notes: 2, Homework: 1, Soon: "сегодня в 10:10"}}}
	var out strings.Builder
	if err := s.pages["lessons"].ExecuteTemplate(&out, "base", list); err != nil {
		t.Fatalf("отрисовка списка: %v", err)
	}
	if !strings.Contains(out.String(), "2 дз") && !strings.Contains(out.String(), "1 дз") {
		t.Error("в списке не видно домашних заданий")
	}
}
