package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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
	// GroupLookups — потолок поисковых запросов за проход при пополнении
	// справочника групп. Обычно новых групп единицы; потолок нужен на
	// случай первого прохода по неполному справочнику: не больше запросов,
	// чем в одном обычном обходе аудиторий (~630), чтобы нагрузка на вуз
	// не выходила за привычную.
	GroupLookups int
}

// Service — сервис модуля.
type Service struct {
	src      source.Source
	repo     Repository
	clock    *clock.Clock
	log      *slog.Logger
	opts     Options
	Notifier Notifier

	// missed — группы, которых поиск вуза не нашёл, и когда. Живёт в
	// памяти процесса сборщика: он крутится сутками, а после перезапуска
	// один лишний запрос на группу не страшен.
	missMu sync.Mutex
	missed map[string]time.Time

	// pausedUntil — до какого момента не ходить к вузу вовсе: он ответил
	// 429 или 403. Общая для сборщика и ленивой привязки групп.
	pauseMu     sync.Mutex
	pausedUntil time.Time
}

// ErrPaused — вуз недавно попросил сбавить темп, и пауза ещё идёт.
var ErrPaused = errors.New("источник на паузе после отказа")

// PausedUntil — до какого момента к вузу не ходим; ok — пауза идёт.
func (s *Service) PausedUntil() (time.Time, bool) {
	s.pauseMu.Lock()
	defer s.pauseMu.Unlock()
	return s.pausedUntil, s.clock.Now().Before(s.pausedUntil)
}

// checkPause возвращает ErrPaused, пока пауза не кончилась.
func (s *Service) checkPause() error {
	if until, ok := s.PausedUntil(); ok {
		return fmt.Errorf("%w до %s", ErrPaused, until.In(s.clock.Location()).Format("15:04"))
	}
	return nil
}

// noteThrottle ставит паузу, если ошибка — отказ источника, и сообщает об
// этом. Любая другая ошибка паузы не вызывает.
func (s *Service) noteThrottle(err error) bool {
	var th *source.ThrottledError
	if !errors.As(err, &th) {
		return false
	}
	until := s.clock.Now().Add(pauseFor(th.RetryAfter))
	s.pauseMu.Lock()
	if until.After(s.pausedUntil) {
		s.pausedUntil = until
	}
	s.pauseMu.Unlock()
	s.log.Warn("вуз попросил сбавить темп, пауза", "код", th.Status, "до", until.Format("15:04"))
	return true
}

// pauseFor — сколько молчать после отказа. Не меньше получаса, даже если
// вуз не сказал сколько: отказ — сигнал, что мы уже на краю. Не больше
// шести часов: дольше расписание протухает сильнее, чем стоит риск.
func pauseFor(retryAfter time.Duration) time.Duration {
	const minPause, maxPause = 30 * time.Minute, 6 * time.Hour
	switch {
	case retryAfter < minPause:
		return minPause
	case retryAfter > maxPause:
		return maxPause
	}
	return retryAfter
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
	if opts.GroupLookups <= 0 {
		opts.GroupLookups = 600
	}
	return &Service{src: src, repo: repo, clock: clk, log: log, opts: opts, missed: map[string]time.Time{}}
}

// ─── сбор ────────────────────────────────────────────────────────────────────

// Collect делает один проход: обходит аудитории, применяет слепок, пополняет
// справочники, чистит журнал и планирует уведомления.
//
// Обход по аудиториям, а не по группам: в каждой паре уже есть и группа, и
// преподаватель, поэтому один проход даёт полный слепок вуза за период.
// Период короткий — день или неделя: выкачивать семестр нельзя по праву.
func (s *Service) Collect(ctx context.Context, from, to time.Time) (domain.ApplyResult, error) {
	if err := s.checkPause(); err != nil {
		return domain.ApplyResult{}, err
	}
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

	// Вуз отказал посреди обхода — проход брошен целиком. Недоопрошенные
	// аудитории нельзя применять: их пары выглядели бы отменёнными.
	if stats.throttled != nil {
		_ = s.repo.FinishRun(ctx, runID, total, stats.failed, 0, 0, stats.throttled)
		return domain.ApplyResult{}, fmt.Errorf("проход прерван: %w", stats.throttled)
	}

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
	s.discoverGroups(ctx, seen.groupNames())
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

type fetchStats struct {
	ok, failed int
	throttled  error // первый отказ источника; проход после него брошен
}

// seenRefs копит справочные записи, встреченные в слепке.
type seenRefs struct {
	auds   map[int64]domain.Auditorium
	lecs   map[int64]string
	groups map[string]bool
}

func (s seenRefs) groupNames() []string {
	out := make([]string, 0, len(s.groups))
	for g := range s.groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
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
		refs    = seenRefs{auds: map[int64]domain.Auditorium{}, lecs: map[int64]string{}, groups: map[string]bool{}}
		dedup   = map[int64]bool{} // поточная пара приходит из каждой её аудитории
		jobs    = make(chan int64)
		wg      sync.WaitGroup
	)
	loc := s.clock.Location()
	// Свой контекст: первый же отказ вуза гасит всех работников сразу, а
	// не только того, кто его получил.
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	for i := 0; i < s.opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for oid := range jobs {
				if ctx.Err() != nil {
					continue
				}
				raw, err := s.src.Schedule(ctx, source.KindAuditorium, oid, from, to)
				mu.Lock()
				if err != nil {
					if stats.throttled == nil && s.noteThrottle(err) {
						stats.throttled = err
						stop()
					}
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
					for _, g := range l.GroupNames {
						if IsGroupName(g) {
							refs.groups[g] = true
						}
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

// discoverGroups дописывает в справочник группы, которые встретились в
// парах, но не попали в посев: набор нового года, переименования.
// Без этого у группы есть расписание, но её не найти поиском — так
// набор 2026 года остался невидимым после посева 12.09.2026.
//
// Числовой id группы пара не несёт, поэтому на каждую новую группу — один
// поиск у вуза. Новых групп за проход обычно ноль, запросов тоже ноль.
// Ошибки здесь не ломают проход: слепок уже применён, а справочник
// догонит на следующем.
func (s *Service) discoverGroups(ctx context.Context, names []string) {
	if len(names) == 0 {
		return
	}
	if s.checkPause() != nil {
		return
	}
	missing, err := s.repo.MissingGroups(ctx, names)
	if err != nil {
		s.log.Warn("новые группы не проверены", "ошибка", err)
		return
	}
	now := s.clock.Now()
	known := map[string]bool{}
	var found []domain.Group
	asked, lost := 0, 0
	for _, name := range missing {
		if known[name] || s.missedRecently(name, now) {
			continue
		}
		if asked >= s.opts.GroupLookups {
			break
		}
		asked++
		res, err := s.src.Search(ctx, source.SearchGroup, name)
		if err != nil {
			if ctx.Err() != nil || s.noteThrottle(err) {
				break
			}
			s.log.Debug("группа не найдена у вуза", "группа", name, "ошибка", err)
			continue
		}
		// Поиск — по подстроке: «УПП26-1» приносит и «УПП26-10». Соседей
		// по выдаче записываем сразу, за ними второй раз ходить не нужно.
		for _, r := range res {
			label := strings.TrimSpace(r.Label)
			id, ok := parseID(r.ID)
			if !ok || !IsGroupName(label) || known[label] {
				continue
			}
			known[label] = true
			found = append(found, domain.Group{ID: id, Name: label,
				FacultyOid: strings.TrimSpace(r.Description), AdmissionYear: AdmissionYear(label)})
		}
		if !known[name] {
			lost++
			s.markMissed(name, now)
		}
	}
	if len(found) > 0 {
		if err := s.repo.UpsertGroups(ctx, found); err != nil {
			s.log.Warn("новые группы не записаны", "ошибка", err)
			return
		}
	}
	if asked > 0 {
		s.log.Info("справочник групп пополнен", "запросов", asked, "добавлено", len(found), "не найдено", lost)
	}
}

// missRetry — через сколько снова спрашивать вуз о группе, которую он не
// нашёл. Сутки: за это время деканат успевает завести группу в поиске.
const missRetry = 24 * time.Hour

func (s *Service) missedRecently(name string, now time.Time) bool {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	at, ok := s.missed[name]
	return ok && now.Sub(at) < missRetry
}

func (s *Service) markMissed(name string, now time.Time) {
	s.missMu.Lock()
	defer s.missMu.Unlock()
	s.missed[name] = now
}

// groupNameRe — настоящая группа: «ПИ24-1», «Ю24-5в». Имя языкового
// потока («006073_2 Иностранный язык (КАЯиПК)-10 …») группой не считается:
// в поиске вуза его нет, и спрашивать о нём бессмысленно.
var groupNameRe = regexp.MustCompile(`^\p{L}+[0-9]{2}-[0-9]+\p{L}*$`)

// IsGroupName сообщает, что имя — учебная группа, а не поток.
func IsGroupName(name string) bool { return groupNameRe.MatchString(name) }

// AdmissionYear — год набора из имени группы: «ПИ24-1» → 2024.
// nil — в имени нет двух цифр подряд.
func AdmissionYear(name string) *int {
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

// EnsureGroupLinks дотягивает расписание группы у вуза — не чаще раза в
// день и только для группы, которую кто-то открыл. Слепок по аудиториям
// не знает, чьи языковые подгруппы (88 % пар английского шли мимо групп);
// расписание самой группы у источника знает. Один запрос на группу в
// сутки — сотые доли процента от обхода аудиторий.
//
// Ошибка источника не ломает экран: без связей расписание показывается
// как раньше, а попытка повторится при следующем открытии.
func (s *Service) EnsureGroupLinks(ctx context.Context, group string, from, to time.Time) error {
	if group == "" {
		return nil
	}
	if err := s.checkPause(); err != nil {
		return err
	}
	today := s.clock.Today()
	if on, ok, err := s.repo.GroupFetchedOn(ctx, group); err != nil {
		return err
	} else if ok && !on.Before(today) {
		return nil
	}
	id, ok, err := s.repo.GroupID(ctx, group)
	if err != nil {
		return err
	}
	if !ok {
		// Справочник групп — из посева; новой группы в нём может не быть.
		found, err := s.src.Search(ctx, source.SearchGroup, group)
		if err != nil {
			s.noteThrottle(err)
			return err
		}
		for _, f := range found {
			if f.Label == group {
				if id, ok = parseID(f.ID); ok {
					break
				}
			}
		}
		if !ok {
			// Отмечаем день и без результата: не спрашивать вуз по кругу.
			return s.repo.ApplyGroupLinks(ctx, group, nil, today)
		}
	}
	lessons, err := s.src.Schedule(ctx, source.KindGroup, id, from, to)
	if err != nil {
		s.noteThrottle(err)
		return err
	}
	oids := make([]int64, 0, len(lessons))
	for _, l := range lessons {
		oids = append(oids, l.LessonOid)
	}
	s.log.Info("дотянуто расписание группы", "группа", group, "пар", len(oids))
	return s.repo.ApplyGroupLinks(ctx, group, oids, today)
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
