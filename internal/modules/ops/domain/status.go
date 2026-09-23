// Package domain — язык модуля ops: состояние сервера, тревоги и отчёт.
//
// Пакет ничего не знает ни о Telegram, ни о базе: состояние собирают
// адаптеры, а здесь решается, что из него считать поломкой и как об этом
// сказать человеку.
package domain

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Host — машина.
type Host struct {
	Uptime      time.Duration
	Load1       float64
	MemTotalMB  int
	MemAvailMB  int
	DiskTotalGB float64
	DiskFreeGB  float64
}

// Service — служба systemd.
type Service struct {
	Name   string
	Active bool
	State  string // active | failed | inactive | …
}

// Collector — последний обход вуза.
type Collector struct {
	Found       bool // проходы вообще были
	LastOK      time.Time
	Last        time.Time
	LastFailure string // ошибка последнего прохода, если он упал
	Lessons     int
	Requests    int
	Errors      int
}

// Notes — очередь записей пар.
type Notes struct {
	Queued    int // ждут обработки
	Working   int // обрабатываются
	Stuck     int // обрабатываются дольше часа — воркер завис или умер
	Ready24h  int
	Failed24h int
	// LastFailure — причина последней неудачи за сутки.
	LastFailure string
}

// LLM — модель конспекта: проба и расход.
type LLM struct {
	Model        string
	ProbeAt      time.Time
	ProbeOK      bool
	ProbeErr     string
	ProbeLatency time.Duration

	Calls24h  int
	Errors24h int
	Tokens24h int
	Tokens7d  int
	LastOKAt  time.Time // последний удачный вызов, проба в том числе
	LastErrAt time.Time
	LastErr   string
	// LastErrCode — код отказа провайдера, «1113».
	LastErrCode string
}

// Status — снимок сервера.
type Status struct {
	At        time.Time
	Host      Host
	HostErr   string
	Services  []Service
	SiteOK    bool
	SiteErr   string
	Collector Collector
	Notes     Notes
	LLM       LLM
	// StoreErr — база не ответила на отчётные запросы: это само по себе
	// поломка, и важнее всего остального в снимке.
	StoreErr string
}

// Problem — поломка. Key стабилен между проверками: по нему бот понимает,
// что это та же поломка, а не новая.
type Problem struct {
	Key  string
	Text string
}

// Пороги. Сбор ночью идёт раз в три часа, поэтому тишина в четыре часа —
// уже поломка, а не ночь.
const (
	CollectorStale = 4 * time.Hour
	DiskFreeMinPct = 10
	MemAvailMinPct = 5
)

// Problems — всё, что сейчас сломано.
func (s Status) Problems() []Problem {
	var out []Problem
	add := func(key, format string, args ...any) {
		out = append(out, Problem{Key: key, Text: fmt.Sprintf(format, args...)})
	}
	if s.StoreErr != "" {
		add("db", "база не отвечает: %s", s.StoreErr)
	}
	for _, sv := range s.Services {
		if !sv.Active {
			add("service:"+sv.Name, "служба schedulefu-%s не работает (%s)", sv.Name, sv.State)
		}
	}
	if !s.SiteOK {
		add("site", "сайт не отвечает: %s", s.SiteErr)
	}
	if s.StoreErr == "" {
		switch {
		case !s.Collector.Found:
			add("collector", "сбор расписания ни разу не прошёл")
		case s.At.Sub(s.Collector.LastOK) > CollectorStale:
			msg := "расписание не обновлялось " + ago(s.At, s.Collector.LastOK)
			if s.Collector.LastFailure != "" {
				msg += ": " + s.Collector.LastFailure
			}
			add("collector", "%s", msg)
		}
		if s.Notes.Stuck > 0 {
			add("notes:stuck", "записей зависло в обработке больше часа: %d", s.Notes.Stuck)
		}
	}
	if s.HostErr == "" {
		if s.Host.DiskTotalGB > 0 && s.Host.DiskFreeGB/s.Host.DiskTotalGB*100 < DiskFreeMinPct {
			add("disk", "на диске осталось %s ГБ из %s", gb(s.Host.DiskFreeGB), gb(s.Host.DiskTotalGB))
		}
		if s.Host.MemTotalMB > 0 && s.Host.MemAvailMB*100/s.Host.MemTotalMB < MemAvailMinPct {
			add("memory", "свободной памяти %d МБ из %d", s.Host.MemAvailMB, s.Host.MemTotalMB)
		}
	}
	if !s.LLM.ProbeAt.IsZero() && !s.LLM.ProbeOK {
		add("llm:probe", "нейросеть не отвечает на пробный запрос: %s", s.LLM.ProbeErr)
	}
	// Настоящий вызов упал после последнего удачного — нейросеть не работает
	// прямо сейчас, и студент с записью ждёт зря.
	if !s.LLM.LastErrAt.IsZero() && s.LLM.LastErrAt.After(s.LLM.LastOKAt) && s.At.Sub(s.LLM.LastErrAt) < 24*time.Hour {
		add("llm:calls", "конспект не пишется: %s", llmError(s.LLM.LastErrCode, s.LLM.LastErr))
	}
	return out
}

// Diff сравнивает прошлые тревоги с текущими: что появилось и что прошло.
// Бот пишет о смене состояния, а не о состоянии — одна и та же поломка
// каждые пять минут приучает глушить чат.
func Diff(active map[string]string, cur []Problem) (raised, cleared []Problem) {
	now := map[string]bool{}
	for _, p := range cur {
		now[p.Key] = true
		if _, ok := active[p.Key]; !ok {
			raised = append(raised, p)
		}
	}
	for key, text := range active {
		if !now[key] {
			cleared = append(cleared, Problem{Key: key, Text: text})
		}
	}
	sort.Slice(cleared, func(i, j int) bool { return cleared[i].Key < cleared[j].Key })
	return raised, cleared
}

// Format — отчёт для Telegram в разметке HTML.
func (s Status) Format() string {
	var b strings.Builder
	problems := s.Problems()
	if len(problems) == 0 {
		b.WriteString("✅ <b>Всё работает</b>\n")
	} else {
		b.WriteString("⚠️ <b>Есть поломки</b>\n")
		for _, p := range problems {
			b.WriteString("• " + esc(p.Text) + "\n")
		}
	}

	b.WriteString("\n<b>Сервер</b>\n")
	if s.HostErr != "" {
		b.WriteString("не прочитать: " + esc(s.HostErr) + "\n")
	} else {
		fmt.Fprintf(&b, "работает %s · нагрузка %.2f\n", uptime(s.Host.Uptime), s.Host.Load1)
		fmt.Fprintf(&b, "память: свободно %s из %s МБ\n", num(s.Host.MemAvailMB), num(s.Host.MemTotalMB))
		fmt.Fprintf(&b, "диск: свободно %s из %s ГБ\n", gb(s.Host.DiskFreeGB), gb(s.Host.DiskTotalGB))
	}
	var svc []string
	for _, sv := range s.Services {
		mark := "✅"
		if !sv.Active {
			mark = "❌"
		}
		svc = append(svc, mark+" "+esc(sv.Name))
	}
	if len(svc) > 0 {
		b.WriteString("службы: " + strings.Join(svc, " · ") + "\n")
	}
	if s.SiteOK {
		b.WriteString("сайт: ✅ отвечает\n")
	} else {
		b.WriteString("сайт: ❌ " + esc(s.SiteErr) + "\n")
	}

	b.WriteString("\n<b>Расписание</b>\n")
	switch {
	case s.StoreErr != "":
		b.WriteString("база не ответила\n")
	case !s.Collector.Found:
		b.WriteString("проходов ещё не было\n")
	default:
		fmt.Fprintf(&b, "обновлено %s · пар %s · запросов к вузу %s\n",
			ago(s.At, s.Collector.LastOK), num(s.Collector.Lessons), num(s.Collector.Requests))
		if s.Collector.LastFailure != "" {
			b.WriteString("последний проход упал: " + esc(s.Collector.LastFailure) + "\n")
		}
	}

	b.WriteString("\n<b>Записи пар</b>\n")
	fmt.Fprintf(&b, "за сутки: готово %d · с ошибкой %d\n", s.Notes.Ready24h, s.Notes.Failed24h)
	fmt.Fprintf(&b, "сейчас: в очереди %d · обрабатывается %d\n", s.Notes.Queued, s.Notes.Working)
	if s.Notes.LastFailure != "" {
		b.WriteString("последняя ошибка: " + esc(s.Notes.LastFailure) + "\n")
	}

	b.WriteString("\n" + s.LLM.Format())
	return b.String()
}

// Format — раздел про нейросеть; он же ответ на /llm.
func (l LLM) Format() string {
	var b strings.Builder
	b.WriteString("<b>Нейросеть</b> " + esc(l.Model) + "\n")
	switch {
	case l.ProbeAt.IsZero():
		b.WriteString("проба: ещё не делалась\n")
	case l.ProbeOK:
		fmt.Fprintf(&b, "проба: ✅ отвечает, %s назад, за %.1f с\n", uptime(time.Since(l.ProbeAt)), l.ProbeLatency.Seconds())
	default:
		b.WriteString("проба: ❌ " + esc(l.ProbeErr) + "\n")
	}
	fmt.Fprintf(&b, "за сутки: вызовов %d · ошибок %d · токенов %s\n", l.Calls24h, l.Errors24h, num(l.Tokens24h))
	fmt.Fprintf(&b, "за неделю: токенов %s\n", num(l.Tokens7d))
	if !l.LastErrAt.IsZero() {
		fmt.Fprintf(&b, "последний отказ: %s — %s\n", l.LastErrAt.Format("02.01 15:04"), esc(llmError(l.LastErrCode, l.LastErr)))
	}
	b.WriteString("<i>Остаток лимита bigmodel.cn не сообщает: здесь наш учёт и проба.</i>\n")
	return b.String()
}

func llmError(code, msg string) string {
	if code != "" {
		return msg + " (код " + code + ")"
	}
	return msg
}

func esc(s string) string { return html.EscapeString(s) }

// num — «11 000»: тысячи через пробел, как пишут по-русски.
func num(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, parts...), " ")
}

func gb(v float64) string {
	if v >= 10 {
		return strconv.Itoa(int(v + 0.5))
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// ago — «12 мин назад», «3 ч назад».
func ago(now, t time.Time) string {
	return uptime(now.Sub(t)) + " назад"
}

func uptime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "меньше минуты"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " мин"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " ч"
	}
	return strconv.Itoa(int(d.Hours()/24)) + " дн"
}
