package web

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Баг 23.09.2026: стили шрифтов шли с fonts.googleapis.com и держали
// отрисовку — замер с телефона показал 4–5 с до первой картинки при ответе
// сервера за 0,23 с. Шрифты теперь свои: у каждого оформления есть файлы
// в static/fonts, и каждый woff2, на который ссылается CSS, на месте.
func TestShriftyOformleniyLezhatUNas(t *testing.T) {
	woff := regexp.MustCompile(`url\(([^)]+)\)`)
	for _, sk := range Skins {
		if len(sk.Fonts) == 0 {
			t.Errorf("%s: у оформления нет шрифтов", sk.ID)
		}
		for _, f := range sk.Fonts {
			css, err := fs.ReadFile(staticFS, "static/fonts/"+f+".css")
			if err != nil {
				t.Errorf("%s: нет static/fonts/%s.css", sk.ID, f)
				continue
			}
			for _, m := range woff.FindAllStringSubmatch(string(css), -1) {
				if strings.Contains(m[1], "//") {
					t.Errorf("%s.css ссылается наружу: %s", f, m[1])
				}
				if _, err := fs.Stat(staticFS, "static/fonts/"+m[1]); err != nil {
					t.Errorf("%s.css: нет файла %s", f, m[1])
				}
				// Без отпечатка в имени шрифт кэшировался бы пять минут, и
				// телефон переспрашивал бы его на каждом переходе.
				if !fontFile.MatchString("/static/fonts/" + m[1]) {
					t.Errorf("%s: имя без отпечатка — годового кэша не будет", m[1])
				}
			}
		}
	}
}
