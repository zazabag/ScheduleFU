// Package domain — язык модуля notes: запись пары, её обработка, конспект,
// домашнее задание.
//
// Пакет не импортирует ничего из проекта. В частности, он не знает о модуле
// schedule: пара здесь — не строка таблицы lessons, а слепок (LessonRef),
// снятый в момент записи. Расписание живёт неделю и чистится, а конспект
// нужен к сессии.
package domain

import (
	"errors"
	"strings"
	"time"
)

// Status — стадия обработки записи.
//
// Порядок линейный: пока файл едет на сервер — Uploading; дальше очередь и
// три шага обработки; конец — Ready или Failed.
type Status string

const (
	StatusUploading    Status = "uploading"
	StatusQueued       Status = "queued"
	StatusDecoding     Status = "decoding"
	StatusTranscribing Status = "transcribing"
	StatusSummarizing  Status = "summarizing"
	StatusReady        Status = "ready"
	StatusFailed       Status = "failed"
)

// Label — стадия по-русски, как её видит человек на экране.
func (s Status) Label() string {
	switch s {
	case StatusUploading:
		return "загружается"
	case StatusQueued:
		return "в очереди"
	case StatusDecoding:
		return "готовим звук"
	case StatusTranscribing:
		return "расшифровываем"
	case StatusSummarizing:
		return "пишем конспект"
	case StatusReady:
		return "готово"
	case StatusFailed:
		return "не получилось"
	}
	return string(s)
}

// Done сообщает, что обработке дальше делать нечего.
func (s Status) Done() bool { return s == StatusReady || s == StatusFailed }

// Working сообщает, что запись сейчас в работе у воркера.
func (s Status) Working() bool {
	return s == StatusQueued || s == StatusDecoding || s == StatusTranscribing || s == StatusSummarizing
}

// Origin — откуда взялся файл.
type Origin string

const (
	// OriginRecord — писали прямо в приложении.
	OriginRecord Origin = "record"
	// OriginUpload — принесли готовый файл: диктофоном телефона, с ноутбука.
	// Для айфона это основной путь, см. docs/07-notes-module.md § «Фон».
	OriginUpload Origin = "upload"
)

// LessonRef — слепок пары на момент записи.
//
// Всё, что нужно, чтобы через полгода понять, к чему относится конспект,
// и чтобы разложить конспекты по предметам. Намеренно самодостаточен:
// ни одного указателя в таблицы модуля schedule.
type LessonRef struct {
	SubjectKey   string // чьё расписание открывали: group:ПИ24-1
	Discipline   string
	Date         time.Time
	BeginsAt     string // ЧЧ:ММ, может быть пустым у загруженного файла
	EndsAt       string
	LecturerName string
	Auditorium   string
	KindOfWork   string
	LessonOid    *int64 // справочно; не ключ
}

// ErrNoDiscipline — запись без предмета не к чему привязать.
var ErrNoDiscipline = errors.New("не указан предмет")

// Validate проверяет то, без чего запись бессмысленна.
func (l LessonRef) Validate() error {
	switch {
	case strings.TrimSpace(l.SubjectKey) == "":
		return errors.New("не указано, чьё это расписание")
	case strings.TrimSpace(l.Discipline) == "":
		return ErrNoDiscipline
	case l.Date.IsZero():
		return errors.New("не указана дата пары")
	}
	return nil
}

// DateKey — дата пары как «ГГГГ-ММ-ДД».
func (l LessonRef) DateKey() string { return l.Date.Format("2006-01-02") }

// Recording — запись пары и состояние её обработки.
type Recording struct {
	ID       int64
	OwnerKey string
	Lesson   LessonRef
	Origin   Origin
	Status   Status

	Chunks      int
	Bytes       int64
	DurationSec int
	AudioPath   string
	Transcript  string

	Attempts  int
	Failure   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasAudio сообщает, лежит ли ещё файл на диске. После успешной расшифровки
// не лежит: запись голоса удаляется, остаётся текст.
func (r Recording) HasAudio() bool { return r.AudioPath != "" }

// Duration — длительность в человеческом виде: «1 ч 32 мин».
func (r Recording) Duration() string { return HumanDuration(r.DurationSec) }

// HumanDuration переводит секунды в «1 ч 32 мин» или «12 мин».
func HumanDuration(sec int) string {
	if sec <= 0 {
		return ""
	}
	m := (sec + 30) / 60
	if m < 60 {
		return itoa(m) + " мин"
	}
	h, rest := m/60, m%60
	if rest == 0 {
		return itoa(h) + " ч"
	}
	return itoa(h) + " ч " + itoa(rest) + " мин"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Segment — кусок расшифровки с временем от начала записи.
//
// Время нужно не для показа, а для конспекта: модель опирается на порядок
// и длительность, чтобы отличить отступление от темы занятия.
type Segment struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// Transcript склеивает сегменты в сплошной текст.
func Transcript(segs []Segment) string {
	var b strings.Builder
	for i, s := range segs {
		t := strings.TrimSpace(s.Text)
		if t == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
	}
	return b.String()
}

// Note — конспект пары.
type Note struct {
	ID          int64
	OwnerKey    string
	RecordingID *int64
	Lesson      LessonRef
	Title       string
	// Body — размеченный текст: абзацы, «## подзаголовок», «- пункт».
	// Разметка своя и заведомо бедная: текст пришёл от модели, и полноценный
	// markdown в шаблоне пришлось бы либо экранировать, либо доверять.
	Body      string
	Theses    []string
	SavedAt   *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Saved сообщает, нажал ли человек «Сохранить»: до этого конспект — черновик.
func (n Note) Saved() bool { return n.SavedAt != nil }

// Homework — домашнее задание.
type Homework struct {
	ID        int64
	OwnerKey  string
	NoteID    *int64
	Lesson    LessonRef
	Body      string
	DueDate   *time.Time
	DueNote   string // «к следующему занятию» — срок словами, датой не является
	Origin    string // ai | manual
	DoneAt    *time.Time
	SavedAt   *time.Time
	CreatedAt time.Time
}

// Done — задание отмечено выполненным.
func (h Homework) Done() bool { return h.DoneAt != nil }

// Saved — задание перенесено в раздел домашних заданий.
func (h Homework) Saved() bool { return h.SavedAt != nil }

// Due — срок в человеческом виде: дата, если она есть, иначе слова модели.
func (h Homework) Due() string {
	if h.DueDate != nil {
		return h.DueDate.Format("02.01.2006")
	}
	return h.DueNote
}

// Recap — то, что модель вернула по расшифровке.
//
// Тезисы и домашние задания отдельными полями, а не абзацем внутри
// конспекта: по ним строятся разные экраны, и разбирать их обратно из
// текста было бы гаданием.
type Recap struct {
	Title    string
	Body     string
	Theses   []string
	Homework []RecapHomework
}

// RecapHomework — задание, как его увидела модель.
type RecapHomework struct {
	Text    string
	DueNote string
}

// Clean приводит ответ модели в пригодный вид: режет пустое, обрезает
// заголовок. Модель иногда отвечает пустыми строками и заголовком на
// три предложения — это не ошибка обработки, а обычное её поведение.
func (r Recap) Clean() Recap {
	out := Recap{Title: strings.TrimSpace(r.Title), Body: strings.TrimSpace(r.Body)}
	if runes := []rune(out.Title); len(runes) > 120 {
		out.Title = strings.TrimSpace(string(runes[:120])) + "…"
	}
	for _, t := range r.Theses {
		if t = strings.TrimSpace(t); t != "" {
			out.Theses = append(out.Theses, t)
		}
	}
	for _, h := range r.Homework {
		if body := strings.TrimSpace(h.Text); body != "" {
			out.Homework = append(out.Homework, RecapHomework{Text: body, DueNote: strings.TrimSpace(h.DueNote)})
		}
	}
	return out
}
