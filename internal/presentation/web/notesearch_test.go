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

func TestOtryvokRezhetsyaPoGranitsamPodsvetki(t *testing.T) {
	got := splitSnippet("до " + ndom.HitStart + "дюрация" + ndom.HitEnd + " после")
	if len(got) != 3 || got[1].Text != "дюрация" || !got[1].Hit || got[0].Hit || got[2].Text != " после" {
		t.Errorf("куски: %+v", got)
	}
}

type searchRepo struct{ notes.Repository }

func (searchRepo) SearchNotes(_ context.Context, owner, _ string, _ int) ([]ndom.NoteHit, error) {
	return []ndom.NoteHit{{Snippet: "<script>x</script> " + ndom.HitStart + "Дюрация" + ndom.HitEnd,
		Note: ndom.Note{ID: 4, Title: "Облигации", Lesson: ndom.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы",
			Date: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), BeginsAt: "10:10"}}}}, nil
}

// Текст конспекта — от модели, недоверенный: подсветка собирается шаблоном,
// а всё остальное экранируется.
func TestPoiskPodsvechivaetINeVerstaetChuzhoyHTML(t *testing.T) {
	s := windowServer(t)
	clk, _ := clock.New("Europe/Moscow")
	s.d.Notes = notes.New(searchRepo{}, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), notes.Options{})
	req := httptest.NewRequest(http.MethodGet, "/lessons/search?q=%D0%B4%D1%8E%D1%80%D0%B0%D1%86%D0%B8%D1%8F", nil)
	req.AddCookie(&http.Cookie{Name: "schedulefu_owner", Value: "0123456789abcdef0123456789abcdef"})
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "<mark>Дюрация</mark>") {
		t.Error("совпадение не подсвечено")
	}
	if strings.Contains(body, "<script>x</script>") {
		t.Error("текст конспекта попал в страницу как HTML")
	}
	if !strings.Contains(body, "#note-4") || !strings.Contains(body, "day=2026-09-17T10%3a10") && !strings.Contains(body, "day=2026-09-17T10%3A10") {
		t.Errorf("ссылка на пару конспекта не собрана")
	}
}
