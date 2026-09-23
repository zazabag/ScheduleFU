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
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

const shareToken = "AAAAAAAAAAAAAAAAAAAAAAAA"

// notesRepo — один конспект автора, открытый по ссылке.
type notesRepo struct {
	notes.Repository
	created []ndom.Note
}

func (r *notesRepo) NoteByShare(_ context.Context, token string) (ndom.Note, bool, error) {
	if token != shareToken {
		return ndom.Note{}, false, nil
	}
	at := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	return ndom.Note{ID: 7, OwnerKey: "автор", Title: "Дюрация облигаций", Body: "## Суть\nТекст <b>как есть</b>",
		SavedAt: &at, ShareToken: shareToken,
		Lesson: ndom.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы", Date: at, LecturerName: "Иванов И. И."}}, true, nil
}
func (r *notesRepo) HomeworksByNote(context.Context, string, int64) ([]ndom.Homework, error) {
	return nil, nil
}
func (r *notesRepo) CopyOf(context.Context, string, int64) (ndom.Note, bool, error) {
	return ndom.Note{}, false, nil
}
func (r *notesRepo) CreateNote(_ context.Context, n ndom.Note) (int64, error) {
	r.created = append(r.created, n)
	return 42, nil
}
func (r *notesRepo) SaveNote(context.Context, string, int64, time.Time) error { return nil }

func sharedServer(t *testing.T) (*Server, *notesRepo) {
	t.Helper()
	repo := &notesRepo{}
	clk, _ := clock.New("Europe/Moscow")
	s, err := New(Deps{Clock: clk, BuildingLabel: func(b string) string { return b },
		Notes: notes.New(repo, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), notes.Options{})})
	if err != nil {
		t.Fatal(err)
	}
	return s, repo
}

func TestKonspektPoSsylkeOtkryvaetsyaBezKlyuchaUstroystva(t *testing.T) {
	s, _ := sharedServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/n/"+shareToken, nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	for _, want := range []string{"Дюрация облигаций", "Финансы", "Сохранить себе", "&lt;b&gt;как есть&lt;/b&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("на странице нет %q", want)
		}
	}
	if !strings.Contains(rec.Header().Get("X-Robots-Tag"), "noindex") {
		t.Error("ссылки на конспекты не должны попадать в поисковики")
	}
}

func TestZakrytayaSsylkaGovoritObEtom(t *testing.T) {
	s, _ := sharedServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/n/BBBBBBBBBBBBBBBBBBBBBBBB", nil))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "Ссылка закрыта") {
		t.Errorf("код %d", rec.Code)
	}
}

func TestSohranitSebeKladyotKopiyuKZakreplyonnoyGruppe(t *testing.T) {
	s, repo := sharedServer(t)
	req := httptest.NewRequest(http.MethodPost, "/n/"+shareToken, nil)
	rec := httptest.NewRecorder()
	SetSubjectCookie(rec, sched.GroupSubject("ПИ24-2"), false)
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("код %d: %s", rec.Code, rec.Body.String())
	}
	if len(repo.created) != 1 || repo.created[0].Lesson.SubjectKey != "group:ПИ24-2" {
		t.Fatalf("копия: %+v", repo.created)
	}
	loc := rec.Header().Get("Location")
	// Копия открывается на экране своей пары, у закреплённой группы.
	if !strings.HasPrefix(loc, "/lessons?group=") || !strings.Contains(loc, "&day=") || !strings.HasSuffix(loc, "#note-42") {
		t.Errorf("после сохранения ведём к копии, а адрес %q", loc)
	}
}

// Кнопки обмена живут в общей карточке конспекта: она одна на экран записи
// и экран пары. Делиться можно только сохранённым.
func TestKnopkiObmenaVKartochkeKonspekta(t *testing.T) {
	s, _ := sharedServer(t)
	render := func(v noteView) string {
		var b strings.Builder
		if err := s.pages["lessons"].ExecuteTemplate(&b, "note-card", v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	if out := render(noteView{ID: 1}); strings.Contains(out, "note-share") {
		t.Error("черновиком поделиться нельзя")
	}
	if out := render(noteView{ID: 1, Saved: true}); !strings.Contains(out, `value="note-share"`) {
		t.Error("у сохранённого конспекта нет «Поделиться»")
	}
	out := render(noteView{ID: 1, Saved: true, ShareHref: "/n/" + shareToken})
	if !strings.Contains(out, `data-share="/n/`+shareToken+`"`) || !strings.Contains(out, `value="note-unshare"`) {
		t.Error("у открытой ссылки нужны «Отправить ссылку» и «Закрыть ссылку»")
	}
}
