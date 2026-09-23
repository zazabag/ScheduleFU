package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
	noteschat "github.com/zazabag/schedulefu/internal/modules/notes/infrastructure/llm"
	notespg "github.com/zazabag/schedulefu/internal/modules/notes/infrastructure/postgres"
	"github.com/zazabag/schedulefu/internal/modules/ops"
	opshost "github.com/zazabag/schedulefu/internal/modules/ops/infrastructure/host"
	opspg "github.com/zazabag/schedulefu/internal/modules/ops/infrastructure/postgres"
	"github.com/zazabag/schedulefu/internal/modules/ops/transport/telegram"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/config"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

// opsCmd — присмотр за сервером: служебный чат в Telegram.
func opsCmd(ctx context.Context, cfg config.Config, log *slog.Logger, _ any) error {
	if cfg.Ops.TelegramToken == "" {
		return errors.New("не задан токен бота: SCHEDULEFU_OPS_TELEGRAM_TOKEN")
	}
	pool, err := db.Open(ctx, cfg.DB.DSN)
	if err != nil {
		return err
	}
	defer pool.Close()
	clk, _ := clock.New(cfg.Source.Timezone)

	services := []string{"serve", "collect"}
	var probe ops.Prober
	if cfg.Notes.Enabled {
		services = append(services, "notes")
		chat := llmChat(cfg, notespg.New(pool), log)
		if chat.Configured() {
			probe = chat
		}
	}
	svc := ops.New(telegram.New(cfg.Ops.TelegramToken, ""), opshost.New(healthURL(cfg.HTTP.Addr)), opspg.New(pool),
		probe, log, ops.Options{
			ChatID: cfg.Ops.ChatID, Services: services, Model: cfg.Notes.LLM.Model,
			Every: cfg.Ops.Every, ProbeEvery: cfg.Ops.ProbeEvery, ReportAt: cfg.Ops.ReportAt,
			Location: clk.Location(),
		})
	if cfg.Ops.ChatID == 0 {
		log.Warn("ops: служебный чат не задан — бот только назовёт id чата в ответ на /start")
	}
	log.Info("присмотр запущен", "чат", cfg.Ops.ChatID)
	return svc.Run(ctx)
}

// llmChat — адаптер модели конспекта с учётом каждого вызова в llm_calls.
// Один на обработку записей и на пробу присмотра: проба учитывается так же,
// как настоящий вызов.
func llmChat(cfg config.Config, repo *notespg.Repo, log *slog.Logger) *noteschat.Chat {
	return noteschat.New(noteschat.Options{BaseURL: cfg.Notes.LLM.BaseURL, APIKey: cfg.Notes.LLM.APIKey,
		Model: cfg.Notes.LLM.Model, NoThinking: cfg.Notes.LLM.NoThinking,
		MaxChars: cfg.Notes.LLM.MaxChars, Timeout: cfg.Notes.LLM.Timeout,
		Record: func(ctx context.Context, c domain.LLMCall) {
			// Учёт не должен ронять конспект: ошибка записи только в лог.
			if err := repo.LogLLMCall(context.WithoutCancel(ctx), c); err != nil {
				log.Warn("учёт вызова модели не записан", "ошибка", err)
			}
		}})
}

// healthURL — адрес проверки сайта изнутри машины по адресу, который
// слушает serve: «:8090» и «127.0.0.1:8090» — оба на петле.
func healthURL(addr string) string {
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr + "/api/v1/health"
}
