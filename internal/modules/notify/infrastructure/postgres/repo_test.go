package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notify"
	"github.com/zazabag/schedulefu/internal/modules/notify/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	schedpg "github.com/zazabag/schedulefu/internal/modules/schedule/infrastructure/postgres"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

var msk = time.FixedZone("MSK", 3*3600)

func setup(t *testing.T) (*notify.Service, *Repo, *schedpg.Repo, context.Context) {
	t.Helper()
	pool := db.TestPool(t, "TEST_DATABASE_URL_NOTIFY", "schedulefu_test_notify",
		"lessons", "lesson_changes", "auditoriums", "groups", "lecturers", "collector_runs", "subscriptions", "outbox", "reminder_days", "morning_days")
	repo := New(pool)
	sr := schedpg.New(pool)
	svc := notify.New(repo, sr, msk, nil, fakeTransport{})
	return svc, repo, sr, context.Background()
}

type fakeTransport struct{}

func (fakeTransport) Name() string { return "webpush" }
func (fakeTransport) Send(context.Context, domain.Delivery) (domain.Outcome, error) {
	return domain.Delivered, nil
}

func day() time.Time     { return time.Date(2026, 9, 14, 0, 0, 0, 0, msk) }
func oid(v int64) *int64 { return &v }

func lesson(n int64, groups []string, lecturer int64) sched.Lesson {
	l := sched.Lesson{LessonOid: n, Date: day(), BeginsAt: "08:30", EndsAt: "10:00", Discipline: "Философия", GroupNames: groups}
	if lecturer > 0 {
		l.LecturerOid, l.LecturerName = oid(lecturer), "Иванов И.И."
	}
	return l
}

func sub(key, target string) domain.Subscription {
	return domain.Subscription{SubjectKey: key, Transport: "webpush", Target: target, Credentials: map[string]string{"p256dh": "k", "auth": "a"}}
}

// Весь путь: база наполнена, приходит правка — письмо в очереди у того,
// кто на неё подписан. Два изменения, один подписчик — одно письмо.
func TestPlanSinceStavitPismaPodpischikam(t *testing.T) {
	svc, repo, sr, ctx := setup(t)
	if err := svc.Subscribe(ctx, sub("group:ПИ24-1", "https://push.example/d1")); err != nil {
		t.Fatal(err)
	}
	if _, err := sr.ApplySnapshot(ctx, day(), day(), []sched.Lesson{lesson(1, []string{"ПИ24-1"}, 0)}); err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	if _, err := sr.ApplySnapshot(ctx, day(), day(), []sched.Lesson{lesson(1, []string{"ПИ24-1"}, 0), lesson(2, []string{"ПИ24-1"}, 0), lesson(3, []string{"ПИ24-1"}, 0)}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.PlanSince(ctx, since); err != nil || n != 1 {
		t.Fatalf("писем %d (%v), ожидалось одно", n, err)
	}
	items, err := repo.Take(ctx, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("в очереди %d (%v)", len(items), err)
	}
	var n domain.Notification
	if err := json.Unmarshal(items[0].Payload, &n); err != nil || n.Body == "" {
		t.Fatalf("письмо: %+v %v", n, err)
	}
	t.Logf("текст: %q", n.Body)
	// Повторный Take не должен выдать то же письмо: попытка отодвинута.
	if again, _ := repo.Take(ctx, 10); len(again) != 0 {
		t.Fatalf("письмо выдано дважды")
	}
}

func TestPlanSinceNePishetChuzhimIDostayotPrepodavatelya(t *testing.T) {
	svc, _, sr, ctx := setup(t)
	for _, s := range []domain.Subscription{sub("group:ПИ24-1", "https://push.example/student"), sub("lecturer:46674", "https://push.example/teacher"), sub("group:Ю24-5", "https://push.example/other")} {
		if err := svc.Subscribe(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sr.ApplySnapshot(ctx, day(), day(), []sched.Lesson{lesson(19, []string{"ПИ24-1"}, 46674)}); err != nil {
		t.Fatal(err)
	}
	since := time.Now()
	if _, err := sr.ApplySnapshot(ctx, day(), day(), []sched.Lesson{lesson(19, []string{"ПИ24-1"}, 46674), lesson(20, []string{"ПИ24-1"}, 46674)}); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.PlanSince(ctx, since); n != 2 {
		t.Fatalf("писем %d, ожидалось два: студенту и преподавателю, но не Ю24-5", n)
	}
}

// Ключ преподавателя — только число; транспорт — только настроенный.
func TestSubscribeOtvergaetMusor(t *testing.T) {
	svc, _, _, ctx := setup(t)
	if err := svc.Subscribe(ctx, sub("lecturer:d6607672-guid", "https://x")); err == nil {
		t.Error("GUID принят")
	}
	bad := sub("group:ПИ24-1", "https://x")
	bad.Transport = "telegram"
	if err := svc.Subscribe(ctx, bad); err == nil {
		t.Error("ненастроенный транспорт принят")
	}
}

func TestDeliverIMyortvayaPodpiska(t *testing.T) {
	_, repo, _, ctx := setup(t)
	dead := notify.New(repo, nil, msk, nil, deadTransport{})
	if err := dead.Subscribe(ctx, sub("group:X", "https://push.example/gone")); err != nil {
		t.Fatal(err)
	}
	subs, _ := repo.For(ctx, []string{"group:X"})
	if err := repo.Enqueue(ctx, []int64{subs[0].ID}, domain.Notification{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	res, err := dead.Deliver(ctx)
	if err != nil || res.Dropped != 1 {
		t.Fatalf("мёртвая подписка: %+v %v", res, err)
	}
	if left, _ := repo.For(ctx, []string{"group:X"}); len(left) != 0 {
		t.Fatal("мёртвая подписка осталась")
	}
}

type deadTransport struct{}

func (deadTransport) Name() string { return "webpush" }
func (deadTransport) Send(context.Context, domain.Delivery) (domain.Outcome, error) {
	return domain.Dead, nil
}

// Подписка помнит ключ устройства, и подтверждение из вкладки без cookie
// его не стирает: иначе напоминания пропадали бы после любого визита.
func TestPodpiskaPomnitKlyuchUstroystva(t *testing.T) {
	_, repo, _, ctx := setup(t)
	sub := domain.Subscription{SubjectKey: "group:ПИ24-1", Transport: "webpush", Target: "https://push.example/1",
		Credentials: map[string]string{"p256dh": "k", "auth": "a"}, OwnerKey: "устройство"}
	if err := repo.Save(ctx, sub); err != nil {
		t.Fatal(err)
	}
	sub.OwnerKey = ""
	if err := repo.Save(ctx, sub); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ForOwners(ctx, []string{"устройство"})
	if err != nil || len(got) != 1 || got[0].OwnerKey != "устройство" || got[0].Credentials["auth"] != "a" {
		t.Fatalf("подписки владельца: %+v %v", got, err)
	}
}

func TestDenNapominaniyOtmechaetsyaOdinRaz(t *testing.T) {
	_, repo, _, ctx := setup(t)
	d := time.Date(2026, 9, 24, 0, 0, 0, 0, msk)
	first, err := repo.ClaimReminderDay(ctx, d)
	if err != nil || !first {
		t.Fatalf("первая отметка: %v %v", first, err)
	}
	if again, _ := repo.ClaimReminderDay(ctx, d); again {
		t.Error("день отмечен дважды — напоминание ушло бы дважды")
	}
}

// Утренняя сводка — по желанию и у конкретной подписки; день отмечается
// один раз.
func TestUtrennyayaSvodkaVBaze(t *testing.T) {
	_, repo, _, ctx := setup(t)
	sub := domain.Subscription{SubjectKey: "group:ПИ24-1", Transport: "webpush", Target: "https://push.example/1",
		Credentials: map[string]string{"p256dh": "k", "auth": "a"}}
	if err := repo.Save(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if on, _ := repo.Morning(ctx, "webpush", sub.Target, sub.SubjectKey); on {
		t.Error("по умолчанию сводка выключена")
	}
	if found, err := repo.SetMorning(ctx, "webpush", "https://push.example/нет", sub.SubjectKey, true); found || err != nil {
		t.Errorf("чужая подписка: %v %v", found, err)
	}
	if found, _ := repo.SetMorning(ctx, "webpush", sub.Target, sub.SubjectKey, true); !found {
		t.Fatal("своя подписка не нашлась")
	}
	subs, err := repo.MorningSubscriptions(ctx)
	if err != nil || len(subs) != 1 || subs[0].Credentials["auth"] != "a" {
		t.Fatalf("подписки на сводку: %+v %v", subs, err)
	}
	d := time.Date(2026, 9, 24, 0, 0, 0, 0, msk)
	if ok, _ := repo.ClaimMorningDay(ctx, d); !ok {
		t.Fatal("первая отметка дня")
	}
	if again, _ := repo.ClaimMorningDay(ctx, d); again {
		t.Error("день отмечен дважды")
	}
}
