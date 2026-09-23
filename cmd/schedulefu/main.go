// schedulefu — единственный исполняемый файл проекта.
//
//	schedulefu serve      страницы, /api/v1, отправка уведомлений
//	schedulefu collect    проход по источнику (один: -once)
//	schedulefu notes      обработка записей пар: расшифровка и конспект
//	schedulefu ops        присмотр за сервером: служебный чат в Telegram
//	schedulefu seed       справочники из data/*.json
//	schedulefu static     сборка версии для GitHub Pages
//	schedulefu vapid      ключи уведомлений
//	schedulefu migrate    миграции без запуска сервера
//
// Это composition root: единственное место, которое знает все модули
// поимённо и связывает порты с реализациями. Композиция явная, руками — на
// пяти модулях контейнер прятал бы порядок инициализации, который лучше
// видеть.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zazabag/schedulefu/internal/platform/config"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	cfgPath := fs.String("config", os.Getenv("SCHEDULEFU_CONFIG"), "путь к YAML-конфигу")
	verbose := fs.Bool("v", false, "подробный лог")

	run, ok := commands[cmd]
	if !ok {
		usage()
		os.Exit(2)
	}
	// Флаги подкоманды регистрируются до разбора.
	extra := run.flags(fs)
	_ = fs.Parse(args)

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("конфигурация", "ошибка", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run.do(ctx, cfg, log, extra); err != nil {
		log.Error(cmd, "ошибка", err)
		os.Exit(1)
	}
}

type command struct {
	help  string
	flags func(*flag.FlagSet) any
	do    func(ctx context.Context, cfg config.Config, log *slog.Logger, extra any) error
}

func usage() {
	fmt.Fprintln(os.Stderr, "использование: schedulefu <команда> [флаги]")
	for _, name := range []string{"serve", "collect", "notes", "ops", "seed", "static", "vapid", "migrate"} {
		fmt.Fprintf(os.Stderr, "  %-9s %s\n", name, commands[name].help)
	}
}
