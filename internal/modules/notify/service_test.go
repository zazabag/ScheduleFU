package notify

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
)

// remindRepo — подписки в памяти и отметка дней. Остальное хранилище
// тесту не нужно: вызов лишнего упадёт на встроенном nil-интерфейсе.
type remindRepo struct {
	Repository
	subs    []domain.Subscription
	claimed map[string]bool
	queued  map[int64]domain.Notification
}

func (r *remindRepo) ForOwners(_ context.Context, owners []string) ([]domain.Subscription, error) {
	want := map[string]bool{}
	for _, o := range owners {
		want[o] = true
	}
	var out []domain.Subscription
	for _, s := range r.subs {
		if want[s.OwnerKey] {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *remindRepo) ClaimReminderDay(_ context.Context, day time.Time) (bool, error) {
	k := day.Format("2006-01-02")
	if r.claimed[k] {
		return false, nil
	}
	r.claimed[k] = true
	return true, nil
}

func (r *remindRepo) Enqueue(_ context.Context, ids []int64, n domain.Notification) error {
	for _, id := range ids {
		r.queued[id] = n
	}
	return nil
}

type fixedReminders struct {
	asked []time.Time
	out   []Reminder
}

func (f *fixedReminders) Reminders(_ context.Context, day time.Time) ([]Reminder, error) {
	f.asked = append(f.asked, day)
	return f.out, nil
}

func TestNapominanieRazVDenVecheromOdnoNaUstroystvo(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	repo := &remindRepo{claimed: map[string]bool{}, queued: map[int64]domain.Notification{}, subs: []domain.Subscription{
		// одно устройство подписано на группу и на преподавателя: одно письмо
		{ID: 1, OwnerKey: "устройство", Transport: "webpush", Target: "https://push/1", SubjectKey: "group:ПИ24-1"},
		{ID: 2, OwnerKey: "устройство", Transport: "webpush", Target: "https://push/1", SubjectKey: "lecturer:5"},
		{ID: 3, OwnerKey: "другое", Transport: "webpush", Target: "https://push/2", SubjectKey: "group:ПИ24-2"},
	}}
	src := &fixedReminders{out: []Reminder{{OwnerKey: "устройство", Notification: domain.Notification{Title: "Завтра"}}}}
	s := New(repo, nil, msk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Reminders = src
	ctx := context.Background()

	if n, _ := s.PlanReminders(ctx, time.Date(2026, 9, 23, 18, 30, 0, 0, msk)); n != 0 || len(src.asked) != 0 {
		t.Fatal("до 19:00 напоминаний не ставим")
	}
	n, err := s.PlanReminders(ctx, time.Date(2026, 9, 23, 19, 5, 0, 0, msk))
	if err != nil || n != 1 {
		t.Fatalf("в 19:05 — одно письмо на устройство, а поставлено %d (%v)", n, err)
	}
	if got := src.asked[0].Format("2006-01-02"); got != "2026-09-24" {
		t.Errorf("напоминаем о завтрашнем дне, а спросили %s", got)
	}
	if _, ok := repo.queued[1]; !ok || len(repo.queued) != 1 {
		t.Errorf("очередь: %v", repo.queued)
	}
	// Перезапуск процесса вечером: память пуста, но день отмечен в базе.
	s2 := New(repo, nil, msk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s2.Reminders = src
	if n, _ := s2.PlanReminders(ctx, time.Date(2026, 9, 23, 21, 0, 0, 0, msk)); n != 0 {
		t.Error("второе напоминание за тот же день")
	}
}
