package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// discRepo — одна дисциплина с потоком на пятнадцать групп.
type discRepo struct{ windowRepo }

func (discRepo) SearchDisciplines(_ context.Context, q string, _ int) ([]sched.DisciplineHit, error) {
	var groups []string
	for i := 1; i <= 15; i++ {
		groups = append(groups, "ЭБ24-"+strings.Repeat("1", 1)+string(rune('0'+i%10)))
	}
	return []sched.DisciplineHit{{Name: "Эконометрика", Lessons: 14, Kinds: []string{"Лекция", "Семинар"},
		Lecturers: []sched.Lecturer{{Oid: 46674, Name: "Замараева Е.И."}}, Groups: groups,
		Buildings: []string{"Ленинградский проспект, 49/2"}}}, nil
}

func TestPoiskPoDistsiplineNaEkrane(t *testing.T) {
	clk := clock.Fixed(time.Date(2026, 9, 23, 9, 0, 0, 0, time.FixedZone("MSK", 3*3600)))
	s, err := New(Deps{Schedule: schedule.New(nil, discRepo{}, clk, nil, schedule.Options{}), Clock: clk,
		BuildingLabel: func(string) string { return "Ленинградский" }})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, s, "/disciplines?q=%D1%8D%D0%BA%D0%BE%D0%BD%D0%BE%D0%BC")
	for _, want := range []string{"Эконометрика", "14 пар в окне · Лекция, Семинар · Ленинградский",
		`href="/schedule?lecturer=46674"`, "и ещё 3"} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q", want)
		}
	}
	_, body = get(t, s, "/disciplines?q=%D1%8D")
	if !strings.Contains(body, "Нужно хотя бы две буквы") {
		t.Error("однобуквенный запрос должен объясняться, а не выдавать полрасписания")
	}
}
