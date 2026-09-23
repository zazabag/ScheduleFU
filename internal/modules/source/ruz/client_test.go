package ruz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/source"
)

func throttlingServer(t *testing.T, status int, retryAfter string) (*Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return New(Options{BaseURL: srv.URL, RPS: 1000}), &hits
}

// Вуз, ответивший 429, просит сбавить темп. Повторять запрос через
// секунду — ровно то, за что банят по адресу: клиент обязан отступить
// сразу и передать наверх, сколько ждать.
func TestNaOtvet429KlientNePovtoryaetIPeredayotPauzu(t *testing.T) {
	c, hits := throttlingServer(t, http.StatusTooManyRequests, "120")
	_, err := c.Schedule(context.Background(), KindAuditorium, 1, time.Now(), time.Now())
	var th *source.ThrottledError
	if !errors.As(err, &th) {
		t.Fatalf("ожидался ThrottledError, получено: %v", err)
	}
	if th.RetryAfter != 2*time.Minute {
		t.Errorf("Retry-After: %s", th.RetryAfter)
	}
	if hits.Load() != 1 {
		t.Errorf("запросов к источнику: %d, ожидался один", hits.Load())
	}
}

// 403 от вуза, который раньше отвечал, — почти всегда блокировка адреса
// на стороне защиты сервера. Долбиться дальше нельзя так же, как при 429.
func TestNaOtvet403KlientOtstupaet(t *testing.T) {
	c, hits := throttlingServer(t, http.StatusForbidden, "")
	_, err := c.Search(context.Background(), SearchGroup, "УПП26-2")
	var th *source.ThrottledError
	if !errors.As(err, &th) || th.RetryAfter != 0 || hits.Load() != 1 {
		t.Fatalf("ошибка %v, запросов %d", err, hits.Load())
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"30":                            30 * time.Second,
		"мусор":                         0,
		"Wed, 23 Sep 2026 12:10:00 GMT": 10 * time.Minute,
		"Wed, 23 Sep 2026 11:00:00 GMT": 0, // уже прошло
	}
	for in, want := range cases {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("%q: %s, ожидалось %s", in, got, want)
		}
	}
}
