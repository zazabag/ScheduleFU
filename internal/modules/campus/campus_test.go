package campus

import (
	"strings"
	"testing"
)

func TestKorpusaIzOSMChitayutsya(t *testing.T) {
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	p := s.Plan()
	if len(p.Buildings) < 6 {
		t.Fatalf("корпусов %d", len(p.Buildings))
	}
	if !strings.Contains(p.Source, "OpenStreetMap") {
		t.Error("ссылка на OSM обязательна по лицензии ODbL")
	}
	b, ok := s.Building("51-1")
	if !ok || b.Levels != 11 || !strings.HasPrefix(b.Path, "M") {
		t.Errorf("51 к.1: %+v", b)
	}
	// Кампус — около трёхсот метров: проекция не должна его растянуть в
	// километры или сжать в точку.
	if p.ViewBox[2] < 200 || p.ViewBox[2] > 400 {
		t.Errorf("ширина плана %d м", p.ViewBox[2])
	}
}

func TestKorpusIEtazhPoAdresuINomeru(t *testing.T) {
	s, _ := Load()
	for _, c := range []struct {
		addr, room, corp string
		level            int
	}{
		{"Ленинградский проспект, 51, корп. 1", "ЛП51_1/0412", "51-1", 4},
		{"Ленинградский проспект, 51, корп. 1", "1006", "51-1", 10},
		{"Ленинградский проспект, 49/2", "ауд.406а_ОВП", "49", 4},
		{"Ленинградский проспект, 55", "326", "55", 3},
		// Нумерация лицея не подтверждена: корпус знаем, этаж — нет.
		{"Ленинградский проспект, 51, строение 4", "Лицей_34", "51-4", 0},
	} {
		loc, ok := s.Locate(c.addr, c.room)
		if !ok || loc.BuildingID != c.corp || loc.Level != c.level {
			t.Errorf("%s: %+v", c.room, loc)
		}
	}
	if _, ok := s.Locate("ул. Кибальчича, 1", "31"); ok {
		t.Error("корпуса вне Ленинградского плана не определяются")
	}
}
