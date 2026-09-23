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

// Отчёт — утром и вечером, в заданное время, и каждый только раз: чаще
// автор просил не писать (23.09.2026) — сообщения тонули бы в шуме.
func TestOtchyotUtromIVecherom(t *testing.T) {
	s, chat, _, _ := newOps(t, -100)
	s.opts.ReportAt = "09:00,21:00"
	day := func(h, m int) time.Time { return time.Date(2026, 9, 24, h, m, 0, 0, time.UTC) }
	if _, due := s.reportDue(day(8, 59)); due {
		t.Fatal("отчёт раньше утра")
	}
	slot, due := s.reportDue(day(9, 1))
	if !due {
		t.Fatal("утреннего отчёта нет")
	}
	s.Report(context.Background(), day(9, 1), slot)
	if _, due := s.reportDue(day(14, 0)); due {
		t.Error("второй утренний отчёт")
	}
	slot, due = s.reportDue(day(21, 5))
	if !due {
		t.Fatal("вечернего отчёта нет")
	}
	s.Report(context.Background(), day(21, 5), slot)
	if _, due := s.reportDue(day(23, 30)); due {
		t.Error("второй вечерний отчёт")
	}
	if _, due := s.reportDue(day(9, 1).Add(24 * time.Hour)); !due {
		t.Error("на следующее утро отчёта нет")
	}
	got := chat.take()
	if len(got) != 2 || !strings.Contains(got[0].text, "Утренний отчёт") || !strings.Contains(got[1].text, "Вечерний отчёт") {
		t.Errorf("отчёты: %+v", got)
	}
}

// Бот перезапускается при каждой выкатке, а выкатки идут пачками — отчёт
// при каждом запуске превращал чат в ленту (23.09.2026: шесть за час).
// Запуск молчит, если всё в порядке, и называет только то, что уже сломано.
func TestZapuskMolchitKogdaVsyoVPoryadke(t *testing.T) {
	s, chat, _, _ := newOps(t, -100)
	s.Start(context.Background())
	if got := chat.take(); len(got) != 0 {
		t.Fatalf("при здоровом запуске сообщения: %+v", got)
	}

	s2, chat2, host2, _ := newOps(t, -100)
	host2.siteErr = errors.New("connection refused")
	s2.Start(context.Background())
	got := chat2.take()
	if len(got) != 1 || !strings.Contains(got[0].text, "сайт не отвечает") {
		t.Fatalf("запуск с поломкой: %+v", got)
	}
	// Уже названная при запуске поломка — не новость для первой проверки.
	s2.Check(context.Background())
	if got := chat2.take(); len(got) != 0 {
		t.Errorf("поломка повторена после запуска: %+v", got)
	}
}
