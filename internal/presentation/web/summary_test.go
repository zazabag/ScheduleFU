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

const testOwner = "0123456789abcdef0123456789abcdef"

// summaryRepo — три конспекта финансов, как их отдаёт хранилище: от новых
// к старым.
type summaryRepo struct{ notes.Repository }

func (summaryRepo) Notes(_ context.Context, owner, _, disc string) ([]ndom.Note, error) {
	if owner != testOwner {
		return nil, nil
	}
	at := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	n := func(id int64, d int, title string) ndom.Note {
		return ndom.Note{ID: id, Title: title, Body: "текст " + title, SavedAt: &time.Time{}, Lesson: ndom.LessonRef{Discipline: disc, Date: at(d)}}
	}
	return []ndom.Note{n(3, 22, "Третья"), n(2, 15, "Вторая"), n(1, 8, "Первая")}, nil
}

func (summaryRepo) Homeworks(context.Context, string, string, string, bool) ([]ndom.Homework, error) {
	return []ndom.Homework{{Body: "задача 5", Lesson: ndom.LessonRef{Date: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}}}, nil
}

func TestSvodkaPredmetaPoPoryadkuKursa(t *testing.T) {
	s := windowServer(t)
	clk, _ := clock.New("Europe/Moscow")
	s.d.Notes = notes.New(summaryRepo{}, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), notes.Options{})
	req := httptest.NewRequest(http.MethodGet, "/lessons/summary?group=%D0%9F%D0%9824-1&d=%D0%A4%D0%B8%D0%BD%D0%B0%D0%BD%D1%81%D1%8B", nil)
	req.AddCookie(&http.Cookie{Name: "schedulefu_owner", Value: testOwner})
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	first, second, third := strings.Index(body, "текст Первая"), strings.Index(body, "текст Вторая"), strings.Index(body, "текст Третья")
	if first < 0 || !(first < second && second < third) {
		t.Errorf("конспекты не по порядку курса: %d %d %d", first, second, third)
	}
	if !strings.Contains(body, `href="#p1"`) || !strings.Contains(body, "задача 5") {
		t.Error("нет оглавления или заданий")
	}
	// Без ключа устройства — пустая сводка, а не чужие конспекты.
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, req.URL.String(), nil))
	if strings.Contains(rec.Body.String(), "текст Первая") {
		t.Error("сводка показала конспекты без ключа устройства")
	}
}
