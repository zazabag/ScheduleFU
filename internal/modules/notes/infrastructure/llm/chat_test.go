package llm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Просить JSON и получать JSON — разные вещи: модель регулярно добавляет
// вежливое вступление и обрамление кодом.
func TestOtvetModeliRazbiraetsyaSObramleniem(t *testing.T) {
	answer := "Конечно, вот конспект:\n```json\n" + `{
	  "title": "Реформы Петра I",
	  "summary": "## Начало\nтекст",
	  "theses": ["первый тезис"],
	  "homework": [{"text": "прочитать главу 3", "due": "к четвергу"}]
	}` + "\n```\nГотово!"

	r, err := parseRecap(answer)
	if err != nil {
		t.Fatalf("не разобрано: %v", err)
	}
	if r.Title != "Реформы Петра I" || !strings.Contains(r.Body, "## Начало") {
		t.Errorf("конспект: %+v", r)
	}
	if len(r.Homework) != 1 || r.Homework[0].DueNote != "к четвергу" {
		t.Errorf("задание: %+v", r.Homework)
	}
}

func TestNeJSONVsyoTakiOshibka(t *testing.T) {
	if _, err := parseRecap("Извините, я не смог обработать этот текст."); err == nil {
		t.Fatal("отговорка модели принята за конспект")
	}
}

// Длинную расшифровку режем по байтам, а буквы русского в UTF-8 занимают
// по два: разрез посреди буквы портит обе половины.
func TestDlinnyyTekstRezhetsyaPoBukvam(t *testing.T) {
	text := strings.Repeat("это предложение о Петре. ", 400)
	parts := split(text, 501)
	if len(parts) < 2 {
		t.Fatalf("текст не разрезан: %d частей", len(parts))
	}
	joined := 0
	for _, p := range parts {
		if !utf8.ValidString(p) {
			t.Fatalf("часть порезана посреди буквы: %q", p[:20])
		}
		joined += len([]rune(p))
	}
	// Склейка теряет только пробелы на стыках — текст не должен пропадать.
	if joined < len([]rune(text))-len(parts) {
		t.Errorf("при нарезке потерян текст: было %d букв, стало %d", len([]rune(text)), joined)
	}
}

// Конец предложения ищется во второй половине окна: разрыв посреди фразы
// модель достраивает по-своему. Здесь точка стоит на 300-м байте при окне
// в 400 — то есть внутри окна и дальше его середины.
func TestRezhemPoKontsuPredlozheniya(t *testing.T) {
	text := strings.Repeat("а", 150) + ". " + strings.Repeat("б", 300)
	parts := split(text, 400)
	if len(parts) < 2 || !strings.HasSuffix(parts[0], ".") {
		t.Fatalf("резать надо по точке, получилось: %d частей, первая кончается на %q",
			len(parts), lastRune(parts[0]))
	}
}

func lastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[len(r)-1])
}
