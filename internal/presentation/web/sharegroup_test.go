package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKorotkayaSsylkaZakreplyaetGruppu(t *testing.T) {
	s := windowServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/g/%D0%9F%D0%9824-1", nil))
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/schedule?group=%D0%9F%D0%9824-1&pin=1" {
		t.Errorf("код %d, адрес %q", rec.Code, rec.Header().Get("Location"))
	}
	// В адресе может оказаться что угодно: не имя группы — не открываем.
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/g/%3Cscript%3E", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("мусор в адресе: код %d", rec.Code)
	}
}

func TestEkranPodelitsyaSQR(t *testing.T) {
	s := windowServer(t)
	req := httptest.NewRequest(http.MethodGet, "/share?group=%D0%9F%D0%9824-1", nil)
	req.Host = "fa.planovo.pro"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{`<svg class="qr-svg"`, `<path fill="#000" d="M`, "https://fa.planovo.pro/g/ПИ24-1", `data-share="https://fa.planovo.pro/g/`} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q", want)
		}
	}
	rec = httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/share/qr.png?group=%D0%9F%D0%9824-1", nil))
	if rec.Header().Get("Content-Type") != "image/png" || !bytes.HasPrefix(rec.Body.Bytes(), []byte("\x89PNG")) {
		t.Errorf("картинка: %q, %d байт", rec.Header().Get("Content-Type"), rec.Body.Len())
	}
}

// QR и ссылка ведут на адрес сайта из настройки, а не на тот, на который
// пришёл запрос: за прокси или в предпросмотре это 127.0.0.1, и QR с таким
// адресом у студента не откроется.
func TestSsylkaVedyotNaAdresSaytaANeNaLokalnyy(t *testing.T) {
	s := windowServer(t)
	s.d.Origin = "https://fa.planovo.pro"
	req := httptest.NewRequest(http.MethodGet, "/share?group=%D0%9F%D0%9824-1", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, "127.0.0.1") || !strings.Contains(body, "https://fa.planovo.pro/g/ПИ24-1") {
		t.Error("ссылка ведёт не на адрес сайта")
	}
}
