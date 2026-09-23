// Package notes — записи пар, расшифровка и конспекты.
//
// Модуль владеет таблицами recordings, notes, homeworks. Пишет в них
// только он (ARCHITECTURE.md § 5).
//
// Модуль намеренно ничего не знает о schedule: пара приходит сюда слепком
// (domain.LessonRef), который собирает транспорт. Иначе конспект зависел
// бы от строки в lessons, а она живёт неделю и удаляется.
package notes

import (
	"context"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

// Repository — хранилище модуля. Реализация — infrastructure/postgres.
type Repository interface {
	CreateRecording(ctx context.Context, rec domain.Recording) (int64, error)
	Recording(ctx context.Context, owner string, id int64) (domain.Recording, bool, error)
	// CountChunk отмечает принятый кусок дозагрузки. Номер нужен, чтобы
	// повтор того же куска после обрыва связи не удваивал запись.
	CountChunk(ctx context.Context, id int64, seq int, size int64, path string) error
	// Enqueue закрывает приём и ставит запись в очередь вместе с пропусками.
	Enqueue(ctx context.Context, id int64, gaps []domain.Gap) error
	Recordings(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Recording, error)
	DeleteRecording(ctx context.Context, owner string, id int64) (string, error)

	// Claim берёт из очереди одну запись и помечает её как взятую в работу.
	// Реализация обязана блокировать строку: воркеров может быть несколько,
	// а расшифровывать одну пару дважды — впустую занимать процессор.
	Claim(ctx context.Context, now time.Time) (domain.Recording, bool, error)
	SetStatus(ctx context.Context, id int64, st domain.Status) error
	SetTranscript(ctx context.Context, id int64, transcript string, durationSec int) error
	// Complete закрывает обработку и забывает путь к аудио: файл к этому
	// моменту уже удалён с диска.
	Complete(ctx context.Context, id int64) error
	Fail(ctx context.Context, id int64, reason string, retryAt time.Time, giveUp bool) error

	CreateNote(ctx context.Context, n domain.Note) (int64, error)
	Note(ctx context.Context, owner string, id int64) (domain.Note, bool, error)
	NoteByRecording(ctx context.Context, owner string, recordingID int64) (domain.Note, bool, error)
	Notes(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Note, error)
	// DraftNotes — готовые, но не сохранённые конспекты предмета.
	DraftNotes(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Note, error)
	SaveNote(ctx context.Context, owner string, id int64, at time.Time) error
	DeleteNote(ctx context.Context, owner string, id int64) error

	CreateHomework(ctx context.Context, h domain.Homework) (int64, error)
	Homeworks(ctx context.Context, owner, subjectKey, discipline string, onlySaved bool) ([]domain.Homework, error)
	HomeworksByNote(ctx context.Context, owner string, noteID int64) ([]domain.Homework, error)
	SaveHomework(ctx context.Context, owner string, id int64, at time.Time) error
	SetHomeworkDone(ctx context.Context, owner string, id int64, at *time.Time) error
	DeleteHomework(ctx context.Context, owner string, id int64) error

	// Disciplines — предметы, по которым у человека уже что-то есть.
	// Список предметов группы приходит из расписания, но конспект переживает
	// окно сбора: предмет, пары которого на этой неделе нет, из раздела
	// пропадать не должен.
	Disciplines(ctx context.Context, owner, subjectKey string) ([]Discipline, error)

	// CleanupDrafts убирает несохранённые конспекты и задания: человек
	// посмотрел результат и ушёл, не нажав «Сохранить».
	CleanupDrafts(ctx context.Context, olderThan time.Duration) (int64, error)
	// StuckAudio — записи, брошенные на полпути: вкладку закрыли, дозагрузка
	// не кончилась. Их файлы надо убрать с диска.
	StuckAudio(ctx context.Context, olderThan time.Duration) ([]domain.Recording, error)
}

// Discipline — предмет в списке раздела «Пары».
type Discipline struct {
	Name       string
	Notes      int
	Homework   int // несделанных
	LastLesson time.Time
}

// Recognizer — порт распознавания речи.
//
// Реализация по умолчанию (infrastructure/asr) запускает sherpa-onnx с
// моделью GigaAM v3 на нашем же сервере: аудио никуда не уезжает, платить
// за минуты не нужно. Облачный распознаватель — другая реализация этого
// же порта, домен разницы не замечает.
type Recognizer interface {
	// Transcribe принимает путь к WAV 16 кГц моно и возвращает сегменты.
	Transcribe(ctx context.Context, wavPath string) ([]domain.Segment, error)
}

// Summarizer — порт конспектирования.
type Summarizer interface {
	Summarize(ctx context.Context, in SummaryInput) (domain.Recap, error)
}

// SummaryInput — что уходит модели.
//
// Здесь нет ни имени преподавателя, ни группы, и это не экономия токенов:
// расшифровка уходит во внешний сервис, и добавлять к ней имена живых
// людей незачем (docs/03-legal-risks.md § 4). Дисциплина нужна — без неё
// модель не понимает, о чём лекция.
type SummaryInput struct {
	Discipline  string
	Date        string
	DurationSec int
	Transcript  string
	// Gaps — где запись прерывалась; в Transcript на их местах стоят метки.
	Gaps []domain.Gap
}

// Media — порт подготовки звука: что бы браузер ни записал (webm/opus у
// Chrome, mp4/aac у Safari), распознаванию нужен WAV 16 кГц моно.
type Media interface {
	ToWav(ctx context.Context, src, dst string) (domain.Sound, error)
}
