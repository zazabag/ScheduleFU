package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterPropuskaetVsplesk(t *testing.T) {
	l := NewRateLimiter(1, 5)
	for i := 0; i < 5; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("запрос %d отклонён внутри разрешённого всплеска", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Fatal("шестой запрос подряд должен быть отклонён")
	}
}

func TestRateLimiterVosstanavlivaetsyaSoVremenem(t *testing.T) {
	l := NewRateLimiter(2, 2)
	now := time.Now()
	l.now = func() time.Time { return now }

	l.Allow("ip")
	l.Allow("ip")
	if l.Allow("ip") {
		t.Fatal("запас должен был кончиться")
	}
	// Прошла секунда — при двух запросах в секунду вернулись два.
	now = now.Add(time.Second)
	if !l.Allow("ip") {
		t.Fatal("через секунду запрос должен пройти")
	}
}

func TestRateLimiterSchitaetAdresaOtdelno(t *testing.T) {
	l := NewRateLimiter(1, 1)
	if !l.Allow("first") || !l.Allow("second") {
		t.Fatal("разные адреса не должны мешать друг другу")
	}
	if l.Allow("first") {
		t.Fatal("для первого адреса запас исчерпан")
	}
}

func TestCleanupUbiraetMolchashchie(t *testing.T) {
	l := NewRateLimiter(1, 1)
	now := time.Now()
	l.now = func() time.Time { return now }
	l.Allow("ip")

	now = now.Add(time.Hour)
	l.Cleanup(30 * time.Minute)
	if len(l.buckets) != 0 {
		t.Fatalf("молчащий адрес остался в карте: %d", len(l.buckets))
	}
}

// TestClientIPNeDoveryaetZagolovku: без обратного прокси X-Forwarded-For
// игнорируется, иначе ограничение обходится подстановкой чужого адреса.
func TestClientIPNeDoveryaetZagolovku(t *testing.T) {
	TrustProxy = false
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	if got := ClientIP(r); got != "10.0.0.1" {
		t.Errorf("ClientIP = %q, ожидался реальный адрес соединения", got)
	}

	TrustProxy = true
	defer func() { TrustProxy = false }()
	if got := ClientIP(r); got != "1.1.1.1" {
		t.Errorf("за прокси ClientIP = %q, ожидался адрес из заголовка", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	for _, key := range []string{
		"Content-Security-Policy", "X-Content-Type-Options",
		"Referrer-Policy", "X-Frame-Options", "Permissions-Policy",
	} {
		if rec.Header().Get(key) == "" {
			t.Errorf("заголовок %s не выставлен", key)
		}
	}
	if got := rec.Header().Get("Content-Security-Policy"); !contains(got, "frame-ancestors 'none'") {
		t.Errorf("политика не запрещает встраивание: %q", got)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func TestMiddlewareOtdayot429(t *testing.T) {
	l := NewRateLimiter(1, 1)
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.5.5.5:1000"

	first := httptest.NewRecorder()
	h.ServeHTTP(first, r)
	if first.Code != http.StatusOK {
		t.Fatalf("первый запрос: %d", first.Code)
	}
	second := httptest.NewRecorder()
	h.ServeHTTP(second, r)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("второй запрос: %d, ожидался 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("не сказано, когда повторить")
	}
}
