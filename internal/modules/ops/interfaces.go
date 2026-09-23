// Package ops — присмотр за сервером: состояние, тревоги и ежедневный отчёт
// в служебный чат Telegram.
//
// Зачем: сервер один, людей при нём нет, а поломки здесь тихие. Сбор
// расписания упал — сайт продолжает отдавать вчерашнее; нейросеть
// исчерпала лимит — студент узнаёт об этом после пары, получив пустоту.
// Модуль замечает это раньше и пишет в чат о смене состояния.
//
// Таблиц у модуля нет. Он читает чужие — collector_runs (schedule),
// recordings и llm_calls (notes) — напрямую, только для отчёта; так
// разрешает § 5 канона при договорённости, записанной здесь: Store только
// читает и ничего не пишет.
package ops

import (
	"context"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/ops/domain"
)

// Command — сообщение из чата: команда и откуда она пришла.
type Command struct {
	ChatID int64
	Text   string
}

// Messenger — служебный чат.
type Messenger interface {
	Send(ctx context.Context, chatID int64, html string) error
	// Updates — входящие команды до отмены контекста.
	Updates(ctx context.Context) <-chan Command
}

// Host — сама машина: ресурсы, службы, отвечает ли сайт.
type Host interface {
	Stats(ctx context.Context) (domain.Host, error)
	Services(ctx context.Context, names []string) []domain.Service
	Site(ctx context.Context) error
}

// Store — отчётные запросы к чужим таблицам. Только чтение.
type Store interface {
	Collector(ctx context.Context) (domain.Collector, error)
	Notes(ctx context.Context, now time.Time) (domain.Notes, error)
	LLM(ctx context.Context, now time.Time) (domain.LLM, error)
}

// Prober — пробный запрос к нейросети: жива ли модель и ключ. Реализует
// адаптер llm модуля notes, связывает cmd.
type Prober interface {
	Ping(ctx context.Context) (time.Duration, error)
}
