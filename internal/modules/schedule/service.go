package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/modules/source"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Notifier получает момент, после которого искать изменения для рассылки.
// Интерфейс, а не зависимость от notify: сборщик обязан работать и без
// уведомлений — при первом наполнении базы, например.
type Notifier interface {
	PlanSince(ctx context.Context, since time.Time) (int, error)
}

// Options — настройки сервиса.
type Options struct {
	Workers     int
	KeepChanges time.Duration
	// OnlyStudySpaces — опрашивать только учебные аудитории. Спортзалы и
	// чужие помещения дают запросы, которые никому не нужны.
	OnlyStudySpaces bool
}

// Service — сервис модуля.
type Service struct {
	src      source.Source
	repo     Repository
	clock    *clock.Clock
	log      *slog.Logger
	opts     Options
	Notifier Notifier
}

// New создаёт сервис.
func New(src source.Source, repo Repository, clk *clock.Clock, log *slog.Logger, opts Options) *Service {
	if log == nil {
		log = slog.Default()
	}
	if opts.Workers <= 0 {
		opts.Workers = 6
	}
	if opts.KeepChanges <= 0 {
		// Месяц: столько имеет смысл отвечать на «а что поменялось», дальше
		// расписание успевает смениться целиком.
		opts.KeepChanges = 30 * 24 * time.Hour
	}
	return &Service{src: src, repo: repo, clock: clk, log: log, opts: opts}
}

// ─── сбор ────────────────────────────────────────────────────────────────────

// Collect делает один проход: обходит аудитории, применяет слепок, пополняет
// справочники, чистит журнал и планирует уведомления.
//
// Обход по аудиториям, а не по группам: в каждой паре уже есть и группа, и
// преподаватель, поэтому один проход даёт полный слепок вуза за период.
// Период короткий — день или неделя: выкачивать семестр нельзя по праву.
func (s *Service) Collect(ctx context.Context, from, to time.Time) (domain.ApplyResult, error) {
	started := s.clock.Now()
	runID, err := s.repo.StartRun(ctx, from, to)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	oids, err := s.repo.AuditoriumOids(ctx, s.opts.OnlyStudySpaces)
	if err != nil {
		return domain.ApplyResult{}, err
	}
	if len(oids) == 0 {
		return domain.ApplyResult{}, fmt.Errorf("справочник аудиторий пуст — нужен посев")
	}

	lessons, seen, stats := s.fetchAll(ctx, oids, from, to)
	total := stats.ok + stats.failed

	// Если источник массово не отвечает, применять слепок опасно: половина
	// пар «исчезнет» и превратится в лавину ложных отмен. Лучше пропустить.
	if total > 0 && float64(stats.failed)/float64(total) > 0.2 {
		err := fmt.Errorf("не отвечает %d из %d аудиторий, слепок не применён", stats.failed, total)
		_ = s.repo.FinishRun(ctx, runID, total, stats.failed, 0, 0, err)
		return domain.ApplyResult{}, err
	}

	res, err := s.repo.ApplySnapshot(ctx, from, to, lessons)
	if err != nil {
		_ = s.repo.FinishRun(ctx, runID, total, stats.failed, len(lessons), 0, err)
		return domain.ApplyResult{}, err
	}
	// Справочники пополняются из того же слепка: отдельных запросов не нужно.
	if err := s.repo.UpsertAuditoriums(ctx, seen.auditoriums()); err != nil {
		s.log.Warn("справочник аудиторий не обновлён", "ошибка", err)
	}
	if err := s.repo.UpsertLecturers(ctx, seen.lecturers()); err != nil {
		s.log.Warn("справочник преподавателей не обновлён", "ошибка", err)
	}
	if removed, err := s.repo.CleanupChanges(ctx, s.opts.KeepChanges); err != nil {
		s.log.Warn("журнал не подчищен", "ошибка", err)
	} else if removed > 0 {
		s.log.Info("журнал подчищен", "удалено", removed)
	}
	if s.Notifier != nil && res.Changes() > 0 {
		// От начала прохода: изменения записаны посередине, по концу выборка
		// не нашла бы ничего.
		if n, err := s.Notifier.PlanSince(ctx, started); err != nil {
			s.log.Warn("уведомления не запланированы", "ошибка", err)
		} else if n > 0 {
			s.log.Info("уведомления запланированы", "писем", n)
		}
	}
	_ = s.repo.FinishRun(ctx, runID, total, stats.failed, len(lessons), res.Changes(), nil)
	s.log.Info("проход завершён", "аудиторий", len(oids), "пар", len(lessons),
		"добавлено", res.Added, "изменено", res.Changed, "удалено", res.Removed,
		"ошибок", stats.failed, "за", time.Since(started).Round(time.Second))
	return res, nil
}

type fetchStats struct{ ok, failed int }

// seenRefs копит справочные записи, встреченные в слепке.
type seenRefs struct {
	auds map[int64]domain.Auditorium
	lecs map[int64]string
}

func (s seenRefs) auditoriums() []domain.Auditorium {
	out := make([]domain.Auditorium, 0, len(s.auds))
	for _, a := range s.auds {
		out = append(out, a)
	}
	return out
}

func (s seenRefs) lecturers() []domain.Lecturer {
	out := make([]domain.Lecturer, 0, len(s.lecs))
	for oid, name := range s.lecs {
		out = append(out, domain.Lecturer{Oid: oid, Name: name})
	}
	return out
}

func (s *Service) fetchAll(ctx context.Context, oids []int64, from, to time.Time) ([]domain.Lesson, seenRefs, fetchStats) {
	var (
		mu      sync.Mutex
		lessons []domain.Lesson
		stats   fetchStats
		refs    = seenRefs{auds: map[int64]domain.Auditorium{}, lecs: map[int64]string{}}
		dedup   = map[int64]bool{} // поточная пара приходит из каждой её аудитории
		jobs    = make(chan int64)
		wg      sync.WaitGroup
	)
	loc := s.clock.Location()
	for i := 0; i < s.opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for oid := range jobs {
				raw, err := s.src.Schedule(ctx, source.KindAuditorium, oid, from, to)
				mu.Lock()
				if err != nil {
					stats.failed++
					mu.Unlock()
					s.log.Debug("аудитория не опрошена", "oid", oid, "ошибка", err)
					continue
				}
				stats.ok++
				for _, r := range raw {
					l, ok := fromSource(r, loc)
					if !ok || dedup[l.LessonOid] {
						continue
					}
					dedup[l.LessonOid] = true
					lessons = append(lessons, l)
					if l.AuditoriumOid != nil {
						refs.auds[*l.AuditoriumOid] = auditoriumFrom(*l.AuditoriumOid,
							s.src.ParseAuditorium(r.Auditorium, r.Building), "", r.AuditoriumAmount)
					}
					if l.LecturerOid != nil && l.LecturerName != "" {
						refs.lecs[*l.LecturerOid] = l.LecturerName
					}
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
			return lessons, refs, stats
		case jobs <- oid:
		}
	}
	close(jobs)
	wg.Wait()
	return lessons, refs, stats
}

// SeedFromSearch наполняет справочник аудиторий через поиск источника: в
// выдаче есть тип аудитории, которого в парах нет.
func (s *Service) SeedAuditoriums(ctx context.Context, found []source.SearchResult, extra []source.Auditorium) error {
	var items []domain.Auditorium
	for _, r := range found {
		oid, ok := parseID(r.ID)
		if !ok {
			continue
		}
		parts := strings.Split(r.Description, "|")
		building, kind := "", ""
		if len(parts) > 1 {
			building = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			kind = strings.TrimSpace(parts[2])
		}
		items = append(items, auditoriumFrom(oid, s.src.ParseAuditorium(r.Label, building), kind, 0))
	}
	return s.repo.UpsertAuditoriums(ctx, items)
}

func parseID(s string) (int64, bool) {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int64(r-'0')
	}
	return n, n > 0
}

// ─── запросы ─────────────────────────────────────────────────────────────────

// FreeRooms — свободные сейчас аудитории площадки с полосой занятости.
// «Свободна» значит ровно одно: в расписании нет пары на этот момент.
// Брони вне расписания источник не публикует, и об этом обязан говорить
// интерфейс, а не молчать сервис.
func (s *Service) FreeRooms(ctx context.Context, site string, at time.Time) ([]domain.RoomView, int, error) {
	days, err := s.repo.SiteDay(ctx, site, at)
	if err != nil {
		return nil, 0, err
	}
	now := at.In(s.clock.Location()).Format("15:04")
	var free []domain.RoomView
	for _, d := range days {
		v := domain.BuildRoomView(d.Auditorium, d.Lessons, now)
		if v.FreeNow {
			free = append(free, v)
		}
	}
	return free, len(days), nil
}

// SiteNow — свободные аудитории и сводка площадки на момент: сколько
// свободно сейчас, сколько будет свободно в каждую пару, как по этажам.
// Сводка и список считаются из одних полос — расходиться им негде.
func (s *Service) SiteNow(ctx context.Context, site string, at time.Time) ([]domain.RoomView, domain.SiteSummary, error) {
	days, err := s.repo.SiteDay(ctx, site, at)
	if err != nil {
		return nil, domain.SiteSummary{}, err
	}
	now := at.In(s.clock.Location()).Format("15:04")
	all := make([]domain.RoomView, 0, len(days))
	var free []domain.RoomView
	for _, d := range days {
		v := domain.BuildRoomView(d.Auditorium, d.Lessons, now)
		all = append(all, v)
		if v.FreeNow {
			free = append(free, v)
		}
	}
	return free, domain.BuildSiteSummary(all, now), nil
}

// Sites — площадки для переключателя.
func (s *Service) Sites(ctx context.Context) ([]SiteRow, error) { return s.repo.Sites(ctx) }

// ScheduleFor — расписание владельца за период.
func (s *Service) ScheduleFor(ctx context.Context, subj domain.Subject, from, to time.Time) ([]domain.Lesson, error) {
	return s.repo.ScheduleFor(ctx, subj, from, to)
}

// WhereIsLecturer — пара преподавателя в момент at, если идёт.
// Вне пар местонахождение неизвестно, и это честный ответ, а не пустой экран.
func (s *Service) WhereIsLecturer(ctx context.Context, oid int64, at time.Time) (*domain.Lesson, []domain.Lesson, error) {
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, s.clock.Location())
	lessons, err := s.repo.ScheduleFor(ctx, domain.LecturerSubject(oid), day, day)
	if err != nil {
		return nil, nil, err
	}
	now := at.In(s.clock.Location()).Format("15:04")
	for i := range lessons {
		if lessons[i].Covers(now) {
			return &lessons[i], lessons, nil
		}
	}
	return nil, lessons, nil
}

// LecturerName — имя по oid; из пар, а если их нет — из справочника.
func (s *Service) LecturerName(ctx context.Context, oid int64, lessons []domain.Lesson) string {
	for _, l := range lessons {
		if l.LecturerName != "" {
			return l.LecturerName
		}
	}
	if name, err := s.repo.LecturerName(ctx, oid); err == nil && name != "" {
		return name
	}
	return "Преподаватель"
}

// Freshness — когда данные последний раз сверялись с вузом.
func (s *Service) Freshness(ctx context.Context) (time.Time, bool) {
	at, ok, err := s.repo.LastSuccessfulRun(ctx)
	if err != nil {
		return time.Time{}, false
	}
	return at, ok
}

// Repo даёт транспорту доступ к поиску справочников; логики там нет.
func (s *Service) Repo() Repository { return s.repo }
