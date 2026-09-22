package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// Cookie, а не localStorage: страницы работают без JavaScript. Ничего
// личного в ней нет — то же, что в адресной строке.
const subjectCookie = "schedulefu_subject"

// SetSubjectCookie закрепляет расписание. Значение кодируется процентами:
// в cookie допустим только ASCII, и без кодирования Go молча выбрасывал
// буквы — «Ю24-5» становилось «24-5», несуществующей группой.
func SetSubjectCookie(w http.ResponseWriter, s sched.Subject, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: subjectCookie, Value: url.QueryEscape(s.Key()), Path: "/",
		MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// ClearSubjectCookie снимает закрепление.
func ClearSubjectCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: subjectCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// SubjectFromCookie читает закреплённое. Значение приходит от браузера и
// может быть подменено — разбирается теми же строгими правилами, что и
// параметр ссылки; непригодное считается отсутствующим.
func SubjectFromCookie(r *http.Request) sched.Subject {
	c, err := r.Cookie(subjectCookie)
	if err != nil || c.Value == "" {
		return sched.Subject{}
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return sched.Subject{}
	}
	s, err := sched.ParseSubjectKey(healPercent(v))
	if err != nil {
		return sched.Subject{}
	}
	return s
}

// recentCookie — последние открытые группы: студент смотрит не только
// свою, но и группу друга или потока; пять имён через «|», процентами.
const recentCookie = "schedulefu_recent"

// healPercent чинит значение, закодированное дважды: «%D0%9C%D0%95%D0%9D22-1»
// вместо «МЕН22-1в». Такие cookie успели разойтись по браузерам, пока
// html/template кодировал наши ссылки повторно; выбрасывать закрепление
// человека из-за нашей ошибки нельзя.
func healPercent(v string) string {
	if !strings.Contains(v, "%D") && !strings.Contains(v, "%d") {
		return v
	}
	decoded, err := url.QueryUnescape(v)
	if err != nil {
		return v
	}
	return decoded
}

// RecentFromCookie — недавние группы, свежие первыми.
func RecentFromCookie(r *http.Request) []string {
	c, err := r.Cookie(recentCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return nil
	}
	var out []string
	for _, name := range strings.Split(v, "|") {
		if name = strings.TrimSpace(healPercent(name)); name != "" && len(out) < 5 {
			out = append(out, name)
		}
	}
	return out
}

// RememberRecent ставит группу первой в списке недавних.
func RememberRecent(w http.ResponseWriter, r *http.Request, group string, secure bool) {
	names := []string{group}
	for _, n := range RecentFromCookie(r) {
		if n != group && len(names) < 5 {
			names = append(names, n)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: recentCookie, Value: url.QueryEscape(strings.Join(names, "|")), Path: "/",
		MaxAge: 180 * 24 * 60 * 60, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

// recentLecturersCookie — последние открытые преподаватели: «oid:имя», имя
// хранится рядом, чтобы показать список без похода в базу.
const recentLecturersCookie = "schedulefu_recent_lecturers"

// RecentLecturer — запись из cookie недавних преподавателей.
type RecentLecturer struct {
	Oid  int64
	Name string
}

// RecentLecturersFromCookie — недавние преподаватели, свежие первыми.
// Значение приходит от браузера: oid проверяется как число, имя — только
// для показа и экранируется шаблоном.
func RecentLecturersFromCookie(r *http.Request) []RecentLecturer {
	c, err := r.Cookie(recentLecturersCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	v, err := url.QueryUnescape(c.Value)
	if err != nil {
		return nil
	}
	var out []RecentLecturer
	for _, item := range strings.Split(v, "|") {
		oidText, name, ok := strings.Cut(item, ":")
		if !ok {
			continue
		}
		oid, err := strconv.ParseInt(oidText, 10, 64)
		if err != nil || oid <= 0 || strings.TrimSpace(name) == "" || len(out) >= 5 {
			continue
		}
		out = append(out, RecentLecturer{Oid: oid, Name: strings.TrimSpace(name)})
	}
	return out
}

// RememberRecentLecturer ставит преподавателя первым в списке недавних.
func RememberRecentLecturer(w http.ResponseWriter, r *http.Request, oid int64, name string, secure bool) {
	if strings.TrimSpace(name) == "" {
		return
	}
	items := []string{strconv.FormatInt(oid, 10) + ":" + strings.ReplaceAll(name, "|", " ")}
	for _, rl := range RecentLecturersFromCookie(r) {
		if rl.Oid != oid && len(items) < 5 {
			items = append(items, strconv.FormatInt(rl.Oid, 10)+":"+rl.Name)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: recentLecturersCookie, Value: url.QueryEscape(strings.Join(items, "|")), Path: "/",
		MaxAge: 180 * 24 * 60 * 60, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}
