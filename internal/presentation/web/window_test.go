package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// windowRepo — площадка из двух аудиторий: 0512 свободна весь день,
// 0514 занята посреди окна.
type windowRepo struct{ schedule.Repository }

func floorPtr(n int) *int { return &n }

var lp = sched.Site{Slug: "leningradsky", Label: "Ленинградский"}

func (windowRepo) SiteDay(context.Context, string, time.Time) ([]schedule.RoomDay, error) {
	return []schedule.RoomDay{
		{Auditorium: sched.Auditorium{Oid: 1, Room: "0512", Building: "ЛП51", Site: lp, Floor: floorPtr(5), IsStudySpace: true}},
		{Auditorium: sched.Auditorium{Oid: 2, Room: "0514", Building: "ЛП51", Site: lp, Floor: floorPtr(5), IsStudySpace: true},
			Lessons: []sched.Lesson{{BeginsAt: "11:50", EndsAt: "13:20", Discipline: "История"}}},
	}, nil
}
func (windowRepo) Auditorium(_ context.Context, oid int64) (sched.Auditorium, bool, error) {
	return sched.Auditorium{Oid: oid, Room: "0512", Building: "ЛП51", Site: lp, Floor: floorPtr(5), IsStudySpace: true}, true, nil
}
func (windowRepo) Sites(context.Context) ([]schedule.SiteRow, error) {
	return []schedule.SiteRow{{Site: lp, Rooms: 2}}, nil
}
func (windowRepo) LastSuccessfulRun(context.Context) (time.Time, bool, error) {
	return time.Time{}, false, nil
}

func windowServer(t *testing.T) *Server {
	t.Helper()
	clk := clock.Fixed(time.Date(2026, 9, 23, 9, 0, 0, 0, time.FixedZone("MSK", 3*3600)))
	s, err := New(Deps{Schedule: schedule.New(nil, windowRepo{}, clk, nil, schedule.Options{}), Clock: clk,
		BuildingLabel: func(b string) string { return b }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOknoPokazyvaetTolkoSvobodnyeVsyoVremya(t *testing.T) {
	s := windowServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rooms?date=2026-09-23&from=11:40&to=14:00&near=1", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d: %s", rec.Code, body)
	}
	if !strings.Contains(body, "Где пересидеть окно") || !strings.Contains(body, "рядом с 0512") {
		t.Error("заголовок окна не нарисован")
	}
	if !strings.Contains(body, ">0512<") || strings.Contains(body, ">0514<") {
		t.Error("0514 занята посреди окна и не должна попасть в список")
	}
}

func TestIntervalBezNulyaVChasahOtklonyaetsya(t *testing.T) {
	s := windowServer(t)
	rec := httptest.NewRecorder()
	// «9:30» строкой больше «14:00»: без проверки интервал вышел бы пустым.
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rooms?from=9:30&to=14:00", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("код %d, ожидался 400", rec.Code)
	}
}

func TestOknoMezhduParamiNaEkraneDnya(t *testing.T) {
	s := windowServer(t)
	rs := rows("08:30", "10:00", "14:00", "15:30", "15:40", "17:10")
	rs[1].AuditoriumOid, rs[1].Room = 1, "0512"
	date := time.Date(2026, 9, 23, 0, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	s.markWindows(context.Background(), rs, date, "2026-09-23", "2026-09-23", "09:00")
	g := rs[0].Gap
	if g == nil {
		t.Fatal("между 10:00 и 14:00 окно")
	}
	if g.Length != "4 ч" || g.Free != "1 свободная" || g.Near != "рядом с 0512" {
		t.Errorf("окно: %+v", g)
	}
	if rs[1].Gap != nil {
		t.Error("десять минут между парами — перемена, не окно")
	}
}

func TestProshedsheeOknoNePokazyvaetsya(t *testing.T) {
	s := windowServer(t)
	rs := rows("08:30", "10:00", "14:00", "15:30")
	date := time.Date(2026, 9, 23, 0, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	s.markWindows(context.Background(), rs, date, "2026-09-23", "2026-09-23", "14:30")
	if rs[0].Gap != nil {
		t.Error("окно кончилось в 14:00")
	}
}

func TestDlitelnostPoRusski(t *testing.T) {
	for min, want := range map[int]string{40: "40 мин", 60: "1 ч", 140: "2 ч 20 мин"} {
		if got := durationRu(min); got != want {
			t.Errorf("%d: %q, ожидалось %q", min, got, want)
		}
	}
}

// Расписания двух групп для экрана общих окон: у ПИ24-1 пары утром и
// вечером, у ПИ24-2 — утром и после обеда. Связи подгрупп уже дотянуты.
func (windowRepo) ScheduleFor(_ context.Context, s sched.Subject, from, _ time.Time) ([]sched.Lesson, error) {
	day := from.AddDate(0, 0, 2)
	mk := func(b, e string) sched.Lesson {
		return sched.Lesson{Date: day, BeginsAt: b, EndsAt: e, Discipline: "x"}
	}
	switch s.Group {
	case "ПИ24-1":
		return []sched.Lesson{mk("08:30", "10:00"), mk("15:40", "17:10")}, nil
	case "ПИ24-2":
		return []sched.Lesson{mk("08:30", "10:00"), mk("14:00", "15:30")}, nil
	}
	return nil, nil
}
func (windowRepo) GroupFetchedOn(context.Context, string) (time.Time, bool, error) {
	return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), true, nil
}

func TestObshchieOknaDvuhGrupp(t *testing.T) {
	s := windowServer(t)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/together?g=пи24-1,ПИ24-2&g=ПИ99-9&date=2026-09-23", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if !strings.Contains(body, "среда, 10:10—13:20 — у всех окно между парами") {
		t.Errorf("лучшее время не найдено:\n%s", body[strings.Index(body, "<main"):])
	}
	if !strings.Contains(body, "ПИ99-9 · пар нет") {
		t.Error("неизвестная группа должна быть видна как пустая, а не пропасть молча")
	}
}

func TestPereezdZaPeremenuPomechaetsya(t *testing.T) {
	rs := rows("08:30", "10:00", "10:10", "11:40", "14:00", "15:30")
	rs[0].Place, rs[1].Place, rs[2].Place = "Ленинградский", "Кибальчича", "Ленинградский"
	markMoves(rs)
	if rs[0].Move == nil || rs[0].Move.Break != "10 мин" || rs[0].Move.To != "Кибальчича" {
		t.Errorf("переезд за десять минут: %+v", rs[0].Move)
	}
	if rs[1].Move != nil {
		t.Error("между 11:40 и 14:00 окно — о нём говорит карточка окна, не предупреждение")
	}
}

func TestVyklyuchennoeGdePrepodavatelVedyotNaRaspisanie(t *testing.T) {
	s := windowServer(t)
	s.d.HideWhereLecturer = true
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/lecturers?oid=46674", nil))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/schedule?lecturer=46674" {
		t.Errorf("код %d, адрес %q", rec.Code, rec.Header().Get("Location"))
	}
}

// Справочник аудиторий для плана: две на 4 этаже 51 к.1, одна на 6-м.
func (windowRepo) Auditoriums(context.Context) ([]sched.Auditorium, error) {
	const b = "Ленинградский проспект, 51, корп. 1"
	return []sched.Auditorium{
		{Room: "0412", Building: b, Site: lp}, {Room: "0409", Building: b, Site: lp}, {Room: "0611", Building: b, Site: lp},
	}, nil
}
