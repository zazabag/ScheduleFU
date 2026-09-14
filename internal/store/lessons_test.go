package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// testStore поднимает чистую схему в тестовой базе.
//
// База своя на пакет: go test прогоняет пакеты параллельно, и общая база
// означала бы, что соседний пакет чистит таблицы посреди чужого теста.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://localhost:5432/schedulefu_test?sslmode=disable"
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Skipf("тестовая база недоступна (%v)", err)
	}
	// Чистим данные между тестами, схему оставляем.
	_, err = s.pool.Exec(ctx,
		`TRUNCATE lessons, lesson_changes, auditoriums, groups, lecturers, collector_runs RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("очистка базы: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

func oidPtr(v int64) *int64 { return &v }

func para(oid int64, d int, begins string, audOid int64, discipline string) Lesson {
	return Lesson{
		LessonOid:     oid,
		Date:          day(d),
		BeginsAt:      begins,
		EndsAt:        "10:00",
		AuditoriumOid: oidPtr(audOid),
		Auditorium:    "ЛП49/2/313",
		Building:      "Ленинградский проспект, 49/2",
		Discipline:    discipline,
		KindOfWork:    "Семинар",
		LecturerOid:   oidPtr(46674),
		LecturerName:  "Замараева Е.И.",
		Stream:        "ПИ24-1; ПИ24-2",
		GroupNames:    []string{"ПИ24-1", "ПИ24-2"},
	}
}

func TestApplySnapshotNahoditDobavleniya(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	res, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2851, "История"),
	})
	if err != nil {
		t.Fatalf("применение слепка: %v", err)
	}
	if res.Added != 2 || res.Changed != 0 || res.Removed != 0 {
		t.Fatalf("ожидалось 2 добавления, получено %+v", res)
	}
}

// TestPovtornoePrimenenieNeDayotLozhnyhIzmeneniy — важнейший тест всей
// затеи. Сборщик ходит по кругу; если одинаковый слепок будет каждый раз
// считаться изменением, пользователи получат шквал ложных уведомлений и
// выключат их навсегда.
func TestPovtornoePrimenenieNeDayotLozhnyhIzmeneniy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	snapshot := []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2852, "История"),
	}

	if _, err := s.ApplySnapshot(ctx, day(7), day(13), snapshot); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		res, err := s.ApplySnapshot(ctx, day(7), day(13), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		if res.Changes() != 0 {
			t.Fatalf("проход %d: тот же слепок дал изменения %+v", i+2, res)
		}
	}
}

func TestApplySnapshotNahoditPerenosVDruguyuAuditoriyu(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
	}); err != nil {
		t.Fatal(err)
	}

	moved := para(1, 7, "08:30", 2999, "Философия")
	moved.Auditorium = "ЛП51_1/0312"
	res, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{moved})
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed != 1 || res.Added != 0 || res.Removed != 0 {
		t.Fatalf("ожидался перенос как изменение, получено %+v", res)
	}

	changes, err := s.ChangesSince(ctx, time.Now().Add(-time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, c := range changes {
		if c.Kind == ChangeChanged && c.LessonOid == 1 {
			found = true
			if c.Before == nil || c.After == nil {
				t.Fatal("у изменения должны быть оба состояния")
			}
			if c.Before.Auditorium == c.After.Auditorium {
				t.Fatal("аудитория до и после совпадает — перенос не зафиксирован")
			}
		}
	}
	if !found {
		t.Fatal("изменение не попало в журнал")
	}
}

// TestPropavshaya para проверяет, что пара, исчезнувшая из источника,
// удаляется и попадает в журнал. Период задаётся явно именно ради этого
// случая: по содержимому пустого слепка границы не восстановить.
func TestApplySnapshotNahoditUdaleniya(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2851, "История"),
	}); err != nil {
		t.Fatal(err)
	}

	res, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || res.Changed != 0 || res.Added != 0 {
		t.Fatalf("ожидалось одно удаление, получено %+v", res)
	}

	changes, err := s.ChangesSince(ctx, time.Now().Add(-time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.Kind == ChangeRemoved {
			if c.Before == nil {
				t.Fatal("у удаления должно быть состояние «до»")
			}
			if len(c.GroupNames) == 0 {
				t.Fatal("у удаления должны сохраниться группы для адресации уведомлений")
			}
			return
		}
	}
	t.Fatal("удаление не попало в журнал")
}

// TestIzmeneniyaZaPredelamiPerioda: слепок за одну неделю не должен трогать
// пары другой недели.
func TestApplySnapshotNeTrogaetChuzhoyPeriod(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
	}); err != nil {
		t.Fatal(err)
	}
	// Следующая неделя, пустая.
	res, err := s.ApplySnapshot(ctx, day(14), day(20), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changes() != 0 {
		t.Fatalf("слепок соседней недели затронул чужие пары: %+v", res)
	}
}

// TestPervoeNapolnenieNePishetVZhurnal: когда база пуста, а источник принёс
// тысячи пар, это значит, что мы узнали расписание, а не что вуз его
// переписал. В журнале от такого — десяток мегабайт, в которые никто не
// заглянет.
func TestPervoeNapolnenieNePishetVZhurnal(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	res, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2851, "История"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 {
		t.Fatalf("пары не добавлены: %+v", res)
	}

	changes, err := s.ChangesSince(ctx, time.Now().Add(-time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("первое наполнение записало %d изменений", len(changes))
	}

	// А вот следующая новая пара — уже настоящее изменение.
	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2851, "История"),
		para(3, 9, "14:00", 2851, "Экономика"),
	}); err != nil {
		t.Fatal(err)
	}
	changes, err = s.ChangesSince(ctx, time.Now().Add(-time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("новая пара дала %d записей в журнале, ожидалась одна", len(changes))
	}
}

func TestCleanupChanges(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Наполняем, потом меняем — чтобы в журнале появилась запись.
	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplySnapshot(ctx, day(7), day(13), []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		para(2, 8, "11:50", 2851, "История"),
	}); err != nil {
		t.Fatal(err)
	}

	// Свежие записи чистка не трогает.
	removed, err := s.CleanupChanges(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Errorf("удалено %d свежих записей", removed)
	}

	// А всё, что старше нуля, — уже история.
	removed, err = s.CleanupChanges(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if removed == 0 {
		t.Error("старые записи не удалены")
	}
}
