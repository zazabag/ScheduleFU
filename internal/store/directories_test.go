package store

import (
	"context"
	"testing"
)

func TestUpsertAuditoriumsFromLessonsStavitOid(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	lessons := []Lesson{
		{
			LessonOid:     1,
			Date:          day(7),
			BeginsAt:      "08:30",
			EndsAt:        "10:00",
			AuditoriumOid: oidPtr(2851),
			Auditorium:    "ЛП49/2/313",
			Building:      "Ленинградский проспект, 49/2",
		},
		{
			LessonOid:     2,
			Date:          day(7),
			BeginsAt:      "11:50",
			EndsAt:        "13:20",
			AuditoriumOid: oidPtr(2999),
			Auditorium:    "ЛП51_1/0312",
			Building:      "Ленинградский проспект, 51, корп. 1",
		},
	}
	if err := s.UpsertAuditoriumsFromLessons(ctx, lessons); err != nil {
		t.Fatalf("сохранение справочника: %v", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT oid, room, campus, floor FROM auditoriums ORDER BY oid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	type got struct {
		oid   int64
		room  string
		camp  string
		floor *int
	}
	var all []got
	for rows.Next() {
		var g got
		if err := rows.Scan(&g.oid, &g.room, &g.camp, &g.floor); err != nil {
			t.Fatal(err)
		}
		all = append(all, g)
	}
	if len(all) != 2 {
		t.Fatalf("ожидалось 2 аудитории, получено %d", len(all))
	}
	for _, g := range all {
		if g.oid == 0 {
			t.Fatal("идентификатор аудитории не проставлен")
		}
		if g.camp != "leningradsky" {
			t.Errorf("аудитория %d: кампус %q, ожидался leningradsky", g.oid, g.camp)
		}
		if g.floor == nil {
			t.Errorf("аудитория %d (%s): этаж не определён", g.oid, g.room)
		}
	}
	// 313 -> третий этаж, 0312 -> тоже третий, но по другому правилу.
	for _, g := range all {
		if *g.floor != 3 {
			t.Errorf("аудитория %s: этаж %d, ожидался 3", g.room, *g.floor)
		}
	}
}

func TestAuditoriumOidsFiltruetNeuchebnye(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	err := s.UpsertAuditoriums(ctx, []Auditorium{
		{Oid: 1, Name: "ЛП49/2/313", Room: "313", Campus: "leningradsky",
			Kind: "Лекционная", IsStudySpace: true},
		{Oid: 2, Name: "Спортивный зал", Room: "Спортивный зал", Campus: "other",
			Kind: "Спортивный зал", IsStudySpace: false},
	})
	if err != nil {
		t.Fatal(err)
	}

	all, err := s.AuditoriumOids(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("без фильтра ожидалось 2, получено %d", len(all))
	}
	study, err := s.AuditoriumOids(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(study) != 1 || study[0] != 1 {
		t.Fatalf("с фильтром ожидалась только учебная аудитория, получено %v", study)
	}
}
