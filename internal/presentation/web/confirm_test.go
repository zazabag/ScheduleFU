package web

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Удаление конспекта, записи и задания необратимо, поэтому одним нажатием
// ничего не удаляется: каждая форма удаления лежит внутри <details
// class="confirm"> и отправляется отдельной кнопкой «Да, удалить».
// Проверяется шаблон целиком — новая кнопка удаления без подтверждения
// уронит тест.
func TestUdalenieTolkoSPodtverzhdeniem(t *testing.T) {
	body, err := os.ReadFile("templates/lessons.html")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	actions := regexp.MustCompile(`value="([a-z]+-delete)"`).FindAllStringSubmatchIndex(src, -1)
	if len(actions) < 3 {
		t.Fatalf("действий удаления %d, ожидалось не меньше трёх", len(actions))
	}
	for _, m := range actions {
		name := src[m[2]:m[3]]
		before := src[:m[0]]
		open := strings.LastIndex(before, `<details class="confirm">`)
		closed := strings.LastIndex(before, `</details>`)
		if open < 0 || closed > open {
			t.Errorf("%s удаляется без подтверждения", name)
			continue
		}
		rest := src[m[1]:]
		end := strings.Index(rest, `</details>`)
		if end < 0 || !strings.Contains(rest[:end], "Да, удалить") {
			t.Errorf("%s: в подтверждении нет кнопки «Да, удалить»", name)
		}
	}
}

func TestKartochkaKonspektaPreduprezhdaetOSsylke(t *testing.T) {
	s, _ := sharedServer(t)
	var b strings.Builder
	if err := s.pages["lessons"].ExecuteTemplate(&b, "note-card", noteView{ID: 1, Saved: true, ShareHref: "/n/x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "перестанет открываться") {
		t.Error("перед удалением конспекта со ссылкой надо сказать, что ссылка сломается")
	}
}
