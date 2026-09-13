package push

import (
	"context"
	"log/slog"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

// Planner превращает найденные изменения в письма для подписчиков.
type Planner struct {
	store *store.Store
	loc   *time.Location
	log   *slog.Logger
}

// NewPlanner создаёт планировщик.
func NewPlanner(s *store.Store, loc *time.Location, log *slog.Logger) *Planner {
	if log == nil {
		log = slog.Default()
	}
	if loc == nil {
		loc = time.UTC
	}
	return &Planner{store: s, loc: loc, log: log}
}

// PlanSince ставит в очередь уведомления об изменениях, найденных после
// указанного момента.
//
// Вызывается сразу после прохода сборщика, но в отдельной операции:
// складывать письма внутри той же транзакции, что и одиннадцать тысяч пар,
// значило бы держать её дольше без всякой нужды.
func (p *Planner) PlanSince(ctx context.Context, since time.Time) (int, error) {
	// Потолок выборки: если за проход изменилось всё расписание вуза,
	// уведомления всё равно будут сгруппированы по адресатам, а тянуть
	// в память весь журнал незачем.
	const maxChanges = 5000

	changes, err := p.store.ChangesSince(ctx, since, maxChanges)
	if err != nil {
		return 0, err
	}
	if len(changes) == 0 {
		return 0, nil
	}

	notifications := Build(changes, p.loc)
	if len(notifications) == 0 {
		return 0, nil
	}

	keys := make([]string, 0, len(notifications))
	for key := range notifications {
		keys = append(keys, key)
	}

	// Спрашиваем разом обо всех адресатах: подписчиков обычно единицы, а
	// адресатов — сотни, и запрос на каждого был бы пустой тратой.
	subs, err := p.store.SubscriptionsFor(ctx, keys)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	bySubject := map[string][]int64{}
	for _, s := range subs {
		bySubject[s.SubjectKey] = append(bySubject[s.SubjectKey], s.ID)
	}

	var queued int
	for key, ids := range bySubject {
		n, ok := notifications[key]
		if !ok {
			continue
		}
		if err := p.store.Enqueue(ctx, ids, n); err != nil {
			p.log.Warn("уведомление не поставлено в очередь", "адресат", key, "ошибка", err)
			continue
		}
		queued += len(ids)
	}
	if queued > 0 {
		p.log.Info("уведомления поставлены в очередь",
			"писем", queued, "адресатов", len(bySubject), "изменений", len(changes))
	}
	return queued, nil
}
