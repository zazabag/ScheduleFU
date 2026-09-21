package ruz

import "testing"

func TestSiteOfRazdelyaetAdresa(t *testing.T) {
	// Главная проверка: прежде все эти адреса попадали в одну площадку
	// «прочие», и фильтр по ней показывал их вперемешку.
	distinct := map[string]string{
		"ул. Верхняя Масловка, д. 15":                  "maslovka",
		"4-й Вешняковский проезд, 4":                   "veshnyakovsky",
		"ул. Щербаковская, 38":                         "shcherbakovskaya",
		"ул. Олеко Дундича, 23":                        "dundicha",
		"Малый Златоустинский переулок, 7, строение 1": "zlatoustinsky",
		"ул. Кибальчича, 1, строение 1":                "kibalchicha",
	}
	seen := map[string]bool{}
	for building, want := range distinct {
		got := SiteOf(building)
		if got.Slug != want {
			t.Errorf("SiteOf(%q) = %q, ожидалось %q", building, got.Slug, want)
		}
		if seen[got.Slug] {
			t.Errorf("площадка %q повторяется — адреса снова слиплись", got.Slug)
		}
		seen[got.Slug] = true
	}
}

func TestSiteOfSkleivaetLeningradsky(t *testing.T) {
	// Обратный случай: четыре адреса — одно физическое место.
	for _, b := range []string{
		"Ленинградский проспект, 49/2",
		"Ленинградский проспект, 51, корп. 1",
		"Ленинградский проспект, 51, строение 4",
		"Ленинградский проспект, 55",
	} {
		if got := SiteOf(b); got.Slug != "leningradsky" {
			t.Errorf("SiteOf(%q) = %q, ожидалось leningradsky", b, got.Slug)
		}
	}
}

func TestSiteOfOtdelyaetFilialy(t *testing.T) {
	if got := SiteOf("Филиалы Омский филиал"); got.Slug != "branch" {
		t.Errorf("филиал получил площадку %q", got.Slug)
	}
	if got := SiteOf(""); got.Slug != SiteUnknown.Slug {
		t.Errorf("пустой адрес получил площадку %q", got.Slug)
	}
}
