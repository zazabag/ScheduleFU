package ops

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/ops/domain"
)

type sent struct {
	chat int64
	text string
}

type fakeChat struct {
	mu   sync.Mutex
	sent []sent
}

func (f *fakeChat) Send(_ context.Context, chat int64, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sent{chat, text})
	return nil
}
func (f *fakeChat) Updates(context.Context) <-chan Command { return nil }

func (f *fakeChat) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.sent
	f.sent = nil
	return out
}

type fakeHost struct{ siteErr error }

func (h *fakeHost) Stats(context.Context) (domain.Host, error) {
	return domain.Host{Uptime: time.Hour, MemTotalMB: 8000, MemAvailMB: 7000, DiskTotalGB: 79, DiskFreeGB: 70}, nil
}
func (h *fakeHost) Services(_ context.Context, names []string) []domain.Service {
	var out []domain.Service
	for _, n := range names {
		out = append(out, domain.Service{Name: n, Active: true, State: "active"})
	}
	return out
}
func (h *fakeHost) Site(context.Context) error { return h.siteErr }

type fakeStore struct{ now time.Time }

func (s *fakeStore) Collector(context.Context) (domain.Collector, error) {
	return domain.Collector{Found: true, LastOK: s.now.Add(-time.Minute), Last: s.now.Add(-time.Minute)}, nil
}
func (s *fakeStore) Notes(context.Context, time.Time) (domain.Notes, error) {
	return domain.Notes{}, nil
}
func (s *fakeStore) LLM(context.Context, time.Time) (domain.LLM, error) { return domain.LLM{}, nil }

type fakeProbe struct{ err error }

func (p *fakeProbe) Ping(context.Context) (time.Duration, error) { return time.Second, p.err }

func newOps(t *testing.T, chatID int64) (*Service, *fakeChat, *fakeHost, *fakeProbe) {
	t.Helper()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	chat, host, probe := &fakeChat{}, &fakeHost{}, &fakeProbe{}
	s := New(chat, host, &fakeStore{now: now}, probe, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		ChatID: chatID, Services: []string{"serve", "collect"}, Model: "glm-4.5-flash",
		Now: func() time.Time { return now }})
	return s, chat, host, probe
}

// Поломка — одно сообщение при появлении и одно при починке, а не
// напоминание на каждой проверке.
func TestTrevogaOdinRazIPochinka(t *testing.T) {
	s, chat, host, _ := newOps(t, -100)
	ctx := context.Background()
	s.Check(ctx)
	if got := chat.take(); len(got) != 0 {
		t.Fatalf("на здоровом сервере сообщения: %+v", got)
	}
	host.siteErr = errors.New("connection refused")
	s.Check(ctx)
	s.Check(ctx)
	got := chat.take()
	if len(got) != 1 || got[0].chat != -100 || !strings.Contains(got[0].text, "сайт не отвечает") {
		t.Fatalf("тревога: %+v", got)
	}
	host.siteErr = nil
	s.Check(ctx)
	if got := chat.take(); len(got) != 1 || !strings.Contains(got[0].text, "починилось") {
		t.Fatalf("починка: %+v", got)
	}
}

// Проба нейросети упала — это тревога, как и любая поломка.
func TestProbaNeyrosetiPodnimaetTrevogu(t *testing.T) {
	s, chat, _, probe := newOps(t, -100)
	ctx := context.Background()
	probe.err = errors.New("llm: исчерпан дневной лимит вызовов (код 1304)")
	s.Probe(ctx)
	s.Check(ctx)
	if got := chat.take(); len(got) != 1 || !strings.Contains(got[0].text, "дневной лимит") {
		t.Fatalf("тревога пробы: %+v", got)
	}
}

// Бот отвечает только своему чату: состояние сервера — не для всех, кто
// добавит бота к себе. Пока чат не задан, на /start он называет id чата —
// и больше ничего.
func TestKomandyTolkoIzSvoegoChata(t *testing.T) {
	s, chat, _, _ := newOps(t, -100)
	ctx := context.Background()
	s.Handle(ctx, Command{ChatID: -100, Text: "/status@FAsupportingbot"})
	if got := chat.take(); len(got) != 1 || !strings.Contains(got[0].text, "Всё работает") {
		t.Fatalf("/status: %+v", got)
	}
	s.Handle(ctx, Command{ChatID: 42, Text: "/status"})
	if got := chat.take(); len(got) != 0 {
		t.Fatalf("чужому чату ответили: %+v", got)
	}

	fresh, chat2, _, _ := newOps(t, 0)
	fresh.Handle(ctx, Command{ChatID: -555, Text: "/start@FAsupportingbot"})
	got := chat2.take()
	if len(got) != 1 || got[0].chat != -555 || !strings.Contains(got[0].text, "-555") {
		t.Fatalf("знакомство: %+v", got)
	}
	fresh.Handle(ctx, Command{ChatID: -555, Text: "/status"})
	if got := chat2.take(); len(got) != 0 {
		t.Fatalf("без заданного чата выдано состояние: %+v", got)
	}
}

// Утренний отчёт — раз в день, в заданное время, даже если всё хорошо:
// тишина иначе неотличима от умершего бота.
func TestUtrenniyOtchyotRazVDen(t *testing.T) {
	s, chat, _, _ := newOps(t, -100)
	loc := time.UTC
	s.opts.DailyAt = "09:00"
	morning := time.Date(2026, 9, 24, 9, 1, 0, 0, loc)
	if s.dailyDue(time.Date(2026, 9, 24, 8, 59, 0, 0, loc)) {
		t.Fatal("отчёт раньше времени")
	}
	if !s.dailyDue(morning) {
		t.Fatal("отчёт не пришёл в срок")
	}
	s.Daily(context.Background(), morning)
	if s.dailyDue(morning.Add(3 * time.Hour)) {
		t.Error("второй отчёт в тот же день")
	}
	if !s.dailyDue(morning.Add(24 * time.Hour)) {
		t.Error("на следующий день отчёта нет")
	}
	if got := chat.take(); len(got) != 1 || !strings.Contains(got[0].text, "Утренний отчёт") {
		t.Errorf("отчёт: %+v", got)
	}
}
