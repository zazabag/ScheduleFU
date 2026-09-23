package notes

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Карточки fakeRepo не трогают: заглушки держат интерфейс.
func (f *fakeRepo) QueueCards(context.Context, string, int64, time.Time) (bool, error) {
	return false, nil
}
func (f *fakeRepo) ClaimCards(context.Context, time.Time) (domain.Note, bool, error) {
	return domain.Note{}, false, nil
}
func (f *fakeRepo) FinishCards(context.Context, int64, string) error         { return nil }
func (f *fakeRepo) AddCards(context.Context, []domain.Card) (int, error)     { return 0, nil }
func (f *fakeRepo) SaveReview(context.Context, domain.Card, time.Time) error { return nil }
func (f *fakeRepo) Card(context.Context, string, int64) (domain.Card, bool, error) {
	return domain.Card{}, false, nil
}
func (f *fakeRepo) Terms(context.Context, string, string, string) ([]domain.Card, error) {
	return nil, nil
}
func (f *fakeRepo) DueCards(context.Context, string, string, string, time.Time, int) ([]domain.Card, error) {
	return nil, nil
}
func (f *fakeRepo) CardStats(context.Context, string, string, string, time.Time) (int, int, *time.Time, error) {
	return 0, 0, nil, nil
}

// cardsRepo — один конспект в очереди на карточки.
type cardsRepo struct {
	Repository
	queued   *domain.Note
	finished map[int64]string
	added    []domain.Card
	card     domain.Card
	reviewed *domain.Card
}

func (r *cardsRepo) ClaimCards(context.Context, time.Time) (domain.Note, bool, error) {
	if r.queued == nil {
		return domain.Note{}, false, nil
	}
	n := *r.queued
	r.queued = nil
	return n, true, nil
}
func (r *cardsRepo) FinishCards(_ context.Context, id int64, failure string) error {
	r.finished[id] = failure
	return nil
}
func (r *cardsRepo) AddCards(_ context.Context, cs []domain.Card) (int, error) {
	r.added = append(r.added, cs...)
	return len(cs), nil
}
func (r *cardsRepo) Card(_ context.Context, owner string, id int64) (domain.Card, bool, error) {
	return r.card, owner == r.card.OwnerKey && id == r.card.ID, nil
}
func (r *cardsRepo) SaveReview(_ context.Context, c domain.Card, _ time.Time) error {
	r.reviewed = &c
	return nil
}

type fakeCarder struct {
	set domain.CardSet
	err error
}

func (f fakeCarder) Cards(context.Context, CardsInput) (domain.CardSet, error) { return f.set, f.err }

func cardsService(repo *cardsRepo, c Carder) *Service {
	clk := clock.Fixed(time.Date(2026, 9, 24, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600)))
	s := New(repo, nil, nil, nil, clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{})
	s.Carder = c
	return s
}

func TestKartochkiIzOcherediLozhatsyaKPredmetu(t *testing.T) {
	note := domain.Note{ID: 5, OwnerKey: "я", Body: "текст", Lesson: domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "Финансы"}}
	repo := &cardsRepo{queued: &note, finished: map[int64]string{}}
	s := cardsService(repo, fakeCarder{set: domain.CardSet{
		Cards: []domain.QA{{Front: "Что такое дюрация?", Back: "срок"}},
		Terms: []domain.QA{{Front: "Купон", Back: "платёж"}},
	}})
	// Записей нет (распознавание не настроено) — очередь карточек всё равно
	// разбирается: им нужна только модель.
	done, err := s.ProcessOne(context.Background())
	if err != nil || !done {
		t.Fatalf("обработка: %v %v", done, err)
	}
	if f, ok := repo.finished[5]; !ok || f != "" {
		t.Errorf("итог: %q", f)
	}
	if len(repo.added) != 2 || repo.added[0].Kind != domain.CardQuestion || repo.added[1].Kind != domain.CardTerm {
		t.Fatalf("карточки: %+v", repo.added)
	}
	c := repo.added[0]
	if c.OwnerKey != "я" || c.Box != 1 || c.DueOn.Format("2006-01-02") != "2026-09-24" || *c.NoteID != 5 {
		t.Errorf("новая карточка — к повторению сегодня: %+v", c)
	}
}

func TestOshibkaModeliVidnaVKonspekte(t *testing.T) {
	note := domain.Note{ID: 6, OwnerKey: "я", Body: "текст"}
	repo := &cardsRepo{queued: &note, finished: map[int64]string{}}
	s := cardsService(repo, fakeCarder{err: errors.New("модель недоступна")})
	if _, err := s.ProcessOne(context.Background()); err == nil {
		t.Error("ошибка модели не вернулась")
	}
	if f := repo.finished[6]; f == "" {
		t.Error("причина неудачи должна дойти до человека")
	}
}

func TestPomnyuNePomnyu(t *testing.T) {
	repo := &cardsRepo{finished: map[int64]string{}, card: domain.Card{ID: 1, OwnerKey: "я", Box: 2}}
	s := cardsService(repo, fakeCarder{})
	if err := s.Review(context.Background(), "я", 1, true); err != nil {
		t.Fatal(err)
	}
	if repo.reviewed.Box != 3 || repo.reviewed.DueOn.Format("2006-01-02") != "2026-09-28" {
		t.Errorf("помню из второй: %+v", repo.reviewed)
	}
	if err := s.Review(context.Background(), "чужой", 1, true); err == nil {
		t.Error("чужую карточку оценить нельзя")
	}
}
