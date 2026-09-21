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
}

// New создаёт сервис. Транспорты регистрируются по имени.
func New(repo Repository, changes ChangeReader, loc *time.Location, log *slog.Logger, transports ...Transport) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{repo: repo, changes: changes, loc: loc, log: log, transports: map[string]Transport{}, Batch: 50, MaxAttempts: 5}
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
