package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

type deckRepo struct{ notes.Repository }

func (deckRepo) CardStats(context.Context, string, string, string, time.Time) (int, int, *time.Time, error) {
	return 3, 10, nil, nil
}
func (deckRepo) DueCards(context.Context, string, string, string, time.Time, int) ([]ndom.Card, error) {
	return []ndom.Card{{ID: 11, Front: "Что такое дюрация?", Back: "Средневзвешенный срок <b>потоков</b>", Box: 2,
		Lesson: ndom.LessonRef{Date: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)}}}, nil
}
func (deckRepo) Terms(context.Context, string, string, string) ([]ndom.Card, error) {
	return []ndom.Card{{Front: "Купон", Back: "Периодический платёж"}}, nil
}

type anyCarder struct{}

func (anyCarder) Cards(context.Context, notes.CardsInput) (ndom.CardSet, error) {
	return ndom.CardSet{}, nil
}

func cardsServer(t *testing.T) *Server {
	s := windowServer(t)
	clk, _ := clock.New("Europe/Moscow")
	n := notes.New(deckRepo{}, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), notes.Options{})
	n.Carder = anyCarder{}
	s.d.Notes = n
	return s
}

func TestEkranKartochek(t *testing.T) {
	s := cardsServer(t)
	req := httptest.NewRequest(http.MethodGet, "/lessons/cards?group=%D0%9F%D0%9824-1&d=%D0%A4%D0%B8%D0%BD%D0%B0%D0%BD%D1%81%D1%8B", nil)
	req.AddCookie(&http.Cookie{Name: "schedulefu_owner", Value: "0123456789abcdef0123456789abcdef"})
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"Сегодня к повторению: 3 из 10", "Что такое дюрация?", "Показать ответ",
		`name="remembered" value="1"`, "Купон", "&lt;b&gt;потоков&lt;/b&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q", want)
		}
	}
}

func TestKnopkaKartochekVKonspekte(t *testing.T) {
	s := cardsServer(t)
	render := func(status string) string {
		var b strings.Builder
		v := noteView{ID: 1, Saved: true, CanCards: true, CardsStatus: status, CardsHref: "/lessons/cards?d=x"}
		if err := s.pages["lessons"].ExecuteTemplate(&b, "note-card", v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if !strings.Contains(render(""), `value="note-cards"`) {
		t.Error("у сохранённого конспекта нет «Сделать карточки»")
	}
	if !strings.Contains(render("working"), "готовим карточки") {
		t.Error("пока модель работает, нужно так и сказать")
	}
	if !strings.Contains(render("ready"), "Карточки и словарь ›") {
		t.Error("готовые карточки не открываются из конспекта")
	}
}
