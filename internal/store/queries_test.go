package store

import (
	"context"
	"testing"
	"time"
)

func mskTime(d int, clock string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04", "2026-09-0"+string(rune('0'+d))+" "+clock)
	return t
}

// setupFree готовит две аудитории и одну пару в первой из них.
func setupFree(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()

	if err := s.UpsertAuditoriums(ctx, []Auditorium{
		{Oid: 2851, Name: "ЛП49/2/313", Room: "313",
			Building: "Ленинградский проспект, 49/2", Campus: "leningradsky",
			Kind: "Лекционная", Floor: intPtr(3), Capacity: intPtr(54), IsStudySpace: true},
		{Oid: 2852, Name: "ЛП49/2/314", Room: "314",
			Building: "Ленинградский проспект, 49/2", Campus: "leningradsky",
			Kind: "Лекционная", Floor: intPtr(3), Capacity: intPtr(32), IsStudySpace: true},
		{Oid: 9999, Name: "Спортивный зал", Room: "Спортивный зал",
			Building: "ул. Касаткина 15", Campus: "other",
			Kind: "Спортивный зал", IsStudySpace: false},
	}); err != nil {
		t.Fatal(err)
	}

	// В 313-й пара с 08:30 до 10:00, потом с 11:50.
	lessons := []Lesson{
		para(1, 7, "08:30", 2851, "Философия"),
		func() Lesson {
			l := para(2, 7, "11:50", 2851, "История")
			l.EndsAt = "13:20"
			return l
		}(),
	}
	if _, err := s.ApplySnapshot(ctx, day(7), day(7), lessons); err != nil {
		t.Fatal(err)
	}
	return s, ctx
}

func intPtr(v int) *int { return &v }

func TestFreeAuditoriumsVoVremyaPary(t *testing.T) {
	s, ctx := setupFree(t)

	// 09:00 — идёт пара в 313-й, свободной она быть не должна.
	free, err := s.FreeAuditoriums(ctx, mskTime(7, "09:00"), "leningradsky")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range free {
		if f.Oid == 2851 {
			t.Fatal("занятая аудитория попала в свободные")
		}
		if f.Oid == 9999 {
			t.Fatal("спортзал попал в выдачу «где позаниматься»")
		}
	}
	var found314 bool
	for _, f := range free {
		if f.Oid == 2852 {
			found314 = true
		}
	}
	if !found314 {
		t.Fatal("свободная аудитория не попала в выдачу")
	}
}

// TestFreeAuditoriumsGranicaPary проверяет границы интервала: пара
// 08:30-10:00 не должна считаться идущей ровно в 10:00.
func TestFreeAuditoriumsGranicaPary(t *testing.T) {
	s, ctx := setupFree(t)

	free, err := s.FreeAuditoriums(ctx, mskTime(7, "10:00"), "leningradsky")
	if err != nil {
		t.Fatal(err)
	}
	var free313 bool
	for _, f := range free {
		if f.Oid == 2851 {
			free313 = true
			// Следующая пара в 11:50 — значит свободна до неё.
			if f.FreeUntil == nil || *f.FreeUntil != "11:50" {
				t.Errorf("FreeUntil = %v, ожидалось 11:50", f.FreeUntil)
			}
		}
	}
	if !free313 {
		t.Fatal("в 10:00 аудитория должна быть уже свободна")
	}

	// А ровно в 08:30 пара идёт.
	free, err = s.FreeAuditoriums(ctx, mskTime(7, "08:30"), "leningradsky")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range free {
		if f.Oid == 2851 {
			t.Fatal("в 08:30 пара уже идёт, аудитория не свободна")
		}
	}
}

// TestFreeUntilPustoyKogdaParBolsheNet: после последней пары дня граница
// не указывается.
func TestFreeUntilPustoyKogdaParBolsheNet(t *testing.T) {
	s, ctx := setupFree(t)

	free, err := s.FreeAuditoriums(ctx, mskTime(7, "14:00"), "leningradsky")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range free {
		if f.Oid == 2851 {
			if f.FreeUntil != nil {
				t.Errorf("после последней пары FreeUntil = %v, ожидалось «до конца дня»", *f.FreeUntil)
			}
			return
		}
	}
	t.Fatal("аудитория должна быть свободна после последней пары")
}

func TestScheduleForGroup(t *testing.T) {
	s, ctx := setupFree(t)

	lessons, err := s.ScheduleForGroup(ctx, "ПИ24-1", day(7), day(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(lessons) != 2 {
		t.Fatalf("ожидалось 2 пары, получено %d", len(lessons))
	}
	if lessons[0].BeginsAt != "08:30" || lessons[1].BeginsAt != "11:50" {
		t.Error("пары не отсортированы по времени")
	}
	if lessons[0].Floor == nil || *lessons[0].Floor != 3 {
		t.Error("этаж не подтянулся из справочника")
	}

	// Группы, которой нет в потоке, расписание не показываем.
	none, err := s.ScheduleForGroup(ctx, "Ю24-1", day(7), day(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("чужой группе выдано %d пар", len(none))
	}
}

func TestWhereIsLecturer(t *testing.T) {
	s, ctx := setupFree(t)

	at, err := s.WhereIsLecturer(ctx, 46674, mskTime(7, "09:00"))
	if err != nil {
		t.Fatal(err)
	}
	if at == nil {
		t.Fatal("преподаватель должен быть найден во время пары")
	}
	if at.Auditorium != "ЛП49/2/313" {
		t.Errorf("аудитория = %q", at.Auditorium)
	}

	// В перерыве преподавателя нигде нет — и это нормальный ответ.
	none, err := s.WhereIsLecturer(ctx, 46674, mskTime(7, "10:30"))
	if err != nil {
		t.Fatal(err)
	}
	if none != nil {
		t.Error("в перерыве преподаватель не должен находиться в аудитории")
	}
}

func TestOccupancyForAuditorium(t *testing.T) {
	s, ctx := setupFree(t)

	occ, err := s.OccupancyForAuditorium(ctx, 2851, day(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(occ) != 2 {
		t.Fatalf("ожидалось 2 пары, получено %d", len(occ))
	}
}
