package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

var tables = []string{"lessons", "lesson_changes", "auditoriums", "groups", "lecturers", "collector_runs", "group_links", "group_fetches"}

func testRepo(t *testing.T) (*Repo, context.Context) {
	t.Helper()
	pool := db.TestPool(t, "TEST_DATABASE_URL_SCHEDULE", "schedulefu_test_schedule", tables...)
	return New(pool), context.Background()
}

func day(d int) time.Time   { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
func oidPtr(v int64) *int64 { return &v }
func intPtr(v int) *int     { return &v }

func para(oid int64, d int, begins string, aud int64, disc string) domain.Lesson {
	return domain.Lesson{
		LessonOid: oid, Date: day(d), BeginsAt: begins, EndsAt: "10:00",
		AuditoriumOid: oidPtr(aud), Auditorium: "ЛП49/2/313", Building: "Ленинградский проспект, 49/2",
		Discipline: disc, KindOfWork: "Семинар", LecturerOid: oidPtr(46674), LecturerName: "Замараева Е.И.",
		Stream: "ПИ24-1; ПИ24-2", GroupNames: []string{"ПИ24-1", "ПИ24-2"},
	}
}

// fill наполняет окно так, чтобы дальнейшие правки считались изменениями.
func fill(t *testing.T, r *Repo, ctx context.Context, lessons ...domain.Lesson) {
	t.Helper()
	if _, err := r.ApplySnapshot(ctx, day(7), day(13), lessons); err != nil {
		t.Fatal(err)
	}
}

func changes(t *testing.T, r *Repo, ctx context.Context) []domain.Change {
	t.Helper()
	c, err := r.ChangesSince(ctx, time.Now().Add(-time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Важнейший тест: сборщик ходит по кругу, и одинаковый слепок не должен
// считаться изменением — иначе шквал ложных уведомлений.
func TestPovtornoePrimenenieNeDayotLozhnyhIzmeneniy(t *testing.T) {
	r, ctx := testRepo(t)
	snap := []domain.Lesson{para(1, 7, "08:30", 2851, "Философия"), para(2, 8, "11:50", 2852, "История")}
	fill(t, r, ctx, snap...)
	for i := 0; i < 3; i++ {
		res, err := r.ApplySnapshot(ctx, day(7), day(13), snap)
		if err != nil || res.Changes() != 0 {
			t.Fatalf("проход %d: %+v %v", i+2, res, err)
		}
	}
}

// Первое наполнение — не изменение: появление данных у нас не есть правка
// вуза. Журнал от такого раздувался на десять мегабайт.
func TestPervoeNapolnenieNePishetVZhurnal(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"), para(2, 8, "11:50", 2851, "История"))
	if c := changes(t, r, ctx); len(c) != 0 {
		t.Fatalf("наполнение записало %d изменений", len(c))
	}
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"), para(2, 8, "11:50", 2851, "История"), para(3, 8, "14:00", 2851, "Экономика"))
	if c := changes(t, r, ctx); len(c) != 1 || c[0].Kind != domain.ChangeAdded {
		t.Fatalf("новая пара в известном дне: %+v", c)
	}
}

// День, впервые вошедший в скользящее окно, тоже не изменение: за неделю
// живой работы каждое утро журнал получал ~2400 «добавлений» — пары нового
// дня, а подписчик получил бы «добавлено 2400 пар» ежедневно.
func TestNovyyDenVOkneNeIzmenenie(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"))
	// Окно сдвинулось: появился день 14 с парами, день 7 остался.
	if _, err := r.ApplySnapshot(ctx, day(7), day(14), []domain.Lesson{
		para(1, 7, "08:30", 2851, "Философия"), para(10, 14, "08:30", 2851, "Право"), para(11, 14, "10:10", 2851, "Право"),
	}); err != nil {
		t.Fatal(err)
	}
	if c := changes(t, r, ctx); len(c) != 0 {
		t.Fatalf("новый день дал %d ложных изменений", len(c))
	}
}

func TestPerenosVDruguyuAuditoriyu(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"))
	moved := para(1, 7, "08:30", 2999, "Философия")
	moved.Auditorium = "ЛП51_1/0312"
	res, err := r.ApplySnapshot(ctx, day(7), day(13), []domain.Lesson{moved})
	if err != nil || res.Changed != 1 || res.Added+res.Removed != 0 {
		t.Fatalf("перенос: %+v %v", res, err)
	}
	c := changes(t, r, ctx)
	if len(c) != 1 || c[0].Before == nil || c[0].After == nil || c[0].Before.Auditorium == c[0].After.Auditorium {
		t.Fatalf("перенос не зафиксирован: %+v", c)
	}
}

// Период задаётся явно ради этого: по пустому слепку границы не восстановить.
func TestUdaleniya(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"), para(2, 8, "11:50", 2851, "История"))
	res, err := r.ApplySnapshot(ctx, day(7), day(13), []domain.Lesson{para(1, 7, "08:30", 2851, "Философия")})
	if err != nil || res.Removed != 1 {
		t.Fatalf("удаление: %+v %v", res, err)
	}
	c := changes(t, r, ctx)
	if len(c) != 1 || c[0].Kind != domain.ChangeRemoved || c[0].Before == nil || len(c[0].GroupNames) == 0 {
		t.Fatalf("удаление в журнале: %+v", c)
	}
}

func TestNeTrogaetChuzhoyPeriod(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"))
	res, err := r.ApplySnapshot(ctx, day(14), day(20), nil)
	if err != nil || res.Changes() != 0 {
		t.Fatalf("соседняя неделя тронула чужое: %+v %v", res, err)
	}
}

func TestCleanupChanges(t *testing.T) {
	r, ctx := testRepo(t)
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"))
	// Новая пара в уже известном дне — иначе это «день впервые в окне» и
	// журнал по правилу останется пуст.
	fill(t, r, ctx, para(1, 7, "08:30", 2851, "Философия"), para(2, 7, "11:50", 2851, "История"))
	if n, _ := r.CleanupChanges(ctx, 24*time.Hour); n != 0 {
		t.Errorf("свежее удалено: %d", n)
	}
	if n, _ := r.CleanupChanges(ctx, 0); n == 0 {
		t.Error("старое не удалено")
	}
}

func seedRooms(t *testing.T, r *Repo, ctx context.Context) {
	t.Helper()
	len49 := domain.Site{Slug: "leningradsky", Label: "Ленинградский", Order: 1}
	if err := r.UpsertAuditoriums(ctx, []domain.Auditorium{
		{Oid: 2851, Name: "ЛП49/2/313", Room: "313", Building: "Ленинградский проспект, 49/2", Site: len49, Kind: "Лекционная", Floor: intPtr(3), Capacity: intPtr(54), IsStudySpace: true},
		{Oid: 2852, Name: "ЛП49/2/314", Room: "314", Building: "Ленинградский проспект, 49/2", Site: len49, Kind: "Лекционная", Floor: intPtr(3), Capacity: intPtr(32), IsStudySpace: true},
		{Oid: 9999, Name: "Спортивный зал", Room: "Спортивный зал", Building: "Касаткина", Site: domain.Site{Slug: "kasatkina", Label: "Касаткина", Order: 10}, Kind: "Спортивный зал"},
	}); err != nil {
		t.Fatal(err)
	}
	second := para(2, 7, "11:50", 2851, "История")
	second.EndsAt = "13:20"
	if _, err := r.ApplySnapshot(ctx, day(7), day(7), []domain.Lesson{para(1, 7, "08:30", 2851, "Философия"), second}); err != nil {
		t.Fatal(err)
	}
}

func TestSiteDayISvobodnye(t *testing.T) {
	r, ctx := testRepo(t)
	seedRooms(t, r, ctx)
	days, err := r.SiteDay(ctx, "leningradsky", day(7))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 {
		t.Fatalf("на площадке %d аудиторий, ожидалось 2 (спортзал вне)", len(days))
	}
	byOid := map[int64]int{}
	for _, d := range days {
		byOid[d.Auditorium.Oid] = len(d.Lessons)
	}
	if byOid[2851] != 2 || byOid[2852] != 0 {
		t.Errorf("пары по аудиториям: %v", byOid)
	}
	// Граница пары и «до когда свободна» — через домен.
	for _, d := range days {
		if d.Auditorium.Oid != 2851 {
			continue
		}
		v := domain.BuildRoomView(d.Auditorium, d.Lessons, "10:00")
		if !v.FreeNow || v.FreeUntil != "11:50" {
			t.Errorf("в 10:00: FreeNow=%v until=%q", v.FreeNow, v.FreeUntil)
		}
		if domain.BuildRoomView(d.Auditorium, d.Lessons, "14:00").FreeUntil != "" {
			t.Error("после последней пары граница должна быть пустой")
		}
	}
}

func TestSitesIsklyuchayutFilialy(t *testing.T) {
	r, ctx := testRepo(t)
	seedRooms(t, r, ctx)
	if err := r.UpsertAuditoriums(ctx, []domain.Auditorium{{Oid: 7, Name: "Омск-1", Room: "1", Site: domain.Site{Slug: "branch", Label: "Филиалы", Order: 90}, IsStudySpace: true}}); err != nil {
		t.Fatal(err)
	}
	sites, err := r.Sites(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sites {
		if s.Site.Slug == "branch" {
			t.Fatal("филиал попал в площадки")
		}
	}
	if len(sites) != 1 || sites[0].Rooms != 2 {
		t.Errorf("площадки: %+v", sites)
	}
}

func TestScheduleForIChuzhayaGruppa(t *testing.T) {
	r, ctx := testRepo(t)
	seedRooms(t, r, ctx)
	ls, err := r.ScheduleFor(ctx, domain.GroupSubject("ПИ24-1"), day(7), day(7))
	if err != nil || len(ls) != 2 || ls[0].BeginsAt != "08:30" {
		t.Fatalf("группа: %d %v", len(ls), err)
	}
	if none, _ := r.ScheduleFor(ctx, domain.GroupSubject("Ю24-1"), day(7), day(7)); len(none) != 0 {
		t.Errorf("чужой группе выдано %d пар", len(none))
	}
	if lec, _ := r.ScheduleFor(ctx, domain.LecturerSubject(46674), day(7), day(7)); len(lec) != 2 {
		t.Errorf("преподаватель: %d пар", len(lec))
	}
}

func TestAuditoriumOidsFiltruetNeuchebnye(t *testing.T) {
	r, ctx := testRepo(t)
	seedRooms(t, r, ctx)
	all, _ := r.AuditoriumOids(ctx, false)
	study, _ := r.AuditoriumOids(ctx, true)
	if len(all) != 3 || len(study) != 2 {
		t.Errorf("все %d, учебных %d", len(all), len(study))
	}
}

// Замер самого частого случая — повторный слепок без изменений. Запись
// пачками дала 696 -> 167 мс на 11 000 пар; это не должно откатиться.
func TestSkorostPovtornogoProhoda(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	r, ctx := testRepo(t)
	snap := make([]domain.Lesson, 0, 11000)
	for i := 0; i < 11000; i++ {
		l := para(int64(100000+i), 7+i%6, "08:30", int64(2000+i%500), "Дисциплина")
		snap = append(snap, l)
	}
	fill(t, r, ctx, snap...)
	start := time.Now()
	res, err := r.ApplySnapshot(ctx, day(7), day(13), snap)
	if err != nil || res.Changes() != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	t.Logf("повторный проход 11 000 пар: %s", time.Since(start).Round(time.Millisecond))
}

func TestSvyaziGruppyDobavlyayutPary(t *testing.T) {
	repo, ctx := testRepo(t)
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	stream := domain.Lesson{LessonOid: 901, Date: day, BeginsAt: "11:50", EndsAt: "13:20", Discipline: "Иностранный язык",
		GroupNames: []string{"006126_2 Иностранный язык (КАЯиПК)-3"}}
	own := domain.Lesson{LessonOid: 902, Date: day, BeginsAt: "14:00", EndsAt: "15:30", Discipline: "Философия", GroupNames: []string{"ПИ24-1"}}
	if _, err := repo.ApplySnapshot(ctx, day, day, []domain.Lesson{stream, own}); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.ScheduleFor(ctx, domain.GroupSubject("ПИ24-1"), day, day); len(got) != 1 {
		t.Fatalf("до привязки ожидали одну пару, получили %d", len(got))
	}
	if _, ok, _ := repo.GroupFetchedOn(ctx, "ПИ24-1"); ok {
		t.Fatal("группу ещё не дотягивали")
	}
	if err := repo.ApplyGroupLinks(ctx, "ПИ24-1", []int64{901, 999}, day); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.ScheduleFor(ctx, domain.GroupSubject("ПИ24-1"), day, day)
	if len(got) != 2 || got[0].Discipline != "Иностранный язык" {
		t.Errorf("после привязки ожидали английский и философию, получили %+v", got)
	}
	if on, ok, _ := repo.GroupFetchedOn(ctx, "ПИ24-1"); !ok || !on.Equal(day) {
		t.Errorf("дата дотягивания: %v %v", on, ok)
	}
}
