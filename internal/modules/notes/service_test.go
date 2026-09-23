package notes

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// fakeRepo — хранилище в памяти. Ровно столько, сколько нужно сервису:
// настоящее проверяется тестами с базой, а здесь важны решения сервиса.
type fakeRepo struct {
	rec       domain.Recording
	notes     []domain.Note
	homeworks []domain.Homework
	failed    string
	gaveUp    bool
	completed bool
}

func (f *fakeRepo) CreateRecording(_ context.Context, rec domain.Recording) (int64, error) {
	f.rec = rec
	f.rec.ID = 7
	return 7, nil
}

func (f *fakeRepo) Recording(_ context.Context, owner string, id int64) (domain.Recording, bool, error) {
	if f.rec.ID != id || f.rec.OwnerKey != owner {
		return domain.Recording{}, false, nil
	}
	return f.rec, true, nil
}

func (f *fakeRepo) CountChunk(_ context.Context, _ int64, seq int, size int64, path string) error {
	if seq != f.rec.Chunks {
		return errors.New("кусок не по порядку дошёл до хранилища")
	}
	f.rec.Chunks, f.rec.Bytes, f.rec.AudioPath = seq+1, f.rec.Bytes+size, path
	return nil
}

func (f *fakeRepo) Enqueue(_ context.Context, _ int64, gaps []domain.Gap) error {
	f.rec.Status, f.rec.Gaps = domain.StatusQueued, gaps
	return nil
}

func (f *fakeRepo) Recordings(context.Context, string, string, string) ([]domain.Recording, error) {
	return nil, nil
}

func (f *fakeRepo) DeleteRecording(_ context.Context, _ string, _ int64) (string, error) {
	path := f.rec.AudioPath
	f.rec = domain.Recording{}
	return path, nil
}

func (f *fakeRepo) Claim(context.Context, time.Time) (domain.Recording, bool, error) {
	if f.rec.Status != domain.StatusQueued {
		return domain.Recording{}, false, nil
	}
	f.rec.Status = domain.StatusDecoding
	return f.rec, true, nil
}

func (f *fakeRepo) SetStatus(_ context.Context, _ int64, st domain.Status) error {
	f.rec.Status = st
	return nil
}

func (f *fakeRepo) SetTranscript(_ context.Context, _ int64, transcript string, dur int) error {
	f.rec.Transcript, f.rec.DurationSec = transcript, dur
	return nil
}

func (f *fakeRepo) Complete(_ context.Context, _ int64) error {
	f.completed = true
	f.rec.Status = domain.StatusReady
	return nil
}

func (f *fakeRepo) Fail(_ context.Context, _ int64, reason string, _ time.Time, giveUp bool) error {
	f.failed, f.gaveUp = reason, giveUp
	return nil
}

func (f *fakeRepo) CreateNote(_ context.Context, n domain.Note) (int64, error) {
	n.ID = int64(len(f.notes) + 1)
	f.notes = append(f.notes, n)
	return n.ID, nil
}

func (f *fakeRepo) Note(_ context.Context, owner string, id int64) (domain.Note, bool, error) {
	for _, n := range f.notes {
		if n.ID == id && n.OwnerKey == owner {
			return n, true, nil
		}
	}
	return domain.Note{}, false, nil
}

func (f *fakeRepo) NoteByRecording(context.Context, string, int64) (domain.Note, bool, error) {
	if len(f.notes) == 0 {
		return domain.Note{}, false, nil
	}
	return f.notes[0], true, nil
}

func (f *fakeRepo) DraftNotes(context.Context, string, string, string) ([]domain.Note, error) {
	return nil, nil
}

func (f *fakeRepo) Notes(context.Context, string, string, string) ([]domain.Note, error) {
	return nil, nil
}

func (f *fakeRepo) SaveNote(_ context.Context, _ string, id int64, at time.Time) error {
	for i := range f.notes {
		if f.notes[i].ID == id {
			f.notes[i].SavedAt = &at
		}
	}
	return nil
}

func (f *fakeRepo) DeleteNote(_ context.Context, _ string, id int64) error {
	var kept []domain.Note
	for _, n := range f.notes {
		if n.ID != id {
			kept = append(kept, n)
		}
	}
	f.notes = kept
	return nil
}

func (f *fakeRepo) CreateHomework(_ context.Context, h domain.Homework) (int64, error) {
	h.ID = int64(len(f.homeworks) + 1)
	f.homeworks = append(f.homeworks, h)
	return h.ID, nil
}

func (f *fakeRepo) Homeworks(context.Context, string, string, string, bool) ([]domain.Homework, error) {
	return f.homeworks, nil
}

func (f *fakeRepo) HomeworksByNote(context.Context, string, int64) ([]domain.Homework, error) {
	return f.homeworks, nil
}

func (f *fakeRepo) SaveHomework(_ context.Context, _ string, id int64, at time.Time) error {
	for i := range f.homeworks {
		if f.homeworks[i].ID == id {
			f.homeworks[i].SavedAt = &at
		}
	}
	return nil
}

func (f *fakeRepo) SetHomeworkDone(context.Context, string, int64, *time.Time) error { return nil }
func (f *fakeRepo) DeleteHomework(context.Context, string, int64) error              { return nil }
func (f *fakeRepo) Disciplines(context.Context, string, string) ([]Discipline, error) {
	return nil, nil
}
func (f *fakeRepo) CleanupLLMCalls(context.Context, time.Duration) (int64, error) { return 0, nil }

func (f *fakeRepo) CleanupDrafts(context.Context, time.Duration) (int64, error) { return 0, nil }
func (f *fakeRepo) StuckAudio(context.Context, time.Duration) ([]domain.Recording, error) {
	return nil, nil
}

// fakeMedia по умолчанию «слышит» полуторачасовую пару нормальной
// громкости; sound подменяет это для проверок тишины и обрывков.
type fakeMedia struct {
	err   error
	sound *domain.Sound
}

func (m fakeMedia) ToWav(_ context.Context, _, dst string) (domain.Sound, error) {
	if m.err != nil {
		return domain.Sound{}, m.err
	}
	snd := domain.Sound{DurationSec: 5400, PeakDB: -6}
	if m.sound != nil {
		snd = *m.sound
	}
	return snd, os.WriteFile(dst, []byte("wav"), 0o600)
}

type fakeASR struct {
	segs []domain.Segment
	err  error
}

func (a fakeASR) Transcribe(context.Context, string) ([]domain.Segment, error) {
	return a.segs, a.err
}

type fakeLLM struct {
	recap domain.Recap
	err   error
	seen  SummaryInput
}

func (l *fakeLLM) Summarize(_ context.Context, in SummaryInput) (domain.Recap, error) {
	l.seen = in
	return l.recap, l.err
}

func newService(t *testing.T, repo *fakeRepo, asr Recognizer, sum Summarizer, media Media) *Service {
	t.Helper()
	clk, err := clock.New("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repo, asr, sum, media, clk, log, Options{AudioDir: t.TempDir(), Attempts: 2})
}

func lesson() domain.LessonRef {
	return domain.LessonRef{SubjectKey: "group:ПИ24-1", Discipline: "История",
		Date: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), BeginsAt: "10:10", LecturerName: "Иванов И.И."}
}

func TestKuskiPrinimayutsyaTolkoPoPoryadku(t *testing.T) {
	repo := &fakeRepo{}
	s := newService(t, repo, fakeASR{}, &fakeLLM{}, fakeMedia{})
	ctx := context.Background()
	rec, err := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("раз")); err != nil {
		t.Fatal(err)
	}
	// Кусок из будущего означает, что предыдущий потерялся: принять его —
	// молча склеить запись с дырой.
	if _, err := s.Append(ctx, "owner", rec.ID, 5, strings.NewReader("пять")); err == nil {
		t.Error("кусок с пропуском принят")
	}
	// А повтор уже принятого — обычное дело при обрыве связи, и он должен
	// пройти тихо, не удвоив запись.
	if _, err := s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("раз")); err != nil {
		t.Errorf("повтор куска отвергнут: %v", err)
	}
	if want := int64(len("раз")); repo.rec.Bytes != want {
		t.Errorf("в записи %d байт вместо %d: повтор куска её удвоил", repo.rec.Bytes, want)
	}
}

func TestChuzhuyuZapisNeDopolnit(t *testing.T) {
	repo := &fakeRepo{}
	s := newService(t, repo, fakeASR{}, &fakeLLM{}, fakeMedia{})
	ctx := context.Background()
	rec, err := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "другой", rec.ID, 0, strings.NewReader("раз")); err == nil {
		t.Fatal("чужая запись дополнена по номеру")
	}
}

func TestPustayaZapisNeIdyotVOchered(t *testing.T) {
	repo := &fakeRepo{}
	s := newService(t, repo, fakeASR{}, &fakeLLM{}, fakeMedia{})
	ctx := context.Background()
	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	if _, err := s.Finish(ctx, "owner", rec.ID, nil); err == nil {
		t.Fatal("запись без звука принята в обработку")
	}
}

func TestObrabotkaDayotChernovikKonspektaIZadaniy(t *testing.T) {
	repo := &fakeRepo{}
	llm := &fakeLLM{recap: domain.Recap{Title: "Пётр I", Body: "## Реформы\nтекст", Theses: []string{"тезис"},
		Homework: []domain.RecapHomework{{Text: "главу 3", DueNote: "к четвергу"}}}}
	s := newService(t, repo, fakeASR{segs: []domain.Segment{{Text: "сегодня о Петре"}}}, llm, fakeMedia{})
	ctx := context.Background()

	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	if _, err := s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("звук")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(ctx, "owner", rec.ID, nil); err != nil {
		t.Fatal(err)
	}
	if done, err := s.ProcessOne(ctx); err != nil || !done {
		t.Fatalf("обработка: сделано=%v, ошибка=%v", done, err)
	}

	if !repo.completed {
		t.Error("обработка не закрыта")
	}
	if len(repo.notes) != 1 || repo.notes[0].Title != "Пётр I" {
		t.Fatalf("конспекты: %+v", repo.notes)
	}
	if repo.notes[0].Saved() {
		t.Error("конспект сохранён сам: человек должен нажать «Сохранить»")
	}
	if len(repo.homeworks) != 1 || repo.homeworks[0].DueNote != "к четвергу" {
		t.Fatalf("задания: %+v", repo.homeworks)
	}
	if repo.homeworks[0].Saved() {
		t.Error("задание сохранено само")
	}
	// Имя преподавателя есть в слепке пары, но наружу, в модель, не уходит.
	if strings.Contains(llm.seen.Transcript, "Иванов") || llm.seen.Discipline != "История" {
		t.Errorf("модели ушло лишнее: %+v", llm.seen)
	}
}

func TestZapisUdalyaetsyaSrazuPosleRasshifrovki(t *testing.T) {
	repo := &fakeRepo{}
	dir := t.TempDir()
	clk, _ := clock.New("Europe/Moscow")
	s := New(repo, fakeASR{segs: []domain.Segment{{Text: "речь"}}},
		&fakeLLM{recap: domain.Recap{Body: "конспект"}}, fakeMedia{},
		clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{AudioDir: dir, Attempts: 2})
	ctx := context.Background()

	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	_, _ = s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("звук"))
	_, _ = s.Finish(ctx, "owner", rec.ID, nil)
	if _, err := s.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rec-7.bin")); !os.IsNotExist(err) {
		t.Fatal("голос преподавателя остался на диске после расшифровки")
	}
	if repo.rec.Transcript == "" {
		t.Error("расшифровка не сохранена, пересобрать конспект будет не из чего")
	}
}

func TestPoslePopytokSdayomsyaINeDerzhimZvuk(t *testing.T) {
	repo := &fakeRepo{}
	dir := t.TempDir()
	clk, _ := clock.New("Europe/Moscow")
	s := New(repo, fakeASR{err: errors.New("модель не отвечает")}, &fakeLLM{}, fakeMedia{},
		clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{AudioDir: dir, Attempts: 1})
	ctx := context.Background()

	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	_, _ = s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("звук"))
	_, _ = s.Finish(ctx, "owner", rec.ID, nil)
	if _, err := s.ProcessOne(ctx); err == nil {
		t.Fatal("ошибка распознавания не всплыла")
	}
	if !repo.gaveUp {
		t.Error("после последней попытки надо сдаваться, а не крутить очередь")
	}
	if _, err := os.Stat(filepath.Join(dir, "rec-7.bin")); !os.IsNotExist(err) {
		t.Error("сдались, а запись голоса оставили")
	}
}

func TestBezModeleyZapisNeBeryotsya(t *testing.T) {
	s := newService(t, &fakeRepo{}, nil, nil, nil)
	if s.CanProcess() {
		t.Fatal("сервис без распознавания считает себя готовым")
	}
	if done, err := s.ProcessOne(context.Background()); done || err != nil {
		t.Fatalf("обработка без моделей: сделано=%v, ошибка=%v", done, err)
	}
}

// Пропуски, которые браузер заметил во время записи, доезжают до модели:
// и списком, и меткой в самой расшифровке.
func TestPropuskiDoezzhayutDoKonspekta(t *testing.T) {
	repo := &fakeRepo{}
	llm := &fakeLLM{recap: domain.Recap{Title: "т", Body: "текст"}}
	s := newService(t, repo, fakeASR{segs: []domain.Segment{
		{Start: 0, Text: "начало"}, {Start: 10 * time.Minute, Text: "продолжение"}}}, llm, fakeMedia{})
	ctx := context.Background()
	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	if _, err := s.Append(ctx, "owner", rec.ID, 0, strings.NewReader("звук")); err != nil {
		t.Fatal(err)
	}
	gaps := []domain.Gap{{AtSec: 300, DurSec: 240}, {AtSec: 1, DurSec: 1}}
	if _, err := s.Finish(ctx, "owner", rec.ID, gaps); err != nil {
		t.Fatal(err)
	}
	if len(repo.rec.Gaps) != 1 {
		t.Fatalf("в записи сохранено пропусков: %+v", repo.rec.Gaps)
	}
	if _, err := s.ProcessOne(ctx); err != nil {
		t.Fatal(err)
	}
	if len(llm.seen.Gaps) != 1 || !strings.Contains(llm.seen.Transcript, "начало [пропуск в записи ~4 мин] продолжение") {
		t.Errorf("модели ушло: %+v", llm.seen)
	}
}

// processEmpty проводит запись через обработку при заданном звуке и пустой
// расшифровке и возвращает хранилище.
func processEmpty(t *testing.T, snd domain.Sound, chunks int) *fakeRepo {
	t.Helper()
	repo := &fakeRepo{}
	clk, _ := clock.New("Europe/Moscow")
	s := New(repo, fakeASR{}, &fakeLLM{}, fakeMedia{sound: &snd},
		clk, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{AudioDir: t.TempDir(), Attempts: 3})
	ctx := context.Background()
	rec, _ := s.Start(ctx, "owner", lesson(), domain.OriginRecord)
	for i := 0; i < chunks; i++ {
		_, _ = s.Append(ctx, "owner", rec.ID, i, strings.NewReader("кусок"))
	}
	_, _ = s.Finish(ctx, "owner", rec.ID, nil)
	if _, err := s.ProcessOne(ctx); err == nil {
		t.Fatal("пустая расшифровка прошла как успех")
	}
	return repo
}

// Баг 23.09.2026: запись с пустой расшифровкой крутилась в очереди три
// попытки по десять минут — человек полчаса смотрел на «в очереди» и не
// получал ничего. Тишина от повтора не станет речью: сдаёмся сразу и
// говорим почему.
func TestTishinaNePovtoryaetsyaIObyasnyaetsya(t *testing.T) {
	repo := processEmpty(t, domain.Sound{DurationSec: 60, PeakDB: -70}, 4)
	if !repo.gaveUp {
		t.Error("тишину поставили на повтор")
	}
	if !strings.Contains(repo.failed, "тишин") {
		t.Errorf("причина: %q", repo.failed)
	}
}

// Запись шла минуту (четыре куска по 15 с), а из файла прочиталось пять
// секунд: звук дошёл не целиком. Это другая беда, чем тишина, и человеку
// надо сказать именно её.
func TestObryvokZapisiNazyvaetsyaObryvkom(t *testing.T) {
	repo := processEmpty(t, domain.Sound{DurationSec: 5, PeakDB: -10}, 4)
	if !repo.gaveUp || !strings.Contains(repo.failed, "5 с") || !strings.Contains(repo.failed, "1 мин") {
		t.Errorf("сдались=%v, причина: %q", repo.gaveUp, repo.failed)
	}
}

// Звук есть и полный, а слов нет — повтор тоже не поможет.
func TestRechNeRaspoznanaBezPovtora(t *testing.T) {
	repo := processEmpty(t, domain.Sound{DurationSec: 60, PeakDB: -12}, 4)
	if !repo.gaveUp || !strings.Contains(repo.failed, "не распознано") {
		t.Errorf("сдались=%v, причина: %q", repo.gaveUp, repo.failed)
	}
}

// Модель не нашла задания — человек вписывает его прямо при сохранении
// конспекта, и оно ложится к той же паре, уже сохранённым.
func TestZadanieVpisyvaetsyaPriSohraneniiKonspekta(t *testing.T) {
	repo := &fakeRepo{}
	s := newService(t, repo, fakeASR{}, &fakeLLM{}, fakeMedia{})
	ctx := context.Background()
	id, _ := repo.CreateNote(ctx, domain.Note{OwnerKey: "owner", Lesson: lesson(), Body: "текст"})

	if err := s.SaveNote(ctx, "owner", id, "  параграф 5  "); err != nil {
		t.Fatal(err)
	}
	if !repo.notes[0].Saved() {
		t.Error("конспект не сохранён")
	}
	if len(repo.homeworks) != 1 {
		t.Fatalf("заданий: %d", len(repo.homeworks))
	}
	h := repo.homeworks[0]
	if h.Body != "параграф 5" || h.Origin != "manual" || h.Lesson.Discipline != "История" || !h.Saved() {
		t.Errorf("задание: %+v", h)
	}

	// Пустое поле — «не задавали»: конспект сохраняется, задания нет.
	id2, _ := repo.CreateNote(ctx, domain.Note{OwnerKey: "owner", Lesson: lesson(), Body: "ещё"})
	if err := s.SaveNote(ctx, "owner", id2, " "); err != nil || len(repo.homeworks) != 1 {
		t.Errorf("пустое задание: %v, заданий %d", err, len(repo.homeworks))
	}
	// Чужой конспект задание к себе не притянет.
	if err := s.SaveNote(ctx, "другой", id, "чужое"); err == nil {
		t.Error("задание вписано к чужому конспекту")
	}
}
