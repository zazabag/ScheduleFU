package domain

import "time"

// ChangeKind — вид изменения расписания.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
	ChangeChanged ChangeKind = "changed"
)

// Change — обнаруженное изменение: то, чего у источника нет вовсе.
// Адресные поля продублированы: у удалённой пары брать их уже неоткуда.
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

// Subjects — все владельцы расписаний, кого касается изменение.
func (c Change) Subjects() []Subject {
	var out []Subject
	for _, g := range c.GroupNames {
		if g != "" {
			out = append(out, GroupSubject(g))
		}
	}
	if c.LecturerOid != nil && *c.LecturerOid > 0 {
		out = append(out, LecturerSubject(*c.LecturerOid))
	}
	return out
}

// ApplyResult — итог применения слепка.
type ApplyResult struct {
	Seen, Added, Removed, Changed int
}

// Changes — общее число изменений.
func (r ApplyResult) Changes() int { return r.Added + r.Removed + r.Changed }
