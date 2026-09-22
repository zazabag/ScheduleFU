package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// В cookie допустим только ASCII; без кодирования «Ю24-5» превращалось в «24-5».
func TestCookiePerezhivaetKirillicu(t *testing.T) {
	rec := httptest.NewRecorder()
	SetSubjectCookie(rec, sched.GroupSubject("Ю24-5"), false)
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	if got := SubjectFromCookie(r); got.Group != "Ю24-5" {
		t.Fatalf("из cookie вернулось %q", got.Group)
	}
}

func TestCookieIgnoriruetPoddelku(t *testing.T) {
	for _, bad := range []string{"lecturer:d6607672-25a6", "person:1", "%%%", ""} {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: subjectCookie, Value: bad})
		if !SubjectFromCookie(r).IsZero() {
			t.Errorf("подделка %q принята", bad)
		}
	}
}

func TestCookieVylechivaetDvoynoeKodirovanie(t *testing.T) {
	// Такие значения успели разойтись по браузерам, пока шаблон кодировал
	// наши ссылки повторно: закрепление человека из-за этого терять нельзя.
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: subjectCookie, Value: url.QueryEscape("group:%D0%9C%D0%95%D0%9D22-1%D0%B2")})
	if got := SubjectFromCookie(r).Group; got != "МЕН22-1в" {
		t.Errorf("получили %q, ожидали МЕН22-1в", got)
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&http.Cookie{Name: subjectCookie, Value: url.QueryEscape("group:ПИ24-1")})
	if got := SubjectFromCookie(r2).Group; got != "ПИ24-1" {
		t.Errorf("здоровое значение испорчено: %q", got)
	}
}
