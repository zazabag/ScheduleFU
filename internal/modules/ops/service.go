package ops

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/ops/domain"
)

// Options — настройки присмотра.
type Options struct {
	// ChatID — служебный чат. Ноль — чат ещё не задан: бот только называет
	// id чата в ответ на /start, чтобы его было откуда взять.
	ChatID int64
	// Services — службы schedulefu-*, за которыми следить.
	Services []string
	Model    string
	// Every — как часто проверять. ProbeEvery — как часто делать пробный
	// запрос к нейросети: он тратит токены, поэтому реже проверок.
	Every      time.Duration
	ProbeEvery time.Duration
	// ReportAt — когда присылать отчёт: «ЧЧ:ММ» через запятую, в поясе
	// Location. Автор просил утром и вечером — чаще отчёт тонет в шуме.
	ReportAt string
	Location *time.Location
	Now      func() time.Time
}

// Service — присмотр за сервером.
type Service struct {
	chat  Messenger
	host  Host
	store Store
	probe Prober // nil — нейросеть не настроена
	log   *slog.Logger
	opts  Options

	mu         sync.Mutex
	active     map[string]string // текущие тревоги: ключ → текст
	lastProbe  domain.LLM        // только поля пробы
	lastReport string            // последний отправленный отчёт: «дата слот»
}

// New собирает присмотр.
func New(chat Messenger, host Host, store Store, probe Prober, log *slog.Logger, opts Options) *Service {
	if opts.Every <= 0 {
		opts.Every = 5 * time.Minute
	}
	if opts.ProbeEvery <= 0 {
		opts.ProbeEvery = 6 * time.Hour
	}
	if opts.ReportAt == "" {
		opts.ReportAt = "09:00,21:00"
	}
	if opts.Location == nil {
		opts.Location = time.UTC
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{chat: chat, host: host, store: store, probe: probe, log: log, opts: opts,
		active: map[string]string{}}
}

func (s *Service) now() time.Time { return s.opts.Now().In(s.opts.Location) }

// Run крутит присмотр, пока жив контекст: проверки, пробы, отчёты утром и вечером
// и ответы на команды.
func (s *Service) Run(ctx context.Context) error {
	updates := s.chat.Updates(ctx)
	s.Start(ctx)

	check := time.NewTicker(s.opts.Every)
	defer check.Stop()
	probe := time.NewTicker(s.opts.ProbeEvery)
	defer probe.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case cmd, ok := <-updates:
			if !ok {
				updates = nil // адаптер закрылся — дальше только проверки
				continue
			}
			s.Handle(ctx, cmd)
		case <-probe.C:
			s.Probe(ctx)
		case <-check.C:
			s.Check(ctx)
			now := s.now()
			if slot, due := s.reportDue(now); due {
				s.Report(ctx, now, slot)
			}
		}
	}
}

// Snapshot собирает состояние сервера.
func (s *Service) Snapshot(ctx context.Context) domain.Status {
	now := s.now()
	st := domain.Status{At: now}
	if h, err := s.host.Stats(ctx); err != nil {
		st.HostErr = err.Error()
	} else {
		st.Host = h
	}
	st.Services = s.host.Services(ctx, s.opts.Services)
	if err := s.host.Site(ctx); err != nil {
		st.SiteErr = err.Error()
	} else {
		st.SiteOK = true
	}
	var err error
	if st.Collector, err = s.store.Collector(ctx); err != nil {
		st.StoreErr = err.Error()
	}
	if st.Notes, err = s.store.Notes(ctx, now); err != nil && st.StoreErr == "" {
		st.StoreErr = err.Error()
	}
	if st.LLM, err = s.store.LLM(ctx, now); err != nil && st.StoreErr == "" {
		st.StoreErr = err.Error()
	}
	st.LLM.Model = s.opts.Model
	s.mu.Lock()
	st.LLM.ProbeAt, st.LLM.ProbeOK = s.lastProbe.ProbeAt, s.lastProbe.ProbeOK
	st.LLM.ProbeErr, st.LLM.ProbeLatency = s.lastProbe.ProbeErr, s.lastProbe.ProbeLatency
	s.mu.Unlock()
	return st
}

// Check сверяет состояние с прошлым и пишет о том, что сломалось и что
// починилось.
func (s *Service) Check(ctx context.Context) {
	st := s.Snapshot(ctx)
	s.mu.Lock()
	raised, cleared := domain.Diff(s.active, st.Problems())
	for _, p := range raised {
		s.active[p.Key] = p.Text
	}
	for _, p := range cleared {
		delete(s.active, p.Key)
	}
	s.mu.Unlock()
	for _, p := range raised {
		s.log.Warn("ops: поломка", "что", p.Key, "текст", p.Text)
		s.send(ctx, "🔴 "+escape(p.Text))
	}
	for _, p := range cleared {
		s.log.Info("ops: починилось", "что", p.Key)
		s.send(ctx, "🟢 починилось: "+escape(p.Text))
	}
}

// Probe делает пробный запрос к нейросети. Узнать, что модель сняли с
// раздачи или кончился лимит, лучше утром от бота, чем после пары.
func (s *Service) Probe(ctx context.Context) {
	if s.probe == nil {
		return
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	latency, err := s.probe.Ping(pctx)
	res := domain.LLM{ProbeAt: s.now(), ProbeOK: err == nil, ProbeLatency: latency}
	if err != nil {
		res.ProbeErr = strings.TrimPrefix(err.Error(), "llm: ")
		s.log.Warn("ops: проба нейросети не прошла", "ошибка", err)
	}
	s.mu.Lock()
	s.lastProbe = res
	s.mu.Unlock()
}

// Start — запуск: проба нейросети и первая проверка.
//
// Бот перезапускается при каждой выкатке, а выкатки идут пачками — отчёт
// при каждом запуске превращал чат в ленту (23.09.2026: шесть за час).
// Поэтому запуск молчит, если всё в порядке, и называет только то, что уже
// сломано. Названное запоминается: первая проверка не повторит его как
// новую поломку.
func (s *Service) Start(ctx context.Context) {
	s.Probe(ctx)
	problems := s.Snapshot(ctx).Problems()
	s.mu.Lock()
	for _, p := range problems {
		s.active[p.Key] = p.Text
	}
	s.mu.Unlock()
	if len(problems) == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("⚠️ <b>При запуске уже не работает</b>\n")
	for _, p := range problems {
		b.WriteString("• " + escape(p.Text) + "\n")
	}
	s.send(ctx, b.String())
}

// reportDue — пора ли отчёта: последний наступивший слот из ReportAt, по
// которому сегодня ещё не отчитывались.
func (s *Service) reportDue(now time.Time) (string, bool) {
	hhmm, slot := now.Format("15:04"), ""
	for _, t := range strings.Split(s.opts.ReportAt, ",") {
		if t = strings.TrimSpace(t); t != "" && hhmm >= t && t > slot {
			slot = t
		}
	}
	if slot == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slot, s.lastReport != now.Format("2006-01-02")+" "+slot
}

// Report — отчёт по расписанию. Приходит и когда всё хорошо: тишина иначе
// неотличима от умершего бота.
func (s *Service) Report(ctx context.Context, now time.Time, slot string) {
	s.mu.Lock()
	s.lastReport = now.Format("2006-01-02") + " " + slot
	s.mu.Unlock()
	title := "☀️ <b>Утренний отчёт</b>"
	if slot >= "15:00" {
		title = "🌙 <b>Вечерний отчёт</b>"
	}
	s.send(ctx, title+"\n\n"+s.Snapshot(ctx).Format())
}

// Handle отвечает на команду из чата.
func (s *Service) Handle(ctx context.Context, cmd Command) {
	name := command(cmd.Text)
	if name == "" {
		return
	}
	// Чат ещё не задан — знакомство: называем id и больше ничего. Состояние
	// сервера — не для любого, кто добавит бота к себе.
	if s.opts.ChatID == 0 {
		if name == "start" {
			s.log.Info("ops: знакомство с чатом", "чат", cmd.ChatID)
			s.sendTo(ctx, cmd.ChatID, "id этого чата: <code>"+strconv.FormatInt(cmd.ChatID, 10)+
				"</code>\nВпишите его в ops.chat_id на сервере — и бот начнёт присылать сюда состояние.")
		}
		return
	}
	if cmd.ChatID != s.opts.ChatID {
		return
	}
	switch name {
	case "start", "status":
		s.send(ctx, s.Snapshot(ctx).Format())
	case "llm":
		s.Probe(ctx)
		s.send(ctx, s.Snapshot(ctx).LLM.Format())
	case "help":
		s.send(ctx, "/status — состояние сервера\n/llm — нейросеть: проба сейчас и расход\n\n"+
			"Сам бот пишет, когда что-то ломается и когда чинится, и присылает отчёт в "+
			strings.ReplaceAll(s.opts.ReportAt, ",", " и ")+".")
	}
}

// command — «status» из «/status@FAsupportingbot аргументы».
func command(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	name := strings.Fields(text[1:])
	if len(name) == 0 {
		return ""
	}
	cmd, _, _ := strings.Cut(name[0], "@")
	return strings.ToLower(cmd)
}

func (s *Service) send(ctx context.Context, html string) {
	if s.opts.ChatID == 0 {
		return
	}
	s.sendTo(ctx, s.opts.ChatID, html)
}

func (s *Service) sendTo(ctx context.Context, chat int64, html string) {
	if err := s.chat.Send(ctx, chat, html); err != nil {
		s.log.Error("ops: сообщение не ушло", "ошибка", err)
	}
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
