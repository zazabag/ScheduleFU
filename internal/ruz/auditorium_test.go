package ruz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseAuditorium(t *testing.T) {
	cases := []struct {
		name      string
		building  string
		wantRoom  string
		wantFloor *int // nil — этаж не определяется
		wantCamp  Campus
	}{
		// Ленинградский 49/2: три цифры, первая — этаж.
		{"ЛП49/2/313", "Ленинградский проспект, 49/2", "313", intPtr(3), CampusLeningradsky},
		{"ЛП49/2/425", "Ленинградский проспект, 49/2", "425", intPtr(4), CampusLeningradsky},

		// Ленинградский 51: четыре цифры с ведущим нулём, этаж — вторая.
		// Наивный разбор «первая цифра» дал бы нулевой этаж для всего корпуса.
		{"ЛП51_1/0312", "Ленинградский проспект, 51, корп. 1", "0312", intPtr(3), CampusLeningradsky},
		{"ЛП51_1/0519", "Ленинградский проспект, 51, корп. 1", "0519", intPtr(5), CampusLeningradsky},

		// Корпуса с неподтверждённой нумерацией: этаж не выдумываем.
		{"Киб1_2/1002", "ул. Кибальчича, 1, строение 2", "1002", nil, CampusOther},
		{"Киб1_1/31", "ул. Кибальчича, 1, строение 1", "31", nil, CampusOther},

		// Нечисловые имена — этажа нет, но это настоящие помещения.
		{"ЛП49/2/Коворкинг № 1 (мал)", "Ленинградский проспект, 49/2",
			"Коворкинг № 1 (мал)", nil, CampusLeningradsky},
		{"Большой зал", "Ленинградский проспект, 49/2", "Большой зал", nil, CampusLeningradsky},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAuditorium(c.name, c.building)
			if got.Room != c.wantRoom {
				t.Errorf("Room = %q, ожидалось %q", got.Room, c.wantRoom)
			}
			if got.Campus != c.wantCamp {
				t.Errorf("Campus = %q, ожидалось %q", got.Campus, c.wantCamp)
			}
			switch {
			case c.wantFloor == nil && got.Floor != nil:
				t.Errorf("Floor = %d, ожидалось «неизвестно»", *got.Floor)
			case c.wantFloor != nil && got.Floor == nil:
				t.Errorf("Floor = «неизвестно», ожидалось %d", *c.wantFloor)
			case c.wantFloor != nil && *got.Floor != *c.wantFloor:
				t.Errorf("Floor = %d, ожидалось %d", *got.Floor, *c.wantFloor)
			}
		})
	}
}

func TestIsRealOtsekaetZaglushki(t *testing.T) {
	junk := []struct{ name, building string }{
		{"в.з./Без аудитории", "ул. Верхняя Масловка, д. 15"},
		{"в.з./д.а.", "ул. Верхняя Масловка, д. 15"},
		{"Онлайн", "Виртуальное"},
	}
	for _, j := range junk {
		if ParseAuditorium(j.name, j.building).IsReal() {
			t.Errorf("%q ошибочно признана настоящей аудиторией", j.name)
		}
	}
	if !ParseAuditorium("ЛП49/2/313", "Ленинградский проспект, 49/2").IsReal() {
		t.Error("настоящая аудитория признана заглушкой")
	}
}

func TestIsStudySpaceOtsekaetZalyIChuzhie(t *testing.T) {
	a := ParseAuditorium("Кас15,17/Зал военной подготовки", "ул. Касаткина 15")
	a.Kind = "Спортивный зал"
	if a.IsStudySpace() {
		t.Error("спортзал не должен попадать в места для занятий")
	}
	b := ParseAuditorium("ЛП49/2/Коворкинг № 1 (мал)", "Ленинградский проспект, 49/2")
	b.Kind = "Коворкинг"
	if !b.IsStudySpace() {
		t.Error("коворкинг должен попадать в места для занятий")
	}
}

// TestNaSobrannomSpravochnike прогоняет разбор по реальной выгрузке.
// Тест не обращается к сети: он читает data/auditoriums.json, собранный
// tools/discover_auditoriums.py. Если файла нет, тест пропускается.
func TestNaSobrannomSpravochnike(t *testing.T) {
	path := filepath.Join("..", "..", "data", "auditoriums.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("нет выгрузки %s, пропускаем", path)
	}
	var dump struct {
		Auditoriums []struct {
			Oid      int64  `json:"oid"`
			Name     string `json:"name"`
			Building string `json:"building"`
			Kind     string `json:"kind"`
		} `json:"auditoriums"`
	}
	if err := json.Unmarshal(raw, &dump); err != nil {
		t.Fatalf("выгрузка не разобрана: %v", err)
	}
	if len(dump.Auditoriums) == 0 {
		t.Fatal("выгрузка пуста")
	}

	var leningradsky, withFloor, real_ int
	for _, r := range dump.Auditoriums {
		a := ParseAuditorium(r.Name, r.Building)
		a.Kind = r.Kind
		if a.Campus == CampusLeningradsky {
			leningradsky++
			if a.Floor != nil {
				withFloor++
			}
		}
		if a.IsReal() {
			real_++
		}
	}
	t.Logf("всего %d, настоящих %d, на Ленинградском %d (из них с этажом %d)",
		len(dump.Auditoriums), real_, leningradsky, withFloor)

	// Ленинградский кампус — четыре адреса, вместе крупнейшая площадка вуза.
	if leningradsky < 100 {
		t.Errorf("на Ленинградском склеилось %d аудиторий, ожидалось не меньше 100", leningradsky)
	}
	// Ради этого правила и написан разбор по корпусам: наивный вариант
	// оставлял без этажа или с нулевым весь 51-й корпус.
	if withFloor < 90 {
		t.Errorf("этаж определён лишь у %d аудиторий кампуса, ожидалось не меньше 90", withFloor)
	}
}
