package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/zazabag/schedulefu/internal/platform/config"
	"github.com/zazabag/schedulefu/internal/presentation/telegram"
)

// botCmd — бот расписания в Telegram. Живёт отдельной службой: опрос
// Telegram не должен зависеть от того, перезапускается ли сайт.
func botCmd(ctx context.Context, cfg config.Config, log *slog.Logger, _ any) error {
	if cfg.Bot.TelegramToken == "" {
		return errors.New("не задан токен бота: SCHEDULEFU_BOT_TELEGRAM_TOKEN")
	}
	a, err := wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer a.pool.Close()
	origin := strings.TrimRight(cfg.Stand.Origin, "/")
	bot := telegram.New(cfg.Bot.TelegramToken, "", telegram.Deps{Schedule: a.schedule, Clock: a.clock,
		BuildingLabel: buildingLabel, Origin: origin, Log: log})
	log.Info("бот расписания запущен")
	return bot.Run(ctx)
}
