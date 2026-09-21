package domain

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Subject — тот, чьё расписание закрепили: группа или преподаватель.
//
// Одна сущность с двумя видами, а не два экрана: клиентом бывает и студент,
// и преподаватель, у которого собственное расписание ничем по смыслу не
// отличается от группового. Подписки, календарь и ссылки говорят на этом
// языке, поэтому он живёт в домене, а не в транспорте.
type Subject struct {
	Kind        SubjectKind
	Group       string
	LecturerOid int64
}

// SubjectKind — вид владельца расписания.
type SubjectKind string

const (
	SubjectNone     SubjectKind = ""
	SubjectGroup    SubjectKind = "group"
	SubjectLecturer SubjectKind = "lecturer"
)

// GroupSubject — расписание группы.
func GroupSubject(name string) Subject {
	return Subject{Kind: SubjectGroup, Group: strings.TrimSpace(name)}
}

// LecturerSubject — расписание преподавателя по числовому oid.
func LecturerSubject(oid int64) Subject {
	return Subject{Kind: SubjectLecturer, LecturerOid: oid}
}

// IsZero — расписание не выбрано.
func (s Subject) IsZero() bool { return s.Kind == SubjectNone }

// Key — устойчивый ключ: «group:ПИ24-1», «lecturer:46674».
// Одна строка вместо пары полей, чтобы подписки хранились единообразно.
func (s Subject) Key() string {
	switch s.Kind {
	case SubjectGroup:
		return "group:" + s.Group
	case SubjectLecturer:
		return "lecturer:" + strconv.FormatInt(s.LecturerOid, 10)
	}
	return ""
}

// Query — параметры ссылки на расписание.
func (s Subject) Query() string {
	switch s.Kind {
	case SubjectGroup:
		return "group=" + url.QueryEscape(s.Group)
	case SubjectLecturer:
		return "lecturer=" + strconv.FormatInt(s.LecturerOid, 10)
	}
	return ""
}

// ParseSubjectKey разбирает ключ обратно.
//
// Ключ преподавателя — только положительное число. Источник не отклоняет
// запрос по GUID, а возвращает по нему чужие пары; такой ключ не должен
// попасть в подписку даже из подделанной cookie.
func ParseSubjectKey(key string) (Subject, error) {
	kind, value, ok := strings.Cut(key, ":")
	if !ok || value == "" {
		return Subject{}, fmt.Errorf("некорректный ключ подписки %q", key)
	}
	switch SubjectKind(kind) {
	case SubjectGroup:
		return GroupSubject(value), nil
	case SubjectLecturer:
		oid, err := strconv.ParseInt(value, 10, 64)
		if err != nil || oid <= 0 {
			return Subject{}, fmt.Errorf("некорректный идентификатор преподавателя в ключе %q", key)
		}
		return LecturerSubject(oid), nil
	}
	return Subject{}, fmt.Errorf("неизвестный вид подписки %q", kind)
}

// SubjectFromValues достаёт владельца из параметров запроса.
func SubjectFromValues(values url.Values) (Subject, error) {
	if g := strings.TrimSpace(values.Get("group")); g != "" {
		return GroupSubject(g), nil
	}
	if l := strings.TrimSpace(values.Get("lecturer")); l != "" {
		oid, err := strconv.ParseInt(l, 10, 64)
		if err != nil || oid <= 0 {
			return Subject{}, fmt.Errorf("идентификатор преподавателя должен быть положительным числом")
		}
		return LecturerSubject(oid), nil
	}
	return Subject{}, nil
}
