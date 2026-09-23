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
	// DailyAt — время утреннего отчёта, «ЧЧ:ММ» в поясе Location.
	DailyAt  string
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

	mu        sync.Mutex
	active    map[string]string // текущие тревоги: ключ → текст
	lastProbe domain.LLM        // только поля пробы
	lastDaily string            // дата последнего утреннего отчёта
}

// New собирает присмотр.
func New(chat Messenger, host Host, store Store, probe Prober, log *slog.Logger, opts Options) *Service {
	if opts.Every <= 0 {
		opts.Every = 5 * time.Minute
	}
	if opts.ProbeEvery <= 0 {
		opts.ProbeEvery = 6 * time.Hour
	}
	if opts.DailyAt == "" {
		opts.DailyAt = "09:00"
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

// Run крутит присмотр, пока жив контекст: проверки, пробы, утренний отчёт
// и ответы на команды.
func (s *Service) Run(ctx context.Context) error {
	updates := s.chat.Updates(ctx)
	s.Probe(ctx)
	// Первая проверка — молча: тревоги, которые уже горят, бот назовёт
	// сообщением о запуске, а не пачкой «новых поломок».
	st := s.Snapshot(ctx)
	s.mu.Lock()
	for _, p := range st.Problems() {
		s.active[p.Key] = p.Text
	}
	s.mu.Unlock()
	if s.opts.ChatID != 0 {
		s.send(ctx, "🤖 <b>Присмотр запущен</b>\n\n"+st.Format())
	}

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
			if now := s.now(); s.dailyDue(now) {
				s.Daily(ctx, now)
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

// dailyDue — пора ли утреннего отчёта: время наступило, а сегодня его ещё
// не было.
func (s *Service) dailyDue(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Format("15:04") >= s.opts.DailyAt && s.lastDaily != now.Format("2006-01-02")
}

// Daily — утренний отчёт. Приходит и когда всё хорошо: тишина иначе
// неотличима от умершего бота.
func (s *Service) Daily(ctx context.Context, now time.Time) {
	s.mu.Lock()
	s.lastDaily = now.Format("2006-01-02")
	s.mu.Unlock()
	s.send(ctx, "☀️ <b>Утренний отчёт</b>\n\n"+s.Snapshot(ctx).Format())
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
			"Сам бот пишет, когда что-то ломается и когда чинится, и присылает утренний отчёт в "+s.opts.DailyAt+".")
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
