package domain

import "testing"

func TestDenPrepodavatelyaSOknami(t *testing.T) {
	d := BuildDaySpan([]Lesson{
		lesson("14:00", "15:30", "c"),
		lesson("08:30", "10:00", "a"),
		lesson("08:30", "10:00", "a2"), // две подгруппы в один слот — одна пара
		lesson("10:10", "11:40", "b"),
	})
	if d.From != "08:30" || d.To != "15:30" || d.Pairs != 3 {
		t.Errorf("день: %+v", d)
	}
	if len(d.Windows) != 1 || d.Windows[0].Begins != "11:40" || d.Windows[0].Ends != "14:00" {
		t.Errorf("окна: %+v", d.Windows)
	}
}

func TestPustoyDen(t *testing.T) {
	if d := BuildDaySpan(nil); d.Pairs != 0 || d.From != "" {
		t.Errorf("пустой день: %+v", d)
	}
}
