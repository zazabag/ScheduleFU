package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zazabag/schedulefu/internal/modules/export"
	"github.com/zazabag/schedulefu/internal/modules/notify"
	notifypg "github.com/zazabag/schedulefu/internal/modules/notify/infrastructure/postgres"
	"github.com/zazabag/schedulefu/internal/modules/notify/transport/webpush"
	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	schedpg "github.com/zazabag/schedulefu/internal/modules/schedule/infrastructure/postgres"
	"github.com/zazabag/schedulefu/internal/modules/source"
	"github.com/zazabag/schedulefu/internal/modules/source/ruz"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/config"
	"github.com/zazabag/schedulefu/internal/platform/db"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
	"github.com/zazabag/schedulefu/internal/presentation/api"
	"github.com/zazabag/schedulefu/internal/presentation/static"
	"github.com/zazabag/schedulefu/internal/presentation/web"
)

var commands = map[string]command{
	"serve":   {help: "HTTP: страницы, /api/v1, отправка уведомлений", flags: noFlags, do: serve},
	"collect": {help: "проход по источнику; -once — один и выйти", flags: collectFlags, do: collect},
	"seed":    {help: "справочники из data/*.json", flags: seedFlags, do: seed},
	"static":  {help: "сборка версии для GitHub Pages", flags: noFlags, do: buildStatic},
	"vapid":   {help: "новая пара ключей уведомлений", flags: noFlags, do: vapid},
	"migrate": {help: "применить миграции и выйти", flags: noFlags, do: migrate},
}

func noFlags(*flag.FlagSet) any { return nil }

// app — собранное приложение: все модули, связанные портами.
type app struct {
	pool     *pgxpool.Pool
	clock    *clock.Clock
	src      source.Source
	schedule *schedule.Service
	notify   *notify.Service
	keys     webpush.Keys
}

func wire(ctx context.Context, cfg config.Config, log *slog.Logger) (*app, error) {
	pool, err := db.Open(ctx, cfg.DB.DSN)
	if err != nil {
		return nil, err
	}
	clk, _ := clock.New(cfg.Source.Timezone)
	src := ruz.New(ruz.Options{BaseURL: cfg.Source.BaseURL, RPS: cfg.Source.RPS})

	schedRepo := schedpg.New(pool)
	schedSvc := schedule.New(src, schedRepo, clk, log, schedule.Options{Workers: cfg.Source.Workers, OnlyStudySpaces: true})

	a := &app{pool: pool, clock: clk, src: src, schedule: schedSvc}
	// Уведомления включаются только с ключами: без них сервис просто
	// работает без них, а не падает.
	if cfg.PushEnabled() {
		a.keys = webpush.Keys{Public: cfg.Notify.VAPIDPublic, Private: cfg.Notify.VAPIDPrivate, Subject: cfg.Notify.Subject}
		a.notify = notify.New(notifypg.New(pool), schedRepo, clk.Location(), log, webpush.New(a.keys))
		schedSvc.Notifier = a.notify
	}
	return a, nil
}

func (a *app) close() { a.pool.Close() }

// buildingLabel — короткая подпись корпуса. Полный адрес источника в
// строке аудитории не помещается, а различать корпуса необходимо: номера
// в них похожи (313 и 0314).
func buildingLabel(b string) string { return ruz.SiteOf(b).Label }

// ─── serve ───────────────────────────────────────────────────────────────────

func serve(ctx context.Context, cfg config.Config, log *slog.Logger, _ any) error {
	a, err := wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.close()

	calendar := func(w http.ResponseWriter, r *http.Request) {
		subj, err := sched.SubjectFromValues(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if subj.IsZero() {
			subj = web.SubjectFromCookie(r)
		}
		if subj.IsZero() {
			http.Error(w, "не указано, чьё расписание выгружать", http.StatusBadRequest)
			return
		}
		body, err := export.Calendar(r.Context(), a.schedule, subj, a.clock.Now(), a.clock.Location(), buildingLabel)
		if err != nil {
			http.Error(w, "не удалось собрать календарь", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		w.Header().Set("Content-Disposition", `inline; filename="schedulefu.ics"`)
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(body))
	}

	site, err := web.New(web.Deps{Schedule: a.schedule, Notify: a.notify, Clock: a.clock, BuildingLabel: buildingLabel, Calendar: calendar, Dev: cfg.Stand.Env == "dev"})
	if err != nil {
		return err
	}
	jsonAPI := api.New(api.Deps{Schedule: a.schedule, Notify: a.notify, Clock: a.clock, PushKey: a.keys.Public, StandEnv: cfg.Stand.Env, Calendar: calendar})

	mux := http.NewServeMux()
	mux.Handle("/api/", jsonAPI.Routes())
	mux.Handle("/", site.Routes())

	// X-Forwarded-For подделывается тривиально: доверяем только за прокси.
	httpx.TrustProxy = cfg.HTTP.TrustProxy
	limiter := httpx.NewRateLimiter(cfg.HTTP.RPS, cfg.HTTP.Burst)
	limiter.StartCleanup(5*time.Minute, 15*time.Minute, ctx.Done())

	if a.notify != nil {
		go a.notify.Run(ctx, 30*time.Second)
		log.Info("уведомления включены")
	} else {
		log.Info("уведомления выключены: ключи не заданы")
	}

	srv := &http.Server{Addr: cfg.HTTP.Addr, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second,
		// Сжатие снаружи ограничителя: отказ 429 — тоже ответ.
		Handler: httpx.Compress(httpx.SecurityHeaders(limiter.Middleware(mux)))}
	go func() {
		log.Info("сервер запущен", "адрес", cfg.HTTP.Addr, "стенд", cfg.Stand.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("сервер остановлен с ошибкой", "ошибка", err)
			os.Exit(1)
		}
	}()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

// ─── collect ─────────────────────────────────────────────────────────────────

type collectOpts struct {
	once     *bool
	noNotify *bool
}

func collectFlags(fs *flag.FlagSet) any {
	return collectOpts{once: fs.Bool("once", false, "один проход и выйти"),
		noNotify: fs.Bool("no-notify", false, "не планировать уведомления (первое наполнение)")}
}

func collect(ctx context.Context, cfg config.Config, log *slog.Logger, extra any) error {
	o := extra.(collectOpts)
	a, err := wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.close()
	if *o.noNotify {
		a.schedule.Notifier = nil
	}
	run := func() {
		from := a.clock.Today()
		if _, err := a.schedule.Collect(ctx, from, from.AddDate(0, 0, cfg.Source.Days-1)); err != nil {
			log.Error("проход не удался", "ошибка", err)
		}
	}
	run()
	if *o.once {
		return nil
	}
	// Цикл внутри процесса, а не в планировщике снаружи: переживает ночь
	// одним процессом и не пересоздаёт соединения.
	t := time.NewTicker(cfg.Source.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			run()
		}
	}
}

// ─── seed ────────────────────────────────────────────────────────────────────

func seedFlags(fs *flag.FlagSet) any {
	return fs.String("data", "data", "папка со справочниками")
}

// seed загружает справочники, собранные скриптами tools/: там есть тип
// аудитории и вместимость, которых в парах нет.
func seed(ctx context.Context, cfg config.Config, log *slog.Logger, extra any) error {
	dir := *extra.(*string)
	a, err := wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.close()

	var auds struct {
		Auditoriums []struct {
			Oid      int64  `json:"oid"`
			Name     string `json:"name"`
			Building string `json:"building"`
			Kind     string `json:"kind"`
			Capacity *int   `json:"capacity"`
		} `json:"auditoriums"`
	}
	if err := readJSON(dir+"/auditoriums.json", &auds); err != nil {
		return err
	}
	var found []source.SearchResult
	for _, r := range auds.Auditoriums {
		found = append(found, source.SearchResult{ID: fmt.Sprint(r.Oid), Label: r.Name,
			Description: r.Name + " | " + r.Building + " | " + r.Kind})
	}
	if err := a.schedule.SeedAuditoriums(ctx, found, nil); err != nil {
		return err
	}
	log.Info("справочник аудиторий загружен", "всего", len(found))

	var grp struct {
		Groups []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			FacultyOid string `json:"faculty_oid"`
		} `json:"groups"`
	}
	if err := readJSON(dir+"/groups.json", &grp); err != nil {
		return err
	}
	var groups []sched.Group
	for _, g := range grp.Groups {
		groups = append(groups, sched.Group{ID: g.ID, Name: g.Name, FacultyOid: g.FacultyOid, AdmissionYear: admissionYear(g.Name)})
	}
	if err := a.schedule.Repo().UpsertGroups(ctx, groups); err != nil {
		return err
	}
	log.Info("справочник групп загружен", "всего", len(groups))
	return nil
}

func admissionYear(name string) *int {
	n, count := 0, 0
	for _, r := range name {
		if r >= '0' && r <= '9' {
			n, count = n*10+int(r-'0'), count+1
			if count == 2 {
				break
			}
			continue
		}
		if count > 0 {
			break
		}
	}
	if count != 2 {
		return nil
	}
	y := 2000 + n
	return &y
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("чтение %s: %w", path, err)
	}
	return json.Unmarshal(raw, dst)
}

// ─── static ──────────────────────────────────────────────────────────────────

func buildStatic(ctx context.Context, cfg config.Config, log *slog.Logger, _ any) error {
	a, err := wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.close()
	n, err := static.Build(ctx, a.schedule.Repo(), a.clock, static.Options{OutDir: cfg.Static.OutDir, APIBase: cfg.Static.APIBase, BuildingLabel: buildingLabel})
	if err != nil {
		return err
	}
	log.Info("сайт собран", "папка", cfg.Static.OutDir, "дней", n)
	return nil
}

// ─── vapid, migrate ──────────────────────────────────────────────────────────

func vapid(_ context.Context, _ config.Config, _ *slog.Logger, _ any) error {
	k, err := webpush.Generate()
	if err != nil {
		return err
	}
	fmt.Printf("SCHEDULEFU_NOTIFY_VAPID_PUBLIC=%s\nSCHEDULEFU_NOTIFY_VAPID_PRIVATE=%s\n", k.Public, k.Private)
	fmt.Fprintln(os.Stderr, "\nПриватный ключ в репозиторий не коммитить. Смена ключей обнуляет все подписки.")
	return nil
}

func migrate(ctx context.Context, cfg config.Config, log *slog.Logger, _ any) error {
	pool, err := db.Open(ctx, cfg.DB.DSN)
	if err != nil {
		return err
	}
	pool.Close()
	log.Info("миграции применены")
	return nil
}
