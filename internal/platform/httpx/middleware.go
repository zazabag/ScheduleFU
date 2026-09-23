// Package httpx — обвязка HTTP: заголовки безопасности и ограничение частоты.
package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders проставляет заголовки, которые браузер применяет к странице.
//
// Политика строгая осознанно: своего скрипта у серверной версии нет вовсе,
// а данные приходят из чужого источника (ruz.fa.ru). Если в расписании
// однажды окажется разметка и где-то просочится мимо экранирования,
// политика не даст ей выполниться.
func SecurityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		// Шрифты подключаются с Google Fonts — единственный внешний источник.
		"style-src 'self' https://fonts.googleapis.com; " +
		"font-src https://fonts.gstatic.com; " +
		"img-src 'self' data:; " +
		"connect-src 'self'; " +
		"form-action 'self'; " +
		"base-uri 'none'; " +
		// Встраивание в чужой фрейм запрещено: сервис показывает
		// местонахождение людей, и кликджекинг здесь ни к чему.
		"frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		// Микрофон открыт своим страницам: раздел «Пары» записывает занятие.
		// Разрешение всё равно спрашивает браузер, но без этой строки он не
		// спросит вовсе. Остальные устройства сервису не нужны.
		h.Set("Permissions-Policy", "geolocation=(), microphone=(self), camera=()")
		next.ServeHTTP(w, r)
	})
}

// RateLimiter ограничивает частоту запросов с одного адреса.
//
// Нужен не против злого умысла, а против обычной беды маленького сервиса:
// один скрипт или зацикленная страница способны занять базу целиком, и
// тогда сервис перестаёт работать для всех остальных.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // запросов в секунду
	burst   float64 // разрешённый всплеск
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter создаёт ограничитель: rate запросов в секунду с
// возможностью всплеска burst.
func NewRateLimiter(rate, burst float64) *RateLimiter {
	return &RateLimiter{
		buckets: make(map[string]*bucket),
		rate:    rate,
		burst:   burst,
		now:     time.Now,
	}
}

// Allow сообщает, можно ли обслужить очередной запрос с адреса.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		// Новому адресу выдаётся полный запас, иначе первый же запрос
		// после паузы выглядел бы превышением.
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}

	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Cleanup убирает адреса, которые давно молчат: без этого карта росла бы
// бесконечно и превратилась бы в утечку памяти.
func (l *RateLimiter) Cleanup(olderThan time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := l.now().Add(-olderThan)
	for key, b := range l.buckets {
		if b.last.Before(cutoff) {
			delete(l.buckets, key)
		}
	}
}

// Middleware отклоняет запросы сверх лимита с кодом 429.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(ClientIP(r)) {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "слишком много запросов, подождите немного", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP определяет адрес клиента.
//
// X-Forwarded-For учитывается только когда сервис стоит за доверенным
// обратным прокси: заголовок подделывается тривиально, и слепое доверие к
// нему позволило бы обойти ограничение, подставляя случайный адрес.
func ClientIP(r *http.Request) string {
	if TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// TrustProxy включается только при запуске за обратным прокси.
var TrustProxy = false

// StartCleanup запускает периодическую уборку молчащих адресов.
func (l *RateLimiter) StartCleanup(every, olderThan time.Duration, stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				l.Cleanup(olderThan)
			}
		}
	}()
}
