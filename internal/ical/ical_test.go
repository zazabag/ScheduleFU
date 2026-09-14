package ical

import (
	"strings"
	"testing"
	"time"
)

var msk = time.FixedZone("MSK", 3*60*60)

func sample() Calendar {
	return Calendar{
		Name:     "Расписание Ю24-5",
		Location: msk,
		Events: []Event{{
			UID:         "lesson-1@schedulefu",
			Start:       time.Date(2026, 9, 15, 8, 30, 0, 0, msk),
			End:         time.Date(2026, 9, 15, 10, 0, 0, 0, msk),
			Summary:     "Философия",
			Location:    "ЛП49/2/313",
			Description: "Семинар · Замараева Е.И.",
		}},
	}
}

func TestRenderStrukturaKalendarya(t *testing.T) {
	out := sample().Render(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"BEGIN:VCALENDAR", "END:VCALENDAR",
		"BEGIN:VEVENT", "END:VEVENT",
		"VERSION:2.0", "UID:lesson-1@schedulefu",
		"DTSTAMP:20260914T120000Z",
		"SUMMARY:Философия",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в календаре нет %q", want)
		}
	}

	// Время выгружается с поясом, а не в UTC: иначе при переезде владельца
	// пары уезжают на несколько часов.
	if !strings.Contains(out, "DTSTART;TZID=MSK:20260915T083000") {
		t.Error("время начала выгружено без часового пояса")
	}
}

// TestRenderPerevodStrok: формат требует CRLF, обычный перевод строки
// часть календарных приложений молча отвергает.
func TestRenderPerevodStrok(t *testing.T) {
	out := sample().Render(time.Now())
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Error("в календаре есть перевод строки без возврата каретки")
	}
}

// TestEscapeSpecsimvolov закрепляет главную ловушку формата:
// незаэкранированная запятая превращает поле в список, и событие ломается.
func TestEscapeSpecsimvolov(t *testing.T) {
	cases := map[string]string{
		"Экономика, ч. 2": `Экономика\, ч. 2`,
		"Право; практика": `Право\; практика`,
		"путь\\к":         `путь\\к`,
		"первая\nвторая":  `первая\nвторая`,
	}
	for in, want := range cases {
		if got := escape(in); got != want {
			t.Errorf("escape(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

// TestFoldNeRvyotBukvy — русский текст в UTF-8 занимает по два байта на
// букву, а складывать строку положено по октетам. Разрыв посреди буквы
// превратил бы её в мусор.
func TestFoldNeRvyotBukvy(t *testing.T) {
	long := "SUMMARY:" + strings.Repeat("Философия и методология науки ", 5)
	folded := fold(long)

	// Склеиваем обратно так же, как это делает читатель календаря.
	unfolded := strings.ReplaceAll(folded, crlf+" ", "")
	if unfolded != long {
		t.Fatal("после складывания и обратной склейки текст изменился")
	}
	for _, line := range strings.Split(folded, crlf) {
		if len(line) > 75 {
			t.Errorf("строка длиной %d октетов превышает предел", len(line))
		}
	}
	if strings.Contains(folded, "�") {
		t.Error("в тексте появился испорченный символ")
	}
}

func TestRenderPustoyKalendar(t *testing.T) {
	c := Calendar{Name: "Пусто", Location: msk}
	out := c.Render(time.Now())
	if !strings.Contains(out, "BEGIN:VCALENDAR") || !strings.Contains(out, "END:VCALENDAR") {
		t.Error("пустой календарь должен оставаться корректным")
	}
	if strings.Contains(out, "BEGIN:VEVENT") {
		t.Error("в пустом календаре не должно быть событий")
	}
}
