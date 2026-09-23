package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Ключ владельца попадает в запрос к базе как есть, поэтому его форма
// проверяется строго: cookie подделывается тривиально.
func TestKlyuchVladeltsaStrogoyFormy(t *testing.T) {
	bad := []string{"", "короткий", "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ",
		"' OR 1=1 --                     ", "0123456789abcdef0123456789abcde"}
	for _, v := range bad {
		if validOwner(v) {
			t.Errorf("принят негодный ключ %q", v)
		}
	}
	if !validOwner("0123456789abcdef0123456789abcdef") {
		t.Error("нормальный ключ отвергнут")
	}
}

func TestKlyuchVydayotsyaOdinRazINeMenyaetsya(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/lessons", nil)
	key := OwnerKey(w, r, false)
	if !validOwner(key) {
		t.Fatalf("выдан негодный ключ %q", key)
	}
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie ключа должна быть HttpOnly и Lax: %+v", c)
	}

	// Тот же человек заходит снова — ключ обязан остаться прежним, иначе
	// конспекты пропадают при каждом заходе.
	r2 := httptest.NewRequest(http.MethodGet, "/lessons", nil)
	r2.AddCookie(c)
	if again := OwnerKey(httptest.NewRecorder(), r2, false); again != key {
		t.Errorf("ключ сменился: было %q, стало %q", key, again)
	}
}

func TestPoddelannyyKlyuchNeChitaetsya(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/lessons", nil)
	r.AddCookie(&http.Cookie{Name: ownerCookie, Value: "чужое"})
	if got := ExistingOwnerKey(r); got != "" {
		t.Errorf("подделка прошла: %q", got)
	}
}
