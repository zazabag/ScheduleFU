package web

import (
	"regexp"
	"testing"
)

// В каждом оформлении на экране дня есть выбор дня недели и перелистывание
// недель: без них попасть на пятницу из понедельника — четыре касания.
// Однажды «Плеер» их спрятал, и это заметили только на проде.
func TestVoVsehOformleniyahEstDniINedeli(t *testing.T) {
	hide := regexp.MustCompile(`(?s)(\.days|\.weeknav)[^{]*\{[^}]*display:\s*none`)
	for _, sk := range Skins {
		body, err := staticFS.ReadFile("static/skins/" + sk.ID + ".css")
		if err != nil {
			t.Fatal(err)
		}
		if loc := hide.FindIndex(body); loc != nil {
			t.Errorf("оформление %s прячет полосу дней или неделю: %q", sk.ID, body[loc[0]:loc[1]])
		}
	}
}
