package push

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	// Своя база на пакет: go test прогоняет пакеты параллельно, и на общей
	// базе тесты этого пакета вычищали таблицы под ногами у тестов store —
	// падало то одно, то другое, причём только в полном прогоне.
	dsn := os.Getenv("TEST_DATABASE_URL_PUSH")
	if dsn == "" {
		dsn = "postgres://localhost:5432/schedulefu_test_push?sslmode=disable"
	}
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("тестовая база недоступна (%v)", err)
	}
	_, err = s.Pool().Exec(context.Background(),
		`TRUNCATE lessons, lesson_changes, push_subscriptions, push_outbox,
		          auditoriums, groups, lecturers, collector_runs RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("очистка базы: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func oid(v int64) *int64 { return &v }

func lesson(n int64, groups []string, lecturer int64) store.Lesson {
	l := store.Lesson{
		LessonOid:  n,
		Date:       time.Date(2026, 9, 14, 0, 0, 0, 0, msk),
		BeginsAt:   "08:30",
		EndsAt:     "10:00",
		Discipline: "Философия",
		GroupNames: groups,
	}
	if lecturer > 0 {
		l.LecturerOid = oid(lecturer)
		l.LecturerName = "Иванов И.И."
	}
	return l
}

// TestPlanSinceStavitPismaPodpischikam проверяет весь путь: изменение в
// расписании — письмо в очереди у того, кто на него подписан.
func TestPlanSinceStavitPismaPodpischikam(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, msk)

	if err := s.SaveSubscription(ctx, store.PushSubscription{
		SubjectKey: "group:ПИ24-1",
		Endpoint:   "https://push.example/device-1",
		P256dh:     "key", Auth: "auth",
	}); err != nil {
		t.Fatal(err)
	}

	// Сначала база наполняется: появление расписания у нас — не правка вуза.
	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(1, []string{"ПИ24-1"}, 0),
	}); err != nil {
		t.Fatal(err)
	}

	since := time.Now()
	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(1, []string{"ПИ24-1"}, 0),
		lesson(2, []string{"ПИ24-1"}, 0),
		lesson(3, []string{"ПИ24-1"}, 0),
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := NewPlanner(s, msk, nil).PlanSince(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	// Два изменения, но подписчик один — и письмо должно быть одно.
	if queued != 1 {
		t.Fatalf("поставлено %d писем, ожидалось одно", queued)
	}

	items, err := s.TakeOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("в очереди %d писем", len(items))
	}
	var n Notification
	if err := json.Unmarshal(items[0].Payload, &n); err != nil {
		t.Fatalf("письмо не разобрано: %v", err)
	}
	if n.Title == "" || n.Body == "" {
		t.Errorf("пустое уведомление: %+v", n)
	}
	t.Logf("текст уведомления: %q", n.Body)
}

// TestPlanSinceNePishetChuzhim: подписка на одну группу не должна ловить
// изменения другой.
func TestPlanSinceNePishetChuzhim(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, msk)

	if err := s.SaveSubscription(ctx, store.PushSubscription{
		SubjectKey: "group:ПИ24-1", Endpoint: "https://push.example/d", P256dh: "k", Auth: "a",
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(9, []string{"Ю24-5"}, 0),
	}); err != nil {
		t.Fatal(err)
	}

	since := time.Now()
	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(9, []string{"Ю24-5"}, 0),
		lesson(10, []string{"Ю24-5"}, 0),
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := NewPlanner(s, msk, nil).PlanSince(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Fatalf("чужое изменение дало %d писем", queued)
	}
}

// TestPlanSinceDostayotPrepodavatelya: преподаватель подписан на себя и
// получает уведомление о своей паре наравне с группой.
func TestPlanSinceDostayotPrepodavatelya(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, msk)

	for _, sub := range []store.PushSubscription{
		{SubjectKey: "group:ПИ24-1", Endpoint: "https://push.example/student", P256dh: "k", Auth: "a"},
		{SubjectKey: "lecturer:46674", Endpoint: "https://push.example/teacher", P256dh: "k", Auth: "a"},
	} {
		if err := s.SaveSubscription(ctx, sub); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(19, []string{"ПИ24-1"}, 46674),
	}); err != nil {
		t.Fatal(err)
	}

	since := time.Now()
	if _, err := s.ApplySnapshot(ctx, day, day, []store.Lesson{
		lesson(19, []string{"ПИ24-1"}, 46674),
		lesson(20, []string{"ПИ24-1"}, 46674),
	}); err != nil {
		t.Fatal(err)
	}

	queued, err := NewPlanner(s, msk, nil).PlanSince(ctx, since)
	if err != nil {
		t.Fatal(err)
	}
	if queued != 2 {
		t.Fatalf("поставлено %d писем, ожидалось два (студенту и преподавателю)", queued)
	}
}

// TestOchered'NeOtdayotOdnoPismoDvazhdy: выборка блокирует взятые строки,
// иначе два отправщика доставили бы одно уведомление дважды.
func TestOcheredNeOtdayotOdnoPismoDvazhdy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.SaveSubscription(ctx, store.PushSubscription{
		SubjectKey: "group:X", Endpoint: "https://push.example/d", P256dh: "k", Auth: "a",
	}); err != nil {
		t.Fatal(err)
	}
	subs, err := s.SubscriptionsFor(ctx, []string{"group:X"})
	if err != nil || len(subs) != 1 {
		t.Fatalf("подписка не найдена: %v", err)
	}
	if err := s.Enqueue(ctx, []int64{subs[0].ID}, Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}

	first, err := s.TakeOutbox(ctx, 10)
	if err != nil || len(first) != 1 {
		t.Fatalf("первая выборка: %d писем (%v)", len(first), err)
	}
	// Повторная выборка сразу же не должна вернуть то же письмо: попытка
	// уже отодвинута на будущее.
	second, err := s.TakeOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("то же письмо выдано повторно: %d", len(second))
	}
}
