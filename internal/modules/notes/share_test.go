package notes

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Обмен fakeRepo не трогает: заглушки держат интерфейс.
func (f *fakeRepo) SetShareToken(context.Context, string, int64, string, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeRepo) NoteByShare(context.Context, string) (domain.Note, bool, error) {
	return domain.Note{}, false, nil
}
func (f *fakeRepo) CopyOf(context.Context, string, int64) (domain.Note, bool, error) {
	return domain.Note{}, false, nil
}

// shareRepo — конспекты и задания разных владельцев в памяти.
type shareRepo struct {
	Repository
	notes []domain.Note
	hws   []domain.Homework
}

func (r *shareRepo) find(pred func(domain.Note) bool) (domain.Note, bool, error) {
	for _, n := range r.notes {
		if pred(n) {
			return n, true, nil
		}
	}
	return domain.Note{}, false, nil
}
func (r *shareRepo) Note(_ context.Context, owner string, id int64) (domain.Note, bool, error) {
	return r.find(func(n domain.Note) bool { return n.ID == id && n.OwnerKey == owner })
}
func (r *shareRepo) NoteByShare(_ context.Context, token string) (domain.Note, bool, error) {
	return r.find(func(n domain.Note) bool { return n.ShareToken == token && n.Saved() })
}
func (r *shareRepo) CopyOf(_ context.Context, owner string, src int64) (domain.Note, bool, error) {
	return r.find(func(n domain.Note) bool { return n.OwnerKey == owner && n.CopiedFrom != nil && *n.CopiedFrom == src })
}
func (r *shareRepo) SetShareToken(_ context.Context, owner string, id int64, token string, _ time.Time) (bool, error) {
	for i, n := range r.notes {
		if n.ID == id && n.OwnerKey == owner && n.Saved() {
			r.notes[i].ShareToken = token
			return true, nil
		}
	}
	return false, nil
}
func (r *shareRepo) CreateNote(_ context.Context, n domain.Note) (int64, error) {
	n.ID = int64(len(r.notes) + 1)
	r.notes = append(r.notes, n)
	return n.ID, nil
}
func (r *shareRepo) SaveNote(_ context.Context, owner string, id int64, at time.Time) error {
	for i := range r.notes {
		if r.notes[i].ID == id && r.notes[i].OwnerKey == owner {
			r.notes[i].SavedAt = &at
		}
	}
	return nil
}
func (r *shareRepo) HomeworksByNote(_ context.Context, owner string, id int64) ([]domain.Homework, error) {
	var out []domain.Homework
	for _, h := range r.hws {
		if h.OwnerKey == owner && h.NoteID != nil && *h.NoteID == id {
			out = append(out, h)
		}
	}
	return out, nil
}
func (r *shareRepo) CreateHomework(_ context.Context, h domain.Homework) (int64, error) {
	h.ID = int64(len(r.hws) + 1)
	r.hws = append(r.hws, h)
	return h.ID, nil
}
func (r *shareRepo) SaveHomework(_ context.Context, owner string, id int64, at time.Time) error {
	for i := range r.hws {
		if r.hws[i].ID == id && r.hws[i].OwnerKey == owner {
			r.hws[i].SavedAt = &at
		}
	}
	return nil
}

func shareService(t *testing.T) (*Service, *shareRepo) {
	t.Helper()
	at := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	one := int64(1)
	repo := &shareRepo{
		notes: []domain.Note{
			{ID: 1, OwnerKey: "автор", Title: "Дюрация", Body: "текст", SavedAt: &at,
				Lesson: domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы"}},
			{ID: 2, OwnerKey: "автор", Title: "Черновик"},
		},
		hws: []domain.Homework{
			{ID: 1, OwnerKey: "автор", NoteID: &one, Body: "задача 5", SavedAt: &at, DoneAt: &at},
			{ID: 2, OwnerKey: "автор", NoteID: &one, Body: "черновик задания"},
		},
	}
	clk, _ := clock.New("Europe/Moscow")
	return New(repo, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{}), repo
}

func TestPodelitsyaMozhnoTolkoSohranyonnymISsylkaNeMenyaetsya(t *testing.T) {
	s, _ := shareService(t)
	ctx := context.Background()
	if _, err := s.Share(ctx, "автор", 2); err != ErrNotShareable {
		t.Errorf("черновик ссылки не получает, а ошибка %v", err)
	}
	if _, err := s.Share(ctx, "чужой", 1); err != ErrNotShareable {
		t.Errorf("чужим конспектом делиться нельзя, а ошибка %v", err)
	}
	a, err := s.Share(ctx, "автор", 1)
	if err != nil || !validShareToken(a) {
		t.Fatalf("ключ %q, ошибка %v", a, err)
	}
	if b, _ := s.Share(ctx, "автор", 1); b != a {
		t.Error("повторное «поделиться» сломало бы уже разосланную ссылку")
	}
}

func TestPoSsylkeNeVidenKlyuchAvtoraIChernoviki(t *testing.T) {
	s, _ := shareService(t)
	ctx := context.Background()
	token, _ := s.Share(ctx, "автор", 1)
	n, hws, ok, err := s.Shared(ctx, token)
	if err != nil || !ok {
		t.Fatalf("конспект по ссылке не открылся: %v", err)
	}
	if n.OwnerKey != "" {
		t.Error("ключ автора ушёл наружу")
	}
	if len(hws) != 1 || hws[0].Body != "задача 5" || hws[0].OwnerKey != "" {
		t.Errorf("задания по ссылке: %+v", hws)
	}
	if _, _, ok, _ := s.Shared(ctx, "короткий"); ok {
		t.Error("ключ неверной формы не должен доходить до базы")
	}
}

func TestSohranitSebeKopiyaPodSvoimRaspisaniemBezDubley(t *testing.T) {
	s, repo := shareService(t)
	ctx := context.Background()
	token, _ := s.Share(ctx, "автор", 1)
	cp, err := s.SaveShared(ctx, "друг", "group:ПИ24-2", token)
	if err != nil {
		t.Fatal(err)
	}
	if cp.OwnerKey != "друг" || cp.Lesson.SubjectKey != "group:ПИ24-2" || !cp.Saved() || cp.ShareToken != "" {
		t.Errorf("копия: %+v", cp)
	}
	var hws []domain.Homework
	for _, h := range repo.hws {
		if h.OwnerKey == "друг" {
			hws = append(hws, h)
		}
	}
	if len(hws) != 1 || hws[0].Done() || !hws[0].Saved() {
		t.Errorf("у друга своё несделанное задание, а получили %+v", hws)
	}
	again, _ := s.SaveShared(ctx, "друг", "group:ПИ24-2", token)
	if again.ID != cp.ID {
		t.Error("второе «сохранить себе» создало дубль")
	}
	if err := s.Unshare(ctx, "автор", 1); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.Shared(ctx, token); ok {
		t.Error("закрытая ссылка продолжает открываться")
	}
}
