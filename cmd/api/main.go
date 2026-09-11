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
	"github.com/zazabag/schedulefu/internal/store"
)

func main() {
	var (
		addr = flag.String("addr", env("ADDR", ":8080"), "адрес прослушивания")
		dsn  = flag.String("dsn", env("DATABASE_URL", "postgres://localhost:5432/schedulefu_dev?sslmode=disable"), "адрес базы")
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

	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.New(st, loc).Routes(),
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
