package domain

import (
	"strings"
	"testing"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

var msk = time.FixedZone("MSK", 3*3600)

func change(kind sched.ChangeKind, day int, groups []string, lecturer int64) sched.Change {
	c := sched.Change{Kind: kind, LessonDate: time.Date(2026, 9, day, 0, 0, 0, 0, msk), GroupNames: groups}
	if lecturer > 0 {
		c.LecturerOid = &lecturer
	}
	return c
}

func TestBuildGruppiruetVOdnoSoobshchenie(t *testing.T) {
	var cs []sched.Change
	for i := 0; i < 40; i++ {
		cs = append(cs, change(sched.ChangeAdded, 14, []string{"ПИ24-1"}, 0))
	}
	got := Build(cs, msk)
	if len(got) != 1 || !strings.Contains(got["group:ПИ24-1"].Body, "40") || got["group:ПИ24-1"].Tag == "" {
		t.Fatalf("сорок правок должны дать одно письмо с числом и тегом: %+v", got)
	}
}

func TestBuildRazdelyaetGruppuIPrepodavatelya(t *testing.T) {
	got := Build([]sched.Change{change(sched.ChangeChanged, 14, []string{"ПИ24-1", "ПИ24-2"}, 46674)}, msk)
	for _, k := range []string{"group:ПИ24-1", "group:ПИ24-2", "lecturer:46674"} {
		if _, ok := got[k]; !ok {
			t.Errorf("нет письма для %q", k)
		}
	}
}

func TestBuildTekst(t *testing.T) {
	body := Build([]sched.Change{
		change(sched.ChangeAdded, 14, []string{"Ю24-5"}, 0),
		change(sched.ChangeRemoved, 14, []string{"Ю24-5"}, 0),
		change(sched.ChangeChanged, 14, []string{"Ю24-5"}, 0),
	}, msk)["group:Ю24-5"].Body
	for _, w := range []string{"добавлена", "изменена", "отменена", "14 сентября"} {
		if !strings.Contains(body, w) {
			t.Errorf("в %q нет %q", body, w)
		}
	}
	two := Build([]sched.Change{change(sched.ChangeAdded, 14, []string{"Ю24-5"}, 0), change(sched.ChangeAdded, 15, []string{"Ю24-5"}, 0)}, msk)
	if !strings.Contains(two["group:Ю24-5"].Body, "2 дн") {
		t.Errorf("несколько дней: %q", two["group:Ю24-5"].Body)
	}
}

func TestPluralRusskieChislitelnye(t *testing.T) {
	cases := map[int]string{1: "добавлена пара", 2: "добавлены 2 пары", 5: "добавлено 5 пар", 11: "добавлено 11 пар",
		14: "добавлено 14 пар", 21: "добавлена 21 пара", 22: "добавлены 22 пары", 101: "добавлена 101 пара"}
	for n, want := range cases {
		if got := plural(n, "добавлена пара", "добавлены %d пары", "добавлено %d пар"); got != want {
			t.Errorf("plural(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

func TestBuildBezAdresata(t *testing.T) {
	// У языковых занятий состав групп пуст — адресовать некому, и это не ошибка.
	if got := Build([]sched.Change{change(sched.ChangeAdded, 14, nil, 0)}, msk); len(got) != 0 {
		t.Errorf("изменение без адресата дало %d писем", len(got))
	}
}
