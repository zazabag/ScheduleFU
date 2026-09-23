package notes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Options — настройки модуля.
type Options struct {
	// AudioDir — куда кладутся файлы, пока они нужны. Каталог наполняется
	// временно: после успешной расшифровки файл удаляется.
	AudioDir string
	// MaxBytes — потолок на запись. Пара в Opus 24 кбит/с — около 16 МБ;
	// потолок ловит не злоупотребление, а забытую включённой запись.
	MaxBytes int64
	// MaxMinutes — потолок длительности. Пара идёт полтора часа, но бывают
	// сдвоенные; всё, что длиннее, почти наверняка забыли выключить.
	MaxMinutes int
	// Retry — через сколько повторить обработку после ошибки.
	Retry time.Duration
	// Attempts — сколько раз пробовать, прежде чем сдаться.
	Attempts int
	// DraftTTL — сколько живёт несохранённый конспект.
	DraftTTL time.Duration
	// KeepAudio оставляет запись на диске после расшифровки. По умолчанию
	// выключено: голос преподавателя у нас не хранится.
	KeepAudio bool
}

// Service — записи и конспекты.
type Service struct {
	repo  Repository
	rec   Recognizer
	sum   Summarizer
	media Media
	clk   *clock.Clock
	log   *slog.Logger
	opts  Options
}

// New собирает модуль. Распознаватель и конспектирование — порты: без них
// раздел работает как хранилище конспектов, написанных руками, а не падает.
func New(repo Repository, rec Recognizer, sum Summarizer, media Media, clk *clock.Clock, log *slog.Logger, opts Options) *Service {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = 512 << 20
	}
	if opts.MaxMinutes <= 0 {
		opts.MaxMinutes = 240
	}
	if opts.Retry <= 0 {
		opts.Retry = 10 * time.Minute
	}
	if opts.Attempts <= 0 {
		opts.Attempts = 3
	}
	if opts.DraftTTL <= 0 {
		opts.DraftTTL = 14 * 24 * time.Hour
	}
	return &Service{repo: repo, rec: rec, sum: sum, media: media, clk: clk, log: log, opts: opts}
}

// CanProcess сообщает, настроена ли обработка. Без моделей запись принимать
// нечестно: человек проговорит полтора часа и получит вечное «в очереди».
func (s *Service) CanProcess() bool { return s.rec != nil && s.sum != nil && s.media != nil }

// ─── приём записи ────────────────────────────────────────────────────────────

// Start заводит запись и возвращает её. Файл ещё не создан: он появится с
// первым куском.
func (s *Service) Start(ctx context.Context, owner string, lesson domain.LessonRef, origin domain.Origin) (domain.Recording, error) {
	if strings.TrimSpace(owner) == "" {
		return domain.Recording{}, errors.New("нет ключа владельца")
	}
	if err := lesson.Validate(); err != nil {
		return domain.Recording{}, err
	}
	rec := domain.Recording{OwnerKey: owner, Lesson: lesson, Origin: origin, Status: domain.StatusUploading}
	id, err := s.repo.CreateRecording(ctx, rec)
	if err != nil {
		return domain.Recording{}, err
	}
	rec.ID = id
	rec.AudioPath = s.path(id)
	return rec, nil
}

// Append дописывает кусок записи в файл.
//
// Куски идут по порядку, и порядок важен: браузер пишет один непрерывный
// поток, разрезанный по времени, и только склейка в исходном порядке снова
// даёт пригодный файл. Повтор уже принятого куска (связь оборвалась после
// записи, но до ответа) отбрасывается молча — иначе кусок попал бы в файл
// дважды.
func (s *Service) Append(ctx context.Context, owner string, id int64, seq int, body io.Reader) (domain.Recording, error) {
	rec, ok, err := s.repo.Recording(ctx, owner, id)
	if err != nil || !ok {
		return domain.Recording{}, notFound(err)
	}
	if rec.Status != domain.StatusUploading {
		return rec, errors.New("запись уже закрыта")
	}
	switch {
	case seq < rec.Chunks:
		return rec, nil // этот кусок уже приняли
	case seq > rec.Chunks:
		return rec, fmt.Errorf("кусок %d пришёл раньше времени, ждём %d", seq, rec.Chunks)
	}
	if rec.Bytes >= s.opts.MaxBytes {
		return rec, errors.New("запись длиннее допустимого")
	}
	if err := os.MkdirAll(s.opts.AudioDir, 0o700); err != nil {
		return rec, fmt.Errorf("notes: каталог записей: %w", err)
	}
	f, err := os.OpenFile(s.path(id), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return rec, fmt.Errorf("notes: файл записи: %w", err)
	}
	defer f.Close()
	// Потолок считается по остатку: иначе один кусок способен принести
	// гигабайт, и проверка «до» ничего не даст.
	n, err := io.Copy(f, io.LimitReader(body, s.opts.MaxBytes-rec.Bytes+1))
	if err != nil {
		return rec, fmt.Errorf("notes: приём куска: %w", err)
	}
	if err := s.repo.CountChunk(ctx, id, seq, n, s.path(id)); err != nil {
		return rec, err
	}
	rec.Chunks, rec.Bytes = seq+1, rec.Bytes+n
	return rec, nil
}

// Finish закрывает приём и ставит запись в очередь обработки. gaps — где
// система выключала микрофон, как их заметил браузер; у загруженного файла
// их нет.
func (s *Service) Finish(ctx context.Context, owner string, id int64, gaps []domain.Gap) (domain.Recording, error) {
	rec, ok, err := s.repo.Recording(ctx, owner, id)
	if err != nil || !ok {
		return domain.Recording{}, notFound(err)
	}
	if rec.Status != domain.StatusUploading {
		return rec, nil
	}
	if rec.Bytes == 0 {
		_, _ = s.repo.DeleteRecording(ctx, owner, id)
		return rec, errors.New("запись пустая: звук не дошёл")
	}
	gaps = domain.CleanGaps(gaps)
	if err := s.repo.Enqueue(ctx, id, gaps); err != nil {
		return rec, err
	}
	rec.Status, rec.Gaps = domain.StatusQueued, gaps
	return rec, nil
}

func (s *Service) path(id int64) string {
	return filepath.Join(s.opts.AudioDir, fmt.Sprintf("rec-%d.bin", id))
}

func notFound(err error) error {
	if err != nil {
		return err
	}
	return errors.New("запись не найдена")
}

// ─── обработка ───────────────────────────────────────────────────────────────

// Run крутит обработку, пока жив контекст. Отдельная команда, а не горутина
// в serve: расшифровка занимает все ядра на десяток минут, и HTTP рядом с
// ней начинает отвечать секундами.
func (s *Service) Run(ctx context.Context, idle time.Duration) {
	t := time.NewTicker(idle)
	defer t.Stop()
	var swept time.Time
	for {
		done, err := s.ProcessOne(ctx)
		if err != nil {
			s.log.Error("notes: обработка", "ошибка", err)
		}
		if s.clk.Now().Sub(swept) > time.Hour {
			s.sweep(ctx)
			swept = s.clk.Now()
		}
		if done {
			// Очередь непустая — сразу за следующей, без паузы.
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// sweep убирает мусор: брошенные на полпути файлы и черновики, которые
// никто не сохранил.
func (s *Service) sweep(ctx context.Context) {
	if n, err := s.repo.CleanupDrafts(ctx, s.opts.DraftTTL); err != nil {
		s.log.Error("notes: уборка черновиков", "ошибка", err)
	} else if n > 0 {
		s.log.Info("notes: убраны черновики", "сколько", n)
	}
	stuck, err := s.repo.StuckAudio(ctx, 24*time.Hour)
	if err != nil {
		s.log.Error("notes: поиск брошенных записей", "ошибка", err)
		return
	}
	for _, rec := range stuck {
		s.dropAudio(rec.ID)
		if _, err := s.repo.DeleteRecording(ctx, rec.OwnerKey, rec.ID); err != nil {
			s.log.Error("notes: удаление брошенной записи", "запись", rec.ID, "ошибка", err)
		}
	}
}

// ProcessOne берёт из очереди одну запись и доводит её до конспекта.
// Возвращает true, если работа была.
func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	if !s.CanProcess() {
		return false, nil
	}
	rec, ok, err := s.repo.Claim(ctx, s.clk.Now())
	if err != nil || !ok {
		return false, err
	}
	s.log.Info("notes: обработка записи", "запись", rec.ID, "предмет", rec.Lesson.Discipline,
		"байт", rec.Bytes, "кусков", rec.Chunks, "откуда", rec.Origin)
	if err := s.process(ctx, rec); err != nil {
		// Тишина от повтора не станет речью: такие ошибки не ждут десять
		// минут в очереди, а сразу показываются человеку.
		var perm permanentError
		giveUp := rec.Attempts+1 >= s.opts.Attempts || errors.As(err, &perm)
		if giveUp {
			// Сдались — файл больше не нужен, а держать чужой голос «на
			// всякий случай» мы не будем.
			s.dropAudio(rec.ID)
		}
		if ferr := s.repo.Fail(ctx, rec.ID, err.Error(), s.clk.Now().Add(s.opts.Retry), giveUp); ferr != nil {
			s.log.Error("notes: отметка ошибки", "запись", rec.ID, "ошибка", ferr)
		}
		return true, fmt.Errorf("запись %d: %w", rec.ID, err)
	}
	return true, nil
}

func (s *Service) process(ctx context.Context, rec domain.Recording) error {
	transcript := rec.Transcript
	// Расшифровка могла остаться от прошлой попытки: конспект не получился,
	// но пересчитывать полтора часа звука ради этого незачем.
	if strings.TrimSpace(transcript) == "" {
		text, dur, err := s.transcribe(ctx, rec)
		if err != nil {
			return err
		}
		if err := s.repo.SetTranscript(ctx, rec.ID, text, dur); err != nil {
			return err
		}
		transcript, rec.DurationSec = text, dur
	}
	// Дальше нужен только текст. Удаление снаружи ветки не случайно: если
	// прошлая попытка упала уже после расшифровки, файл остался на диске, и
	// второй раз сюда мы зайдём мимо распознавания.
	if !s.opts.KeepAudio {
		s.dropAudio(rec.ID)
	}

	if err := s.repo.SetStatus(ctx, rec.ID, domain.StatusSummarizing); err != nil {
		return err
	}
	recap, err := s.sum.Summarize(ctx, SummaryInput{
		Discipline:  rec.Lesson.Discipline,
		Date:        rec.Lesson.DateKey(),
		DurationSec: rec.DurationSec,
		Transcript:  transcript,
		Gaps:        rec.Gaps,
	})
	if err != nil {
		return fmt.Errorf("конспект: %w", err)
	}
	recap = recap.Clean()
	if recap.Body == "" {
		return errors.New("модель вернула пустой конспект")
	}
	if err := s.storeRecap(ctx, rec, recap); err != nil {
		return err
	}
	return s.repo.Complete(ctx, rec.ID)
}

func (s *Service) transcribe(ctx context.Context, rec domain.Recording) (string, int, error) {
	src := s.path(rec.ID)
	if _, err := os.Stat(src); err != nil {
		return "", 0, fmt.Errorf("файл записи пропал: %w", err)
	}
	if err := s.repo.SetStatus(ctx, rec.ID, domain.StatusDecoding); err != nil {
		return "", 0, err
	}
	wav := strings.TrimSuffix(src, ".bin") + ".wav"
	snd, err := s.media.ToWav(ctx, src, wav)
	if err != nil {
		// Битый контейнер при повторе битым и останется.
		return "", 0, permanentError{fmt.Errorf("подготовка звука: %w", err)}
	}
	defer os.Remove(wav)
	dur := snd.DurationSec
	s.log.Info("notes: звук подготовлен", "запись", rec.ID, "секунд", dur, "пик_дБ", fmt.Sprintf("%.1f", snd.PeakDB))
	if snd.Silent() {
		return "", 0, permanentError{fmt.Errorf("микрофон писал тишину: в записи %s звука, но громче шума нет ничего. "+
			"Проверьте, что браузеру разрешён микрофон и телефон не лежит микрофоном вниз", domain.HumanSeconds(dur))}
	}
	if dur > s.opts.MaxMinutes*60 {
		return "", 0, fmt.Errorf("запись длиной %s: похоже, её забыли выключить", domain.HumanDuration(dur))
	}
	if err := s.repo.SetStatus(ctx, rec.ID, domain.StatusTranscribing); err != nil {
		return "", 0, err
	}
	segs, err := s.rec.Transcribe(ctx, wav)
	if err != nil {
		return "", 0, fmt.Errorf("расшифровка: %w", err)
	}
	text := domain.TranscriptWithGaps(segs, rec.Gaps)
	if strings.TrimSpace(domain.Transcript(segs)) == "" {
		return "", 0, permanentError{emptyReason(rec, snd)}
	}
	return text, dur, nil
}

// storeRecap кладёт конспект и задания черновиками: сохранит их человек.
func (s *Service) storeRecap(ctx context.Context, rec domain.Recording, recap domain.Recap) error {
	// Повторная обработка не должна плодить конспекты.
	if old, ok, err := s.repo.NoteByRecording(ctx, rec.OwnerKey, rec.ID); err != nil {
		return err
	} else if ok {
		if err := s.repo.DeleteNote(ctx, rec.OwnerKey, old.ID); err != nil {
			return err
		}
	}
	id := rec.ID
	note := domain.Note{OwnerKey: rec.OwnerKey, RecordingID: &id, Lesson: rec.Lesson,
		Title: recap.Title, Body: recap.Body, Theses: recap.Theses}
	noteID, err := s.repo.CreateNote(ctx, note)
	if err != nil {
		return err
	}
	for _, h := range recap.Homework {
		hw := domain.Homework{OwnerKey: rec.OwnerKey, NoteID: &noteID, Lesson: rec.Lesson,
			Body: h.Text, DueNote: h.DueNote, Origin: "ai"}
		if _, err := s.repo.CreateHomework(ctx, hw); err != nil {
			return err
		}
	}
	return nil
}

// permanentError — ошибка, которую повтор не исправит.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// chunkSec — длина куска записи из браузера (record.js, recorder.start).
const chunkSec = 15

// emptyReason объясняет пустую расшифровку громкой записи. Если из файла
// прочиталось заметно меньше, чем браузер прислал кусков, звук дошёл не
// целиком — это надо назвать, иначе человек будет чинить микрофон.
func emptyReason(rec domain.Recording, snd domain.Sound) error {
	if rec.Origin == domain.OriginRecord && rec.Chunks >= 2 {
		expected := rec.Chunks * chunkSec
		if snd.DurationSec < (rec.Chunks-1)*chunkSec/2 {
			return fmt.Errorf("из записи прочиталось только %s звука из примерно %s: запись дошла не целиком, и речи в этом обрывке нет",
				domain.HumanSeconds(snd.DurationSec), domain.HumanSeconds(expected))
		}
	}
	return fmt.Errorf("в записи %s звука, но речи в ней не распознано: возможно, говорили слишком далеко от телефона",
		domain.HumanSeconds(snd.DurationSec))
}

func (s *Service) dropAudio(id int64) {
	if err := os.Remove(s.path(id)); err != nil && !os.IsNotExist(err) {
		s.log.Error("notes: удаление записи", "запись", id, "ошибка", err)
	}
}

// ─── чтение и правка ─────────────────────────────────────────────────────────

// Disciplines — предметы, по которым у человека уже что-то есть.
func (s *Service) Disciplines(ctx context.Context, owner, subjectKey string) ([]Discipline, error) {
	return s.repo.Disciplines(ctx, owner, subjectKey)
}

// Recordings — записи по предмету, свежие первыми.
func (s *Service) Recordings(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Recording, error) {
	return s.repo.Recordings(ctx, owner, subjectKey, discipline)
}

// Recording — одна запись владельца.
func (s *Service) Recording(ctx context.Context, owner string, id int64) (domain.Recording, bool, error) {
	return s.repo.Recording(ctx, owner, id)
}

// Notes — конспекты по предмету.
func (s *Service) Notes(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Note, error) {
	return s.repo.Notes(ctx, owner, subjectKey, discipline)
}

// Note — один конспект.
func (s *Service) Note(ctx context.Context, owner string, id int64) (domain.Note, bool, error) {
	return s.repo.Note(ctx, owner, id)
}

// NoteByRecording — конспект, сделанный по записи.
func (s *Service) NoteByRecording(ctx context.Context, owner string, recordingID int64) (domain.Note, bool, error) {
	return s.repo.NoteByRecording(ctx, owner, recordingID)
}

// SaveNote переводит конспект из черновика в сохранённые.
func (s *Service) SaveNote(ctx context.Context, owner string, id int64) error {
	return s.repo.SaveNote(ctx, owner, id, s.clk.Now())
}

// DeleteNote убирает конспект вместе с его заданиями-черновиками.
func (s *Service) DeleteNote(ctx context.Context, owner string, id int64) error {
	return s.repo.DeleteNote(ctx, owner, id)
}

// DeleteRecording убирает запись и её файл.
func (s *Service) DeleteRecording(ctx context.Context, owner string, id int64) error {
	path, err := s.repo.DeleteRecording(ctx, owner, id)
	if err != nil {
		return err
	}
	if path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Homeworks — задания по предмету. onlySaved оставляет только перенесённые
// в раздел домашних заданий, без черновиков из свежего конспекта.
func (s *Service) Homeworks(ctx context.Context, owner, subjectKey, discipline string, onlySaved bool) ([]domain.Homework, error) {
	return s.repo.Homeworks(ctx, owner, subjectKey, discipline, onlySaved)
}

// HomeworksByNote — задания, выделенные из конспекта.
func (s *Service) HomeworksByNote(ctx context.Context, owner string, noteID int64) ([]domain.Homework, error) {
	return s.repo.HomeworksByNote(ctx, owner, noteID)
}

// SaveHomework переносит задание в раздел домашних заданий.
func (s *Service) SaveHomework(ctx context.Context, owner string, id int64) error {
	return s.repo.SaveHomework(ctx, owner, id, s.clk.Now())
}

// AddHomework вписывает задание руками — например, когда записи не было.
func (s *Service) AddHomework(ctx context.Context, owner string, lesson domain.LessonRef, body string) error {
	if strings.TrimSpace(body) == "" {
		return errors.New("пустое задание")
	}
	if err := lesson.Validate(); err != nil {
		return err
	}
	id, err := s.repo.CreateHomework(ctx, domain.Homework{OwnerKey: owner, Lesson: lesson,
		Body: strings.TrimSpace(body), Origin: "manual"})
	if err != nil {
		return err
	}
	// Вписанное руками сохранено по определению: человек его уже написал.
	return s.repo.SaveHomework(ctx, owner, id, s.clk.Now())
}

// ToggleHomework отмечает задание сделанным и обратно.
func (s *Service) ToggleHomework(ctx context.Context, owner string, id int64, done bool) error {
	if !done {
		return s.repo.SetHomeworkDone(ctx, owner, id, nil)
	}
	now := s.clk.Now()
	return s.repo.SetHomeworkDone(ctx, owner, id, &now)
}

// DeleteHomework убирает задание.
func (s *Service) DeleteHomework(ctx context.Context, owner string, id int64) error {
	return s.repo.DeleteHomework(ctx, owner, id)
}
