// Package host — состояние машины для присмотра: память, нагрузка, диск,
// службы systemd, отвечает ли сайт.
//
// Читает /proc и зовёт systemctl — рассчитано на боевой Ubuntu. На машине
// разработчика (macOS, Windows) Stats честно отвечает ошибкой, а не нулями,
// похожими на правду.
package host

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/ops"
	"github.com/zazabag/schedulefu/internal/modules/ops/domain"
)

// Machine — эта машина.
type Machine struct {
	// SiteURL — адрес проверки здоровья сайта, изнутри машины.
	SiteURL string
	// Unit — шаблон имени службы: «schedulefu-%s».
	Unit string
	http *http.Client
}

// New собирает адаптер.
func New(siteURL string) *Machine {
	return &Machine{SiteURL: siteURL, Unit: "schedulefu-%s", http: &http.Client{Timeout: 10 * time.Second}}
}

// Stats — память, нагрузка, аптайм и диск.
func (m *Machine) Stats(context.Context) (domain.Host, error) {
	var h domain.Host
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return h, fmt.Errorf("память: %w", err)
	}
	h.MemTotalMB, h.MemAvailMB = parseMeminfo(string(raw))
	if raw, err = os.ReadFile("/proc/loadavg"); err == nil {
		h.Load1 = firstFloat(string(raw))
	}
	if raw, err = os.ReadFile("/proc/uptime"); err == nil {
		h.Uptime = time.Duration(firstFloat(string(raw)) * float64(time.Second))
	}
	h.DiskTotalGB, h.DiskFreeGB, err = disk("/")
	if err != nil {
		return h, fmt.Errorf("диск: %w", err)
	}
	return h, nil
}

// Services спрашивает systemd о каждой службе. Ошибка запуска systemctl —
// не «служба работает»: такая служба показывается как неизвестная.
func (m *Machine) Services(ctx context.Context, names []string) []domain.Service {
	out := make([]domain.Service, 0, len(names))
	for _, n := range names {
		// is-active отвечает ненулевым кодом на всё, кроме active, — сам код
		// не ошибка, ошибка — пустой вывод.
		raw, _ := exec.CommandContext(ctx, "systemctl", "is-active", fmt.Sprintf(m.Unit, n)).Output()
		state := strings.TrimSpace(string(raw))
		if state == "" {
			state = "неизвестно"
		}
		out = append(out, domain.Service{Name: n, State: state, Active: state == "active"})
	}
	return out
}

// Site проверяет, что сайт отвечает изнутри машины.
func (m *Machine) Site(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.SiteURL, nil)
	if err != nil {
		return err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ответ %d", resp.StatusCode)
	}
	return nil
}

// parseMeminfo — MemTotal и MemAvailable в мегабайтах.
func parseMeminfo(s string) (total, avail int) {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.Atoi(f[1])
		switch f[0] {
		case "MemTotal:":
			total = kb / 1024
		case "MemAvailable:":
			avail = kb / 1024
		}
	}
	return total, avail
}

func firstFloat(s string) float64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

var _ ops.Host = (*Machine)(nil)
