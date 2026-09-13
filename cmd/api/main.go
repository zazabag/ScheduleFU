// Команда api — HTTP-сервер поверх собранного расписания.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zazabag/schedulefu/internal/httpapi"
	"github.com/zazabag/schedulefu/internal/httpx"
	"github.com/zazabag/schedulefu/internal/store"
	"github.com/zazabag/schedulefu/internal/web"
)

func main() {
	var (
		addr       = flag.String("addr", env("ADDR", ":8080"), "адрес прослушивания")
		dsn        = flag.String("dsn", env("DATABASE_URL", "postgres://localhost:5432/schedulefu_dev?sslmode=disable"), "адрес базы")
		rps        = flag.Float64("rps", 8, "запросов в секунду с одного адреса")
		burst      = flag.Float64("burst", 30, "разрешённый всплеск запросов")
		trustProxy = flag.Bool("trust-proxy", false, "доверять X-Forwarded-For (включать только за обратным прокси)")
	)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dsn)
	if err != nil {
		log.Error("база недоступна", "ошибка", err)
		os.Exit(1)
	}
	defer st.Close()

	// Время вуза московское; сервер может стоять где угодно.
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		loc = time.FixedZone("MSK", 3*60*60)
	}

	// JSON-API и веб-интерфейс живут в одном процессе: API нужен боту и
	// экспорту в календарь, страницы — людям.
	site, err := web.New(st, loc)
	if err != nil {
		log.Error("не удалось собрать интерфейс", "ошибка", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", httpapi.New(st, loc).Routes())
	mux.Handle("/", site.Routes())

	// Заголовок X-Forwarded-For подделывается тривиально, поэтому доверять
	// ему можно только когда перед сервисом стоит наш обратный прокси.
	httpx.TrustProxy = *trustProxy

	limiter := httpx.NewRateLimiter(*rps, *burst)
	limiter.StartCleanup(5*time.Minute, 15*time.Minute, ctx.Done())

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpx.SecurityHeaders(limiter.Middleware(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	go func() {
		log.Info("сервер запущен", "адрес", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("сервер остановлен с ошибкой", "ошибка", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("не удалось корректно остановить сервер", "ошибка", err)
	}
	log.Info("остановлен")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
