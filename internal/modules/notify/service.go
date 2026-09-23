package notify

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// Service — планирование и доставка.
type Service struct {
	repo       Repository
	changes    ChangeReader
	transports map[string]Transport
	loc        *time.Location
	log        *slog.Logger

	Batch       int
	MaxAttempts int

	// Reminders — источник напоминаний о заданиях; nil — напоминаний нет.
	Reminders ReminderSource
	// RemindAt — с какого часа вечера (ЧЧ:ММ, пояс вуза) ставить
	// напоминания на завтра. Вечер, а не утро: к утру сделать уже поздно.
	RemindAt string
	// remindedFor — день, за который напоминания уже поставлены этим
	// процессом: чтобы не спрашивать базу каждые полминуты.
	remindedFor string
}

// New создаёт сервис. Транспорты регистрируются по имени.
func New(repo Repository, changes ChangeReader, loc *time.Location, log *slog.Logger, transports ...Transport) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{repo: repo, changes: changes, loc: loc, log: log, transports: map[string]Transport{}, Batch: 50, MaxAttempts: 5, RemindAt: "19:00"}
	for _, t := range transports {
		s.transports[t.Name()] = t
	}
	return s
}

// Enabled сообщает, есть ли хоть один транспорт.
func (s *Service) Enabled() bool { return len(s.transports) > 0 }

// HasTransport — зарегистрирован ли транспорт с таким именем.
func (s *Service) HasTransport(name string) bool { _, ok := s.transports[name]; return ok }

// Subscribe сохраняет подписку. Транспорт должен быть известен: подписка на
// несуществующий транспорт легла бы в базу мёртвым грузом.
func (s *Service) Subscribe(ctx context.Context, sub domain.Subscription) error {
	if _, err := sched.ParseSubjectKey(sub.SubjectKey); err != nil {
		return err
	}
	if !s.HasTransport(sub.Transport) {
		return fmt.Errorf("транспорт %q не настроен", sub.Transport)
	}
	return s.repo.Save(ctx, sub)
}

// Unsubscribe снимает подписку; пустой subjectKey — все подписки адресата.
func (s *Service) Unsubscribe(ctx context.Context, transport, target, subjectKey string) error {
	if subjectKey != "" {
		if _, err := sched.ParseSubjectKey(subjectKey); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, transport, target, subjectKey)
}

// PlanSince ставит в очередь письма об изменениях после since.
//
// Отдельной операцией после прохода сборщика, а не внутри его транзакции:
// та держит одиннадцать тысяч пар, и держать её дольше незачем.
func (s *Service) PlanSince(ctx context.Context, since time.Time) (int, error) {
	const maxChanges = 5000
	changes, err := s.changes.ChangesSince(ctx, since, maxChanges)
	if err != nil || len(changes) == 0 {
		return 0, err
	}
	letters := domain.Build(changes, s.loc)
	keys := make([]string, 0, len(letters))
	for k := range letters {
		keys = append(keys, k)
	}
	// Все адресаты одним запросом: подписчиков единицы, адресатов сотни.
	subs, err := s.repo.For(ctx, keys)
	if err != nil || len(subs) == 0 {
		return 0, err
	}
	byKey := map[string][]int64{}
	for _, sub := range subs {
		byKey[sub.SubjectKey] = append(byKey[sub.SubjectKey], sub.ID)
	}
	queued := 0
	for key, ids := range byKey {
		if err := s.repo.Enqueue(ctx, ids, letters[key]); err != nil {
			s.log.Warn("письмо не поставлено в очередь", "адресат", key, "ошибка", err)
			continue
		}
		queued += len(ids)
	}
	if queued > 0 {
		s.log.Info("уведомления в очереди", "писем", queued, "адресатов", len(byKey), "изменений", len(changes))
	}
	return queued, nil
}

// PlanReminders ставит в очередь напоминания о заданиях к парам завтрашнего
// дня — один раз за день и не раньше RemindAt. Одно письмо на устройство,
// сколько бы у него ни было подписок на разные расписания: иначе человек,
// следящий за группой и за преподавателем, получил бы одно и то же дважды.
func (s *Service) PlanReminders(ctx context.Context, now time.Time) (int, error) {
	if s.Reminders == nil {
		return 0, nil
	}
	now = now.In(s.loc)
	if now.Format("15:04") < s.RemindAt {
		return 0, nil
	}
	y, m, d := now.Date()
	tomorrow := time.Date(y, m, d, 0, 0, 0, 0, s.loc).AddDate(0, 0, 1)
	key := tomorrow.Format("2006-01-02")
	if s.remindedFor == key {
		return 0, nil
	}
	claimed, err := s.repo.ClaimReminderDay(ctx, tomorrow)
	if err != nil {
		return 0, err
	}
	s.remindedFor = key
	if !claimed {
		return 0, nil
	}
	reminders, err := s.Reminders.Reminders(ctx, tomorrow)
	if err != nil || len(reminders) == 0 {
		return 0, err
	}
	owners := make([]string, 0, len(reminders))
	for _, r := range reminders {
		owners = append(owners, r.OwnerKey)
	}
	subs, err := s.repo.ForOwners(ctx, owners)
	if err != nil {
		return 0, err
	}
	byOwner := map[string][]int64{}
	seen := map[string]bool{}
	for _, sub := range subs {
		dst := sub.OwnerKey + "\x1f" + sub.Transport + "\x1f" + sub.Target
		if seen[dst] {
			continue
		}
		seen[dst] = true
		byOwner[sub.OwnerKey] = append(byOwner[sub.OwnerKey], sub.ID)
	}
	queued := 0
	for _, r := range reminders {
		ids := byOwner[r.OwnerKey]
		if len(ids) == 0 {
			continue
		}
		if err := s.repo.Enqueue(ctx, ids, r.Notification); err != nil {
			s.log.Warn("напоминание не поставлено в очередь", "ошибка", err)
			continue
		}
		queued += len(ids)
	}
	if queued > 0 {
		s.log.Info("напоминания о заданиях в очереди", "писем", queued, "день", key)
	}
	return queued, nil
}

// Result — итог разбора очереди.
type Result struct{ Delivered, Dropped, Failed, Retry int }

// Deliver отправляет одну пачку.
func (s *Service) Deliver(ctx context.Context) (Result, error) {
	var res Result
	items, err := s.repo.Take(ctx, s.Batch)
	if err != nil {
		return res, err
	}
	for _, d := range items {
		tr, ok := s.transports[d.Subscription.Transport]
		if !ok {
			_ = s.repo.MarkFailed(ctx, d.ID, "транспорт "+d.Subscription.Transport+" не настроен")
			res.Failed++
			continue
		}
		outcome, err := tr.Send(ctx, d)
		switch {
		case err == nil && outcome == domain.Delivered:
			_ = s.repo.MarkDelivered(ctx, d.ID)
			res.Delivered++
		case outcome == domain.Dead:
			// Адресат исчез навсегда: каждая следующая попытка кончится тем же.
			_ = s.repo.DropSubscription(ctx, d.Subscription.ID)
			res.Dropped++
		case outcome == domain.Failed || d.Attempts >= s.MaxAttempts:
			reason := "отказ транспорта"
			if err != nil {
				reason = err.Error()
			}
			_ = s.repo.MarkFailed(ctx, d.ID, reason)
			res.Failed++
		default:
			_ = s.repo.NoteFailure(ctx, d.Subscription.ID)
			res.Retry++
		}
	}
	return res, nil
}

// Run разбирает очередь по кругу и раз в шесть часов чистит старое.
func (s *Service) Run(ctx context.Context, every time.Duration) {
	if !s.Enabled() {
		return
	}
	tick := time.NewTicker(every)
	clean := time.NewTicker(6 * time.Hour)
	defer tick.Stop()
	defer clean.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-clean.C:
			if n, err := s.repo.CleanupOutbox(ctx, 7*24*time.Hour); err == nil && n > 0 {
				s.log.Info("очередь подчищена", "удалено", n)
			}
		case <-tick.C:
			if _, err := s.PlanReminders(ctx, time.Now()); err != nil {
				s.log.Warn("напоминания не поставлены", "ошибка", err)
			}
			res, err := s.Deliver(ctx)
			if err != nil {
				s.log.Warn("очередь не разобрана", "ошибка", err)
			} else if res.Delivered+res.Dropped+res.Failed+res.Retry > 0 {
				s.log.Info("уведомления отправлены", "доставлено", res.Delivered,
					"отписалось", res.Dropped, "неудачно", res.Failed, "повторим", res.Retry)
			}
		}
	}
}
