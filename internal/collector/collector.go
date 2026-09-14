package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zazabag/schedulefu/internal/ruz"
	"github.com/zazabag/schedulefu/internal/store"
)

// Notifier получает уведомление о том, что проход нашёл изменения.
//
// Интерфейс, а не прямая зависимость от пакета push: сборщик должен
// работать и без уведомлений — например, при первом наполнении базы,
// когда «изменением» считается всё расписание разом.
type Notifier interface {
	PlanSince(ctx context.Context, since time.Time) (int, error)
}

// Collector собирает расписание обходом аудиторий.
//
// Обход идёт именно по аудиториям, а не по группам: в каждой паре уже есть
// и группа, и преподаватель, и аудитория, поэтому один проход по известным
// аудиториям даёт полный слепок расписания вуза за период. Расписания групп
// и преподавателей строятся из него локально, без отдельных запросов.
type Collector struct {
	client *ruz.Client
	store  *store.Store
	log    *slog.Logger

	// Workers — сколько аудиторий опрашивается одновременно. Ограничение
	// частоты живёт в клиенте; здесь ограничивается только параллелизм.
	Workers int

	// Notifier необязателен: без него сборщик просто копит изменения в
	// журнале, никого не беспокоя.
	Notifier Notifier

	// KeepChanges — сколько хранить журнал изменений.
	KeepChanges time.Duration
}

// New создаёт сборщик.
func New(c *ruz.Client, s *store.Store, log *slog.Logger) *Collector {
	if log == nil {
		log = slog.Default()
	}
	return &Collector{
		client: c, store: s, log: log, Workers: 6,
		// Месяц: столько имеет смысл отвечать на вопрос «а что поменялось»,
		// дальше расписание успевает смениться целиком.
		KeepChanges: 30 * 24 * time.Hour,
	}
}

// RunOnce делает один полный проход по аудиториям за период и применяет
// результат к хранилищу.
//
// Период должен быть коротким — день или неделя. Выкачивать весь семестр
// нельзя: это уже не «посмотреть расписание», а копирование базы вуза,
// см. docs/03-legal-risks.md.
func (c *Collector) RunOnce(ctx context.Context, auditoriumOids []int64, from, to time.Time) (store.ApplyResult, error) {
	started := time.Now()
	runID, err := c.store.StartRun(ctx, from, to)
	if err != nil {
		return store.ApplyResult{}, err
	}

	lessons, stats := c.fetchAll(ctx, auditoriumOids, from, to)

	// Если источник массово не отвечает, применять такой слепок опасно:
	// половина пар «исчезнет» и превратится в лавину ложных уведомлений
	// об отменах. Лучше пропустить проход целиком.
	if stats.failed > 0 && stats.ok == 0 {
		err := fmt.Errorf("источник не ответил ни на один из %d запросов", stats.failed)
		c.store.FinishRun(ctx, runID, stats.ok+stats.failed, stats.failed, 0, 0, err)
		return store.ApplyResult{}, err
	}
	if share := float64(stats.failed) / float64(stats.ok+stats.failed); share > 0.2 {
		err := fmt.Errorf("не отвечает %.0f%% аудиторий (%d из %d), слепок не применён",
			share*100, stats.failed, stats.ok+stats.failed)
		c.store.FinishRun(ctx, runID, stats.ok+stats.failed, stats.failed, 0, 0, err)
		return store.ApplyResult{}, err
	}

	res, err := c.store.ApplySnapshot(ctx, from, to, lessons)
	if err != nil {
		c.store.FinishRun(ctx, runID, stats.ok+stats.failed, stats.failed, len(lessons), 0, err)
		return store.ApplyResult{}, err
	}

	// Справочники пополняем из того же слепка: отдельных запросов не нужно.
	if err := c.store.UpsertAuditoriumsFromLessons(ctx, lessons); err != nil {
		c.log.Warn("справочник аудиторий не обновлён", "ошибка", err)
	}
	if err := c.store.UpsertLecturersFromLessons(ctx, lessons); err != nil {
		c.log.Warn("справочник преподавателей не обновлён", "ошибка", err)
	}

	if err := c.store.FinishRun(ctx, runID, stats.ok+stats.failed, stats.failed,
		len(lessons), res.Changes(), nil); err != nil {
		c.log.Warn("итог прохода не записан", "ошибка", err)
	}

	if c.Notifier != nil && res.Changes() > 0 {
		// Момент начала прохода, а не его конца: изменения записаны
		// где-то посередине, и по концу выборка не нашла бы ничего.
		if queued, err := c.Notifier.PlanSince(ctx, started); err != nil {
			c.log.Warn("уведомления не запланированы", "ошибка", err)
		} else if queued > 0 {
			c.log.Info("уведомления запланированы", "писем", queued)
		}
	}

	// Журнал нужен на дни, а не на месяцы: и уведомления, и экран
	// «что изменилось» смотрят недавнее. Чистим здесь, потому что здесь
	// журнал и растёт.
	if removed, err := c.store.CleanupChanges(ctx, c.KeepChanges); err != nil {
		c.log.Warn("журнал изменений не подчищен", "ошибка", err)
	} else if removed > 0 {
		c.log.Info("журнал подчищен", "удалено", removed)
	}

	c.log.Info("проход завершён",
		"аудиторий", len(auditoriumOids),
		"пар", len(lessons),
		"добавлено", res.Added,
		"изменено", res.Changed,
		"удалено", res.Removed,
		"ошибок", stats.failed,
		"за", time.Since(started).Round(time.Second))
	return res, nil
}

type fetchStats struct {
	ok     int
	failed int
}

// fetchAll опрашивает аудитории параллельно и собирает пары.
func (c *Collector) fetchAll(ctx context.Context, oids []int64, from, to time.Time) ([]store.Lesson, fetchStats) {
	workers := c.Workers
	if workers <= 0 {
		workers = 6
	}

	var (
		mu      sync.Mutex
		lessons []store.Lesson
		stats   fetchStats
		// Одна пара приходит столько раз, сколько аудиторий её упоминают
		// (обычно один, но поточные занятия могут повторяться), поэтому
		// дедуплицируем по lessonOid.
		seen = map[int64]bool{}
	)

	jobs := make(chan int64)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for oid := range jobs {
				raw, err := c.client.Schedule(ctx, ruz.KindAuditorium, oid, from, to)
				mu.Lock()
				if err != nil {
					stats.failed++
					mu.Unlock()
					c.log.Debug("аудитория не опрошена", "oid", oid, "ошибка", err)
					continue
				}
				stats.ok++
				for _, r := range raw {
					l, ok := ToStoreLesson(r)
					if !ok || seen[l.LessonOid] {
						continue
					}
					seen[l.LessonOid] = true
					lessons = append(lessons, l)
				}
				mu.Unlock()
			}
		}()
	}

	for _, oid := range oids {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return lessons, stats
		case jobs <- oid:
		}
	}
	close(jobs)
	wg.Wait()
	return lessons, stats
}
