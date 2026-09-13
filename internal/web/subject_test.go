package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSubjectKeyTudaIObratno(t *testing.T) {
	cases := []Subject{
		{Kind: SubjectGroup, Group: "ПИ24-1", Label: "ПИ24-1"},
		{Kind: SubjectLecturer, LecturerOid: 46674},
	}
	for _, want := range cases {
		got, err := ParseSubjectKey(want.Key())
		if err != nil {
			t.Fatalf("ключ %q не разобран: %v", want.Key(), err)
		}
		if got.Kind != want.Kind || got.Group != want.Group || got.LecturerOid != want.LecturerOid {
			t.Errorf("ключ %q вернул %+v, ожидалось %+v", want.Key(), got, want)
		}
	}
}

// TestParseSubjectKeyOtvergaetGUID: ключом преподавателя может быть только
// число. GUID источник не отклоняет, а отдаёт по нему чужие пары — такой
// ключ не должен попасть в подписку даже случайно.
func TestParseSubjectKeyOtvergaetGUID(t *testing.T) {
	bad := []string{
		"lecturer:d6607672-25a6-4ef6-8d02-69106f92cf1e",
		"lecturer:0",
		"lecturer:-5",
		"lecturer:",
		"person:46674",
		"ПИ24-1",
		"",
	}
	for _, key := range bad {
		if _, err := ParseSubjectKey(key); err == nil {
			t.Errorf("ключ %q принят, а должен быть отвергнут", key)
		}
	}
}

func TestSubjectFromQuery(t *testing.T) {
	g, err := SubjectFromQuery(url.Values{"group": {"Ю24-5"}})
	if err != nil || g.Kind != SubjectGroup || g.Group != "Ю24-5" {
		t.Fatalf("группа разобрана как %+v (%v)", g, err)
	}

	l, err := SubjectFromQuery(url.Values{"lecturer": {"46674"}})
	if err != nil || l.Kind != SubjectLecturer || l.LecturerOid != 46674 {
		t.Fatalf("преподаватель разобран как %+v (%v)", l, err)
	}

	empty, err := SubjectFromQuery(url.Values{})
	if err != nil || !empty.IsZero() {
		t.Fatalf("пустой запрос дал %+v (%v)", empty, err)
	}

	if _, err := SubjectFromQuery(url.Values{"lecturer": {"guid-here"}}); err == nil {
		t.Error("нечисловой идентификатор преподавателя принят")
	}
}

func TestSubjectQuery(t *testing.T) {
	g := Subject{Kind: SubjectGroup, Group: "ПИ24-1"}
	if got := g.Query(); got != "group=%D0%9F%D0%9824-1" {
		t.Errorf("Query группы = %q", got)
	}
	l := Subject{Kind: SubjectLecturer, LecturerOid: 46674}
	if got := l.Query(); got != "lecturer=46674" {
		t.Errorf("Query преподавателя = %q", got)
	}
}

// TestCookiePerezhivaetKirillicu закрепляет найденную ошибку: в cookie
// допустим только ASCII, и без кодирования Go молча выбрасывал буквы —
// «Ю24-5» сохранялось как «24-5», то есть как несуществующая группа.
func TestCookiePerezhivaetKirillicu(t *testing.T) {
	want := Subject{Kind: SubjectGroup, Group: "Ю24-5", Label: "Ю24-5"}

	rec := httptest.NewRecorder()
	SetSubjectCookie(rec, want, false)

	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	got := SubjectFromCookie(r)
	if got.Group != want.Group {
		t.Fatalf("из cookie вернулось %q, ожидалось %q", got.Group, want.Group)
	}
}

func TestCookieIgnoriruetPoddelku(t *testing.T) {
	// Значение приходит от браузера и правится вручную: непригодный ключ
	// должен считаться отсутствующим, а не приводить к запросу по GUID.
	for _, bad := range []string{"lecturer:d6607672-25a6", "person:1", "%%%", ""} {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: subjectCookie, Value: bad})
		if got := SubjectFromCookie(r); !got.IsZero() {
			t.Errorf("подделка %q принята как %+v", bad, got)
		}
	}
}
