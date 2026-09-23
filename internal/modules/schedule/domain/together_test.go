package domain

import "testing"

func TestObshcheeOknoTolkoKogdaSvobodnyVse(t *testing.T) {
	a := []Lesson{lesson("08:30", "10:00", "А"), lesson("14:00", "15:30", "А")}
	b := []Lesson{lesson("10:10", "11:40", "Б"), lesson("14:00", "15:30", "Б")}
	cells := CommonDay([][]Lesson{a, b})
	if cells[0].Free() || len(cells[0].Busy) != 1 || cells[0].Busy[0] != 0 {
		t.Errorf("8:30 занят первый: %+v", cells[0])
	}
	if !cells[2].Free() || !cells[2].AllIn || cells[2].Inside != 2 {
		t.Errorf("11:50 свободны оба и у обоих это окно: %+v", cells[2])
	}
	if cells[5].Inside != 0 {
		t.Error("17:20 уже после пар — не окно")
	}
}

func TestVyhodnoyOdnogoNeVstrecha(t *testing.T) {
	a := []Lesson{lesson("08:30", "10:00", "А")}
	cells := CommonDay([][]Lesson{a, nil})
	if cells[3].AllIn {
		t.Error("у второго пар нет — в вузе не все")
	}
	if got := BestCommon([][]CommonCell{cells}, 2, 3); len(got) != 0 {
		t.Errorf("ехать ради встречи никто не будет, а предложено %v", got)
	}
}

func TestLuchsheeVremyaSnachalaOknoUVseh(t *testing.T) {
	a := []Lesson{lesson("08:30", "10:00", "А"), lesson("15:40", "17:10", "А")}
	b := []Lesson{lesson("08:30", "10:00", "Б"), lesson("15:40", "17:10", "Б")}
	week := [][]CommonCell{CommonDay([][]Lesson{a, b})}
	best := BestCommon(week, 2, 5)
	if len(best) == 0 || best[0].From != "10:10" || best[0].To != "15:30" || !best[0].AllInside || best[0].Pairs != 3 {
		t.Fatalf("лучшее — окно 10:10—15:30 у обоих, получили %+v", best)
	}
}
