package push

import (
	"strings"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

var msk = time.FixedZone("MSK", 3*60*60)

func change(kind store.ChangeKind, day int, groups []string, lecturer int64) store.Change {
	c := store.Change{
		Kind:       kind,
		LessonDate: time.Date(2026, 9, day, 0, 0, 0, 0, msk),
		GroupNames: groups,
	}
	if lecturer > 0 {
		c.LecturerOid = &lecturer
	}
	return c
}

// TestBuildGruppiruetVOdnoSoobshchenie — ради этого всё и затевалось: за
// проход сборщик находит сотни правок, и по уведомлению на каждую телефон
// превратился бы в пулемёт.
func TestBuildGruppiruetVOdnoSoobshchenie(t *testing.T) {
	var changes []store.Change
	for i := 0; i < 40; i++ {
		changes = append(changes, change(store.ChangeAdded, 14, []string{"ПИ24-1"}, 0))
	}
	got := Build(changes, msk)

	if len(got) != 1 {
		t.Fatalf("получилось %d уведомлений, ожидалось одно", len(got))
	}
	n := got["group:ПИ24-1"]
	if !strings.Contains(n.Body, "40") {
		t.Errorf("в тексте нет числа изменений: %q", n.Body)
	}
	if n.Tag == "" {
		t.Error("без тега уведомления будут копиться стопкой")
	}
}

func TestBuildRazdelyaetGruppuIPrepodavatelya(t *testing.T) {
	changes := []store.Change{
		change(store.ChangeChanged, 14, []string{"ПИ24-1", "ПИ24-2"}, 46674),
	}
	got := Build(changes, msk)

	for _, key := range []string{"group:ПИ24-1", "group:ПИ24-2", "lecturer:46674"} {
		if _, ok := got[key]; !ok {
			t.Errorf("нет уведомления для %q", key)
		}
	}
	if len(got) != 3 {
		t.Fatalf("адресатов %d, ожидалось 3", len(got))
	}
}

func TestBuildSchitaetVidyOtdelno(t *testing.T) {
	changes := []store.Change{
		change(store.ChangeAdded, 14, []string{"Ю24-5"}, 0),
		change(store.ChangeRemoved, 14, []string{"Ю24-5"}, 0),
		change(store.ChangeChanged, 14, []string{"Ю24-5"}, 0),
	}
	body := Build(changes, msk)["group:Ю24-5"].Body
	for _, want := range []string{"добавлена", "изменена", "отменена"} {
		if !strings.Contains(body, want) {
			t.Errorf("в тексте %q нет слова %q", body, want)
		}
	}
	if !strings.Contains(body, "14 сентября") {
		t.Errorf("не указан день: %q", body)
	}
}

func TestBuildNeskolkoDney(t *testing.T) {
	changes := []store.Change{
		change(store.ChangeAdded, 14, []string{"Ю24-5"}, 0),
		change(store.ChangeAdded, 15, []string{"Ю24-5"}, 0),
	}
	body := Build(changes, msk)["group:Ю24-5"].Body
	if !strings.Contains(body, "2 дн") {
		t.Errorf("не указано число дней: %q", body)
	}
}

func TestPluralRusskieChislitelnye(t *testing.T) {
	cases := map[int]string{
		1:   "добавлена пара",
		2:   "добавлены 2 пары",
		4:   "добавлены 4 пары",
		5:   "добавлено 5 пар",
		11:  "добавлено 11 пар",
		14:  "добавлено 14 пар",
		21:  "добавлена 21 пара",
		22:  "добавлены 22 пары",
		25:  "добавлено 25 пар",
		40:  "добавлено 40 пар",
		101: "добавлена 101 пара",
	}
	for n, want := range cases {
		got := plural(n, "добавлена пара", "добавлены %d пары", "добавлено %d пар")
		if got != want {
			t.Errorf("plural(%d) = %q, ожидалось %q", n, got, want)
		}
	}
}

func TestBuildPustyeIzmeneniya(t *testing.T) {
	if got := Build(nil, msk); len(got) != 0 {
		t.Errorf("без изменений не должно быть уведомлений, получено %d", len(got))
	}
	// Пара без групп и без преподавателя адресовать некому — это не ошибка,
	// а обычное дело: у языковых занятий состав групп пустой.
	if got := Build([]store.Change{change(store.ChangeAdded, 14, nil, 0)}, msk); len(got) != 0 {
		t.Errorf("изменение без адресата дало %d уведомлений", len(got))
	}
}
