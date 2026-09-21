package web

import (
	"net/http"
	"net/url"
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
	s, err := sched.ParseSubjectKey(v)
	if err != nil {
		return sched.Subject{}
	}
	return s
}

// recentCookie — последние открытые группы: студент смотрит не только
// свою, но и группу друга или потока; пять имён через «|», процентами.
const recentCookie = "schedulefu_recent"

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
		if name = strings.TrimSpace(name); name != "" && len(out) < 5 {
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
