package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// recapRepo — два сохранённых конспекта по финансам: неделю назад и в
// тот же день, что открытая пара.
type recapRepo struct{ notes.Repository }

func (recapRepo) Notes(_ context.Context, owner, _, disc string) ([]ndom.Note, error) {
	if owner != "0123456789abcdef0123456789abcdef" || disc != "Финансы" {
		return nil, nil
	}
	at := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	saved := at(20)
	return []ndom.Note{
		{ID: 9, Title: "Сегодняшняя", Lesson: ndom.LessonRef{Discipline: disc, Date: at(24)}, SavedAt: &saved},
		{ID: 7, Title: "Дюрация", Theses: []string{"а", "б", "в", "г"}, SavedAt: &saved,
			Lesson: ndom.LessonRef{Discipline: disc, Date: at(17), BeginsAt: "10:10"}},
	}, nil
}

func TestVProshlyyRazIzSvoegoKonspekta(t *testing.T) {
	s := windowServer(t)
	clk, _ := clock.New("Europe/Moscow")
	s.d.Notes = notes.New(recapRepo{}, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), notes.Options{})
	rows := []lessonRow{{Discipline: "Финансы"}, {Discipline: "История"}}
	req := httptest.NewRequest(http.MethodGet, "/schedule", nil)
	req.AddCookie(&http.Cookie{Name: "schedulefu_owner", Value: "0123456789abcdef0123456789abcdef"})
	s.markRecaps(req, rows, sched.GroupSubject("ПИ24-1"), time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), false)
	r := rows[0].Recap
	if r == nil || r.Title != "Дюрация" || len(r.Theses) != maxRecapTheses {
		t.Fatalf("в прошлый раз: %+v", r)
	}
	if want := "#note-7"; len(r.Href) < len(want) || string(r.Href)[len(r.Href)-len(want):] != want {
		t.Errorf("ссылка на конспект: %s", r.Href)
	}
	if rows[1].Recap != nil {
		t.Error("по истории конспектов нет")
	}
	// Без ключа устройства — чужие конспекты не показываем, и своих нет.
	rows = []lessonRow{{Discipline: "Финансы"}}
	s.markRecaps(httptest.NewRequest(http.MethodGet, "/schedule", nil), rows, sched.GroupSubject("ПИ24-1"), time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), false)
	if rows[0].Recap != nil {
		t.Error("без ключа устройства конспектов нет")
	}
}
