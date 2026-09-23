package domain

import (
	"strings"
	"testing"
	"time"
)

func TestLeytnerPomnyuDalsheNePomnyuSnachala(t *testing.T) {
	today := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	c := Card{Box: 1, DueOn: today}
	c = c.Review(true, today)
	if c.Box != 2 || !c.DueOn.Equal(today.AddDate(0, 0, 2)) {
		t.Errorf("помню из первой: коробка %d, срок %s", c.Box, c.DueOn.Format("02.01"))
	}
	c.Box = MaxBox
	if c = c.Review(true, today); c.Box != MaxBox || !c.DueOn.Equal(today.AddDate(0, 0, 16)) {
		t.Errorf("из последней коробки дальше некуда: %d %s", c.Box, c.DueOn.Format("02.01"))
	}
	if c = c.Review(false, today); c.Box != 1 || !c.DueOn.Equal(today) {
		t.Errorf("не помню — в первую и сегодня же: %d %s", c.Box, c.DueOn.Format("02.01"))
	}
}

func TestKartochkiChistyatsya(t *testing.T) {
	var many []QA
	for i := 0; i < 30; i++ {
		many = append(many, QA{Front: "вопрос " + string(rune('а'+i)), Back: "ответ"})
	}
	s := CardSet{Cards: append([]QA{{Front: " ", Back: "x"}, {Front: "Дюрация", Back: strings.Repeat("о", 1000)}, {Front: "дюрация", Back: "дубль"}}, many...)}.Clean()
	if len(s.Cards) != maxCards {
		t.Errorf("карточек %d, потолок %d", len(s.Cards), maxCards)
	}
	if s.Cards[0].Front != "Дюрация" || len([]rune(s.Cards[0].Back)) > maxCardText {
		t.Errorf("первая: %+v", s.Cards[0].Front)
	}
	if s.Cards[1].Front == "дюрация" {
		t.Error("дубль по регистру не отсеян")
	}
}
