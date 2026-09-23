package notify

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

type morningRepo struct {
	Repository
	subs    []domain.Subscription
	claimed map[string]bool
	queued  map[int64]domain.Notification
}

func (r *morningRepo) MorningSubscriptions(context.Context) ([]domain.Subscription, error) {
	return r.subs, nil
}
func (r *morningRepo) ClaimMorningDay(_ context.Context, day time.Time) (bool, error) {
	k := day.Format("2006-01-02")
	if r.claimed[k] {
		return false, nil
	}
	r.claimed[k] = true
	return true, nil
}
func (r *morningRepo) Enqueue(_ context.Context, ids []int64, n domain.Notification) error {
	for _, id := range ids {
		r.queued[id] = n
	}
	return nil
}

type dayFake struct{ asked map[string]int }

func (d *dayFake) LessonsOn(_ context.Context, key string, day time.Time) ([]sched.Lesson, error) {
	d.asked[key]++
	if key != "group:ПИ24-1" {
		return nil, nil // у второй группы сегодня пар нет
	}
	return []sched.Lesson{{Date: day, BeginsAt: "10:10", EndsAt: "11:40", Discipline: "Финансы", Auditorium: "ЛП51_1/0412"}}, nil
}

func TestUtrennyayaSvodkaRazVDenPosle7(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	repo := &morningRepo{claimed: map[string]bool{}, queued: map[int64]domain.Notification{}, subs: []domain.Subscription{
		{ID: 1, SubjectKey: "group:ПИ24-1"}, {ID: 2, SubjectKey: "group:ПИ24-1"}, {ID: 3, SubjectKey: "group:ПИ24-2"},
	}}
	days := &dayFake{asked: map[string]int{}}
	s := New(repo, nil, msk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Days = days
	ctx := context.Background()
	if n, _ := s.PlanMorning(ctx, time.Date(2026, 9, 24, 6, 50, 0, 0, msk)); n != 0 {
		t.Fatal("до 7:00 сводок нет")
	}
	n, err := s.PlanMorning(ctx, time.Date(2026, 9, 24, 7, 1, 0, 0, msk))
	if err != nil || n != 2 {
		t.Fatalf("двум подписчикам группы с парами — по сводке, а поставлено %d (%v)", n, err)
	}
	if days.asked["group:ПИ24-1"] != 1 {
		t.Errorf("расписание группы читается один раз на всех её подписчиков: %d", days.asked["group:ПИ24-1"])
	}
	if _, ok := repo.queued[3]; ok {
		t.Error("у второй группы пар нет — сводки нет")
	}
	if got := repo.queued[1]; got.Title != "Сегодня 1 пара" || got.URL != "/schedule?group=%D0%9F%D0%9824-1" {
		t.Errorf("сводка: %+v", got)
	}
	if n, _ := s.PlanMorning(ctx, time.Date(2026, 9, 24, 9, 0, 0, 0, msk)); n != 0 {
		t.Error("вторая сводка за тот же день")
	}
}
