package httpx

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func handlerOf(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, body)
	})
}

func TestCompressSzhimaetStranicu(t *testing.T) {
	// Похоже на настоящую страницу: сотня одинаковых карточек.
	body := strings.Repeat(`<div class="room"><span class="room-num">313</span></div>`, 500)

	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(handlerOf(body)).ServeHTTP(rec, r)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Accept-Encoding") {
		t.Error("не выставлен Vary: кэш отдаст сжатое тем, кто не просил")
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Error("Content-Length остался от несжатого тела")
	}

	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("ответ не разжимается: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != body {
		t.Error("после разжатия тело не совпадает с исходным")
	}
	if rec.Body.Len() >= len(body)/5 {
		t.Errorf("сжатие почти не сработало: %d из %d байт", rec.Body.Len(), len(body))
	}
}

func TestCompressNeTrogaetTehKtoNeProsil(t *testing.T) {
	body := "обычный ответ"
	r := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	Compress(handlerOf(body)).ServeHTTP(rec, r)

	if rec.Header().Get("Content-Encoding") != "" {
		t.Error("ответ сжат клиенту, который об этом не просил")
	}
	if rec.Body.String() != body {
		t.Error("тело изменилось")
	}
}

// TestCompressPropuskaetKartinki: png и шрифты уже сжаты, повторное
// сжатие только жжёт процессор.
func TestCompressPropuskaetKartinki(t *testing.T) {
	r := httptest.NewRequest("GET", "/static/icon-192.png", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(handlerOf("данные картинки")).ServeHTTP(rec, r)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Error("картинка сжата повторно")
	}
}

func TestCompressSohranyaetKodOtveta(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "не найдено")
	})
	r := httptest.NewRequest("GET", "/nope", nil)
	r.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Compress(h).ServeHTTP(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Errorf("код ответа = %d, ожидался 404", rec.Code)
	}
}
