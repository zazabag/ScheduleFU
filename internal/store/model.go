package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Lesson — пара в нормализованном виде, пригодном для хранения.
type Lesson struct {
	LessonOid     int64
	Date          time.Time
	BeginsAt      string // HH:MM
	EndsAt        string // HH:MM
	AuditoriumOid *int64
	Auditorium    string
	Building      string
	Discipline    string
	KindOfWork    string
	LecturerOid   *int64
	LecturerName  string
	Stream        string
	GroupNames    []string
	Subgroup      string
	Note          string

	SourceModifiedAt *time.Time
}

// Fingerprint — отпечаток значимых полей пары.
//
// Сравнивать слепки поле за полем дорого и легко забыть новое поле, поэтому
// сравнение идёт по отпечатку. Сюда входит только то, изменение чего человек
// заметит и о чём его имеет смысл уведомить: служебные поля источника
// (кто правил, когда создана запись) намеренно не участвуют — иначе каждая
// техническая правка деканата порождала бы ложное уведомление.
func (l Lesson) Fingerprint() string {
	var b strings.Builder
	write := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
			b.WriteByte('\x1f') // разделитель, не встречающийся в данных
		}
	}
	aud, lec := "", ""
	if l.AuditoriumOid != nil {
		aud = strconv.FormatInt(*l.AuditoriumOid, 10)
	}
	if l.LecturerOid != nil {
		lec = strconv.FormatInt(*l.LecturerOid, 10)
	}
	write(
		l.Date.Format("2006-01-02"),
		l.BeginsAt, l.EndsAt,
		aud, l.Auditorium,
		l.Discipline, l.KindOfWork,
		lec, l.LecturerName,
		strings.Join(l.GroupNames, ","),
		l.Subgroup, l.Note,
	)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// Auditorium — аудитория в справочнике.
type Auditorium struct {
	Oid          int64
	Name         string
	Prefix       string
	Room         string
	Building     string
	Campus       string
	Kind         string
	Floor        *int
	Capacity     *int
	IsStudySpace bool
}

// Group — учебная группа.
type Group struct {
	ID            int64
	Name          string
	FacultyOid    string
	AdmissionYear *int
}

// Lecturer — преподаватель.
type Lecturer struct {
	Oid  int64
	Name string
}

// ChangeKind — вид изменения в расписании.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
	ChangeChanged ChangeKind = "changed"
)

// Change — обнаруженное изменение расписания.
type Change struct {
	ID            int64
	LessonOid     int64
	Kind          ChangeKind
	DetectedAt    time.Time
	LessonDate    time.Time
	GroupNames    []string
	LecturerOid   *int64
	AuditoriumOid *int64
	Before        *Lesson
	After         *Lesson
}

// ApplyResult — итог применения слепка.
type ApplyResult struct {
	Seen    int
	Added   int
	Removed int
	Changed int
}

// Changes — общее число изменений.
func (r ApplyResult) Changes() int { return r.Added + r.Removed + r.Changed }
