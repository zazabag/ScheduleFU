package llm

import (
	"strings"
	"testing"

	"github.com/zazabag/schedulefu/internal/modules/notes"
)

func TestKartochkiIzOtvetaModeli(t *testing.T) {
	s, err := parseCards("Вот карточки:\n```json\n" + `{"cards":[{"q":"Что такое дюрация?","a":"Средневзвешенный срок потоков облигации."},{"q":"","a":"пусто"}],
	  "terms":[{"term":"Купон","def":"Периодический процентный платёж по облигации."}]}` + "\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Cards) != 1 || s.Cards[0].Front != "Что такое дюрация?" || len(s.Terms) != 1 {
		t.Errorf("разбор: %+v", s)
	}
}

func TestPromptKartochekBezImenIMetok(t *testing.T) {
	p := cardsPrompt(notes.CardsInput{Discipline: "Финансы", Title: "Облигации", Theses: []string{"дюрация"}, Body: "текст"})
	for _, want := range []string{"«Финансы»", "«Облигации»", "- дюрация", "<<<\nтекст\n>>>"} {
		if !strings.Contains(p, want) {
			t.Errorf("в запросе нет %q", want)
		}
	}
}
