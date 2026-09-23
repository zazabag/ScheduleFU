package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/modules/source"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// fakeRepo — хранилище в памяти ровно на то, что трогает проход сборщика.
// Встроенный интерфейс держит остальные методы: вызов любого из них в
// тесте упадёт, и это желаемо — значит, проход полез куда не должен.
type fakeRepo struct {
	Repository
	groups map[string]domain.Group
}

func (r *fakeRepo) StartRun(context.Context, time.Time, time.Time) (int64, error) { return 1, nil }
func (r *fakeRepo) FinishRun(context.Context, int64, int, int, int, int, error) error {
	return nil
}
func (r *fakeRepo) AuditoriumOids(context.Context, bool) ([]int64, error) { return []int64{1}, nil }
func (r *fakeRepo) ApplySnapshot(context.Context, time.Time, time.Time, []domain.Lesson) (domain.ApplyResult, error) {
	return domain.ApplyResult{}, nil
}
func (r *fakeRepo) UpsertAuditoriums(context.Context, []domain.Auditorium) error { return nil }
func (r *fakeRepo) UpsertLecturers(context.Context, []domain.Lecturer) error     { return nil }
func (r *fakeRepo) CleanupChanges(context.Context, time.Duration) (int64, error) { return 0, nil }

func (r *fakeRepo) MissingGroups(_ context.Context, names []string) ([]string, error) {
	var out []string
	for _, n := range names {
		if _, ok := r.groups[n]; !ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func (r *fakeRepo) UpsertGroups(_ context.Context, items []domain.Group) error {
	for _, g := range items {
		r.groups[g.Name] = g
	}
	return nil
}

// fakeSource отдаёт одну пару и ведёт счёт поисковых запросов.
type fakeSource struct {
	stream   string
	index    map[string][]source.SearchResult
	searches []string
}

func (s *fakeSource) Search(_ context.Context, _ source.SearchKind, term string) ([]source.SearchResult, error) {
	s.searches = append(s.searches, term)
	return s.index[term], nil
}

func (s *fakeSource) Schedule(context.Context, source.Kind, int64, time.Time, time.Time) ([]source.Lesson, error) {
	return []source.Lesson{{LessonOid: 1, Date: "2026-09-23", BeginLesson: "14:00", EndLesson: "15:30",
		Discipline: "Основы российской государственности", Stream: s.stream}}, nil
}

func (s *fakeSource) ParseAuditorium(name, building string) source.Auditorium {
	return source.Auditorium{Name: name, Building: building}
}

func newCollector(src *fakeSource, repo *fakeRepo) *Service {
	clk := clock.Fixed(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	return New(src, repo, clk, nil, Options{Workers: 1})
}

// Баг 23.09.2026: справочник групп наполнялся только посевом, и набор
// 2026 года (УПП26-2 и другие) не находился поиском, хотя пары этих групп
// лежали в базе. Проход сборщика обязан дописывать группы, которых нет.
func TestNovayaGruppaIzParPopadaetVSpravochnik(t *testing.T) {
	src := &fakeSource{
		stream: "УПП26-1; УПП26-2",
		index: map[string][]source.SearchResult{
			"УПП26-1": {{ID: "170001", Label: "УПП26-1", Description: "3"}},
			"УПП26-2": {{ID: "170002", Label: "УПП26-2", Description: "3"}},
		},
	}
	repo := &fakeRepo{groups: map[string]domain.Group{}}
	if _, err := newCollector(src, repo).Collect(context.Background(), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	g, ok := repo.groups["УПП26-2"]
	if !ok {
		t.Fatalf("УПП26-2 не попала в справочник: %v", repo.groups)
	}
	if g.ID != 170002 || g.FacultyOid != "3" || g.AdmissionYear == nil || *g.AdmissionYear != 2026 {
		t.Errorf("запись группы: %+v", g)
	}
}

// Вежливость к источнику: известная группа не ищется вовсе, а ненайденная
// не спрашивается у вуза заново на каждом часовом проходе.
func TestPoiskGruppyNeDergaetIstochnikZrya(t *testing.T) {
	src := &fakeSource{
		stream: "ПИ24-1; ПИ24-2; 006073_2 Иностранный язык (КАЯиПК)-10 ПИ24-1_2",
		index:  map[string][]source.SearchResult{},
	}
	repo := &fakeRepo{groups: map[string]domain.Group{"ПИ24-1": {ID: 1, Name: "ПИ24-1"}}}
	svc := newCollector(src, repo)
	for i := 0; i < 3; i++ {
		if _, err := svc.Collect(context.Background(), time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Один запрос за три прохода: ПИ24-2 не нашлась и отложена на сутки,
	// ПИ24-1 известна, имя языкового потока группой не считается.
	if len(src.searches) != 1 || src.searches[0] != "ПИ24-2" {
		t.Errorf("запросы к источнику: %q", src.searches)
	}
}

// Выдача поиска — по подстроке, и соседи по выдаче записываются сразу:
// «УПП26-1» находит и «УПП26-10», второго запроса за ней не будет.
func TestSosediPoVydacheZapisyvayutsyaSrazu(t *testing.T) {
	src := &fakeSource{
		stream: "УПП26-1; УПП26-10",
		index: map[string][]source.SearchResult{
			"УПП26-1": {
				{ID: "170001", Label: "УПП26-1", Description: "3"},
				{ID: "170010", Label: "УПП26-10", Description: "3"},
			},
		},
	}
	repo := &fakeRepo{groups: map[string]domain.Group{}}
	if _, err := newCollector(src, repo).Collect(context.Background(), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.groups["УПП26-10"]; !ok || len(src.searches) != 1 {
		t.Errorf("группы %v, запросы %q", repo.groups, src.searches)
	}
}

func TestAdmissionYear(t *testing.T) {
	for name, want := range map[string]int{"ПИ24-1": 2024, "УПП26-2": 2026, "Ю24-5в": 2024} {
		if got := AdmissionYear(name); got == nil || *got != want {
			t.Errorf("%s: %v", name, got)
		}
	}
	if AdmissionYear("тест") != nil {
		t.Error("год у имени без цифр")
	}
}

// throttledSource отвечает «слишком часто» на всё и считает обращения.
type throttledSource struct {
	fakeSource
	calls int
}

func (s *throttledSource) Schedule(context.Context, source.Kind, int64, time.Time, time.Time) ([]source.Lesson, error) {
	s.calls++
	return nil, &source.ThrottledError{Status: 429, RetryAfter: 2 * time.Hour}
}

func (s *throttledSource) Search(context.Context, source.SearchKind, string) ([]source.SearchResult, error) {
	s.calls++
	return nil, &source.ThrottledError{Status: 429}
}

type countingRepo struct {
	fakeRepo
	oids    []int64
	applied int
}

func (r *countingRepo) AuditoriumOids(context.Context, bool) ([]int64, error) { return r.oids, nil }
func (r *countingRepo) ApplySnapshot(context.Context, time.Time, time.Time, []domain.Lesson) (domain.ApplyResult, error) {
	r.applied++
	return domain.ApplyResult{}, nil
}
func (r *countingRepo) GroupFetchedOn(context.Context, string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (r *countingRepo) GroupID(context.Context, string) (int64, bool, error) { return 7, true, nil }

// Вуз попросил сбавить темп — проход останавливается на первом же отказе,
// а не добивает оставшиеся шестьсот аудиторий. Слепок не применяется:
// иначе недоопрошенные аудитории превратились бы в ложные отмены пар.
// До конца паузы к вузу не ходит ни сборщик, ни ленивая привязка групп.
func TestPosle429SborshchikZamolkaetNaPauzu(t *testing.T) {
	src := &throttledSource{}
	oids := make([]int64, 600)
	for i := range oids {
		oids[i] = int64(i + 1)
	}
	repo := &countingRepo{fakeRepo: fakeRepo{groups: map[string]domain.Group{}}, oids: oids}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	svc := New(src, repo, clock.Fixed(now), nil, Options{Workers: 1})

	_, err := svc.Collect(context.Background(), now, now)
	var th *source.ThrottledError
	if !errors.As(err, &th) {
		t.Fatalf("ожидался отказ источника, получено: %v", err)
	}
	if src.calls != 1 || repo.applied != 0 {
		t.Fatalf("запросов %d, применений слепка %d", src.calls, repo.applied)
	}
	if until, ok := svc.PausedUntil(); !ok || !until.Equal(now.Add(2*time.Hour)) {
		t.Errorf("пауза до %s (%v)", until, ok)
	}

	if _, err := svc.Collect(context.Background(), now, now); !errors.Is(err, ErrPaused) {
		t.Errorf("проход во время паузы: %v", err)
	}
	if err := svc.EnsureGroupLinks(context.Background(), "ПИ24-1", now, now); !errors.Is(err, ErrPaused) {
		t.Errorf("привязка группы во время паузы: %v", err)
	}
	if src.calls != 1 {
		t.Errorf("во время паузы было %d запросов к вузу", src.calls-1)
	}
}

// Пауза без Retry-After не короче получаса, а просьба подождать сутки
// ужимается до шести часов: иначе расписание протухло бы на весь день.
func TestPauzaOgranichenaSnizuISverhu(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:              30 * time.Minute,
		time.Minute:    30 * time.Minute,
		2 * time.Hour:  2 * time.Hour,
		24 * time.Hour: 6 * time.Hour,
	}
	for in, want := range cases {
		if got := pauseFor(in); got != want {
			t.Errorf("%s: %s, ожидалось %s", in, got, want)
		}
	}
}

// Ночью расписание никто не смотрит и деканат его не правит: проходы
// реже. Окно переходит через полночь.
func TestNochyuObkhodRezhe(t *testing.T) {
	p := Pace{Interval: time.Hour, NightInterval: 3 * time.Hour, NightFrom: "23:00", NightTo: "06:30"}
	msk := time.FixedZone("MSK", 3*3600)
	cases := map[string]time.Duration{
		"12:00": time.Hour,
		"22:59": time.Hour,
		"23:00": 3 * time.Hour,
		"02:00": 3 * time.Hour,
		"06:29": 3 * time.Hour,
		"06:30": time.Hour,
	}
	for hhmm, want := range cases {
		at, _ := time.ParseInLocation("2006-01-02 15:04", "2026-09-23 "+hhmm, msk)
		if got := p.Next(at); got != want {
			t.Errorf("%s: %s, ожидалось %s", hhmm, got, want)
		}
	}
	if (Pace{Interval: time.Hour}).Next(time.Now()) != time.Hour {
		t.Error("без ночного окна интервал должен быть обычным")
	}
}
