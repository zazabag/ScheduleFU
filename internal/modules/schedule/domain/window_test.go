package domain

import "testing"

func ip(n int) *int { return &n }

func TestSvobodnaVsyoOknoANeTolkoVNachale(t *testing.T) {
	ls := []Lesson{lesson("12:30", "13:20", "Консультация")}
	if FreeThrough(ls, "11:40", "14:00") {
		t.Error("пара посреди окна: аудитория не свободна всё окно")
	}
	if !FreeThrough(ls, "13:20", "14:00") {
		t.Error("пара кончилась ровно к началу окна — свободна")
	}
	if !FreeThrough(nil, "11:40", "14:00") {
		t.Error("без пар свободна")
	}
}

func TestRyadomSnachalaTotZheEtazhPotomSosednie(t *testing.T) {
	anchor := Auditorium{Oid: 1, Building: "ЛП49", Floor: ip(4)}
	same := Auditorium{Oid: 2, Building: "ЛП49", Floor: ip(4)}
	up := Auditorium{Oid: 3, Building: "ЛП49", Floor: ip(5)}
	far := Auditorium{Oid: 4, Building: "ЛП49", Floor: ip(1)}
	unknown := Auditorium{Oid: 5, Building: "ЛП49"}
	other := Auditorium{Oid: 6, Building: "ЛП51", Floor: ip(4)}
	order := []Auditorium{same, up, far, unknown, other}
	for i := 1; i < len(order); i++ {
		if Nearness(order[i-1], anchor) >= Nearness(order[i], anchor) {
			t.Errorf("%d должна быть ближе %d", order[i-1].Oid, order[i].Oid)
		}
	}
}

func TestOknoMezhduParami(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{"10:00", "10:10", false}, // обычная перемена
		{"13:20", "14:00", false}, // большой перерыв — не окно, его и так все знают
		{"11:40", "14:00", true},  // пропала пара
		{"10:00", "15:40", true},
	}
	for _, c := range cases {
		if got := IsWindow(c.from, c.to); got != c.want {
			t.Errorf("%s—%s: окно=%v, ожидалось %v", c.from, c.to, got, c.want)
		}
	}
}
