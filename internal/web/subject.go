package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Subject — то, чьё расписание человек закрепил за собой.
//
// Клиентом сервиса может быть и студент, и преподаватель: у преподавателя
// есть собственное расписание, ничем не отличающееся по смыслу от
// группового. Поэтому «моё расписание» — одна сущность с двумя видами, а
// не два отдельных экрана: подписка, уведомления и ссылки работают для
// обоих одинаково.
type Subject struct {
	Kind SubjectKind
	// Group заполняется для вида SubjectGroup: имя группы и есть ключ.
	Group string
	// LecturerOid заполняется для вида SubjectLecturer.
	//
	// Ключ преподавателя — только числовой oid: запрос расписания по GUID
	// источник не отклоняет, а возвращает чужие пары, см. docs/06.
	LecturerOid int64
	// Label — как это называется в интерфейсе.
	Label string
}

// SubjectKind — вид владельца расписания.
type SubjectKind string

const (
	SubjectNone     SubjectKind = ""
	SubjectGroup    SubjectKind = "group"
	SubjectLecturer SubjectKind = "lecturer"
)

// IsZero сообщает, что расписание ещё не выбрано.
func (s Subject) IsZero() bool { return s.Kind == SubjectNone }

// Key — устойчивый ключ подписки: «group:ПИ24-1» или «lecturer:46674».
//
// Одна строка вместо пары полей нужна, чтобы подписки хранились и
// сравнивались единообразно, независимо от вида.
func (s Subject) Key() string {
	switch s.Kind {
	case SubjectGroup:
		return "group:" + s.Group
	case SubjectLecturer:
		return "lecturer:" + strconv.FormatInt(s.LecturerOid, 10)
	}
	return ""
}

// ParseSubjectKey разбирает ключ подписки обратно.
func ParseSubjectKey(key string) (Subject, error) {
	kind, value, ok := strings.Cut(key, ":")
	if !ok || value == "" {
		return Subject{}, fmt.Errorf("некорректный ключ подписки %q", key)
	}
	switch SubjectKind(kind) {
	case SubjectGroup:
		return Subject{Kind: SubjectGroup, Group: value, Label: value}, nil
	case SubjectLecturer:
		oid, err := strconv.ParseInt(value, 10, 64)
		if err != nil || oid <= 0 {
			return Subject{}, fmt.Errorf("некорректный идентификатор преподавателя в ключе %q", key)
		}
		return Subject{Kind: SubjectLecturer, LecturerOid: oid}, nil
	}
	return Subject{}, fmt.Errorf("неизвестный вид подписки %q", kind)
}

// Query возвращает параметры ссылки на расписание этого владельца.
func (s Subject) Query() string {
	switch s.Kind {
	case SubjectGroup:
		return "group=" + url.QueryEscape(s.Group)
	case SubjectLecturer:
		return "lecturer=" + strconv.FormatInt(s.LecturerOid, 10)
	}
	return ""
}

// SubjectFromQuery достаёт владельца расписания из параметров запроса.
func SubjectFromQuery(values url.Values) (Subject, error) {
	if g := strings.TrimSpace(values.Get("group")); g != "" {
		return Subject{Kind: SubjectGroup, Group: g, Label: g}, nil
	}
	if l := strings.TrimSpace(values.Get("lecturer")); l != "" {
		oid, err := strconv.ParseInt(l, 10, 64)
		if err != nil || oid <= 0 {
			return Subject{}, fmt.Errorf("идентификатор преподавателя должен быть положительным числом")
		}
		return Subject{Kind: SubjectLecturer, LecturerOid: oid}, nil
	}
	return Subject{}, nil
}

// subjectCookie хранит закреплённое расписание.
//
// Cookie, а не localStorage: серверная версия работает без JavaScript, и
// закрепление должно переживать обычный переход по ссылке. Ничего личного
// в ней нет — только имя группы или номер преподавателя, то же самое, что
// видно в адресной строке.
const subjectCookie = "schedulefu_subject"

// SetSubjectCookie закрепляет расписание за посетителем.
//
// Значение кодируется процентами: в cookie допустим только ASCII, а имена
// групп кириллические. Без кодирования Go молча выбрасывает недопустимые
// байты, и «Ю24-5» превращается в «24-5» — закрепление указывало бы на
// несуществующую группу.
func SetSubjectCookie(w http.ResponseWriter, s Subject, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:  subjectCookie,
		Value: url.QueryEscape(s.Key()),
		Path:  "/",
		// Год: закрепление должно пережить семестр, перевыбирать группу
		// каждую неделю никто не станет.
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSubjectCookie снимает закрепление.
func ClearSubjectCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: subjectCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

// SubjectFromCookie читает закреплённое расписание.
//
// Значение приходит от браузера и может быть подменено вручную, поэтому
// разбирается теми же строгими правилами, что и параметр ссылки: непригодный
// ключ просто считается отсутствующим.
func SubjectFromCookie(r *http.Request) Subject {
	c, err := r.Cookie(subjectCookie)
	if err != nil || c.Value == "" {
		return Subject{}
	}
	value, err := url.QueryUnescape(c.Value)
	if err != nil {
		return Subject{}
	}
	s, err := ParseSubjectKey(value)
	if err != nil {
		return Subject{}
	}
	return s
}
