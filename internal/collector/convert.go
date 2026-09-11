// Package collector — фоновый сбор расписания из ruz.fa.ru в наше хранилище.
package collector

import (
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/ruz"
	"github.com/zazabag/schedulefu/internal/store"
)

// moscow — часовой пояс вуза. Источник отдаёт время без зоны, и трактовать
// его нужно как московское, иначе на сервере в другом поясе «свободна
// сейчас» будет считаться неверно.
var moscow = mustLoadMoscow()

func mustLoadMoscow() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		// На системах без базы часовых поясов подставляем фиксированный UTC+3.
		return time.FixedZone("MSK", 3*60*60)
	}
	return loc
}

// ToStoreLesson переводит пару из формата источника во внутренний.
func ToStoreLesson(l ruz.Lesson) (store.Lesson, bool) {
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(l.Date), moscow)
	if err != nil {
		// Пара без разбираемой даты бесполезна: её некуда поместить.
		return store.Lesson{}, false
	}
	if l.LessonOid == 0 {
		// Без идентификатора пару нельзя сопоставить между слепками,
		// а значит нельзя и отследить её изменение.
		return store.Lesson{}, false
	}
	// Источник помечает удалённые записи, но иногда всё равно их отдаёт.
	if l.DeletionMark != 0 {
		return store.Lesson{}, false
	}

	out := store.Lesson{
		LessonOid:    l.LessonOid,
		Date:         date,
		BeginsAt:     normalizeTime(l.BeginLesson),
		EndsAt:       normalizeTime(l.EndLesson),
		Auditorium:   strings.TrimSpace(l.Auditorium),
		Building:     strings.TrimSpace(l.Building),
		Discipline:   strings.TrimSpace(l.Discipline),
		KindOfWork:   strings.TrimSpace(l.KindOfWork),
		LecturerName: strings.TrimSpace(l.Lecturer),
		Stream:       strings.TrimSpace(l.Stream),
		GroupNames:   ParseStream(l.Stream, l.Group),
		Subgroup:     strings.TrimSpace(l.SubGroup),
		Note:         strings.TrimSpace(l.Note),
	}
	if out.BeginsAt == "" || out.EndsAt == "" {
		return store.Lesson{}, false
	}
	if l.AuditoriumOid > 0 {
		oid := l.AuditoriumOid
		out.AuditoriumOid = &oid
	}
	if l.LecturerOid > 0 {
		oid := l.LecturerOid
		out.LecturerOid = &oid
	}
	if ts, ok := parseSourceTime(l.ModifiedDate); ok {
		out.SourceModifiedAt = &ts
	}
	return out, true
}

// ParseStream разбирает поле потока в список групп.
//
// У пары поле group почти всегда пустое, а состав групп лежит в stream
// строкой вида "ПИ24-1; ПИ24-2; ПИ24-3". По этому списку адресуются
// уведомления, поэтому разбор должен быть устойчив к лишним пробелам и
// разным разделителям.
func ParseStream(stream, group string) []string {
	raw := stream
	if strings.TrimSpace(raw) == "" {
		raw = group
	}
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ';' || r == ',' || r == '\n'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// normalizeTime приводит время пары к HH:MM.
func normalizeTime(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 && s[2] == ':' {
		return s[:5]
	}
	return ""
}

// parseSourceTime разбирает временные метки источника.
//
// Источник отдаёт их в нестандартном виде "2026-09-09T10:51:08Z00:00":
// это не RFC3339, у которого либо Z, либо смещение, но не оба сразу.
// Разбираем без зоны и считаем московским временем.
func parseSourceTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if i := strings.Index(s, "Z"); i > 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "+"); i > 0 {
		s = s[:i]
	}
	ts, err := time.ParseInLocation("2006-01-02T15:04:05", s, moscow)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
