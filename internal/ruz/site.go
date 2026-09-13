package ruz

import "strings"

// Site — площадка: здание или группа зданий, которые человек воспринимает
// как одно место.
//
// Отдельная сущность рядом с Campus нужна потому, что Campus отвечает лишь
// на вопрос «это Ленинградский или нет», и все прочие адреса схлопывались в
// одно значение. Фильтр по нему показывал вперемешку Масловку, Вешняковский
// и ещё десять зданий.
type Site struct {
	Slug  string // ключ для ссылок и хранения
	Label string // короткая подпись
	Order int    // порядок в переключателе: чем меньше, тем левее
}

// SiteUnknown — площадка, для которой не задано правило.
var SiteUnknown = Site{Slug: "other", Label: "Прочие", Order: 99}

// sites перечисляет московские площадки вуза. Порядок задан вручную:
// сортировка по числу аудиторий скакала бы от выгрузки к выгрузке.
var sites = []struct {
	match string
	site  Site
}{
	{"Ленинградский", Site{"leningradsky", "Ленинградский", 1}},
	{"Верхняя Масловка", Site{"maslovka", "Масловка", 2}},
	{"Вешняковский", Site{"veshnyakovsky", "Вешняковский", 3}},
	{"Щербаковская", Site{"shcherbakovskaya", "Щербаковская", 4}},
	{"Олеко Дундича", Site{"dundicha", "Дундича", 5}},
	{"Златоустинский", Site{"zlatoustinsky", "Златоустинский", 6}},
	{"Кибальчича", Site{"kibalchicha", "Кибальчича", 7}},
	{"Кронштадтский", Site{"kronshtadtsky", "Кронштадтский", 8}},
	{"Мира", Site{"mira", "Проспект Мира", 9}},
	{"Касаткина", Site{"kasatkina", "Касаткина", 10}},
	{"Баумана", Site{"bauman", "МГТУ", 11}},
	{"Фили", Site{"fili", "Фили", 12}},
	{"Щепкина", Site{"schepkina", "Щепкина", 13}},
	{"Вешняковская", Site{"veshnyakovskaya", "Вешняковская", 14}},
}

// SiteOf определяет площадку по адресу из источника.
//
// Ленинградские 49, 51 и 55 намеренно попадают в одну площадку: физически
// это одно место с внутренними переходами, и разделять их в интерфейсе
// значило бы врать о географии.
func SiteOf(building string) Site {
	b := strings.TrimSpace(building)
	if b == "" {
		return SiteUnknown
	}
	if strings.HasPrefix(b, "Филиалы") {
		return Site{"branch", "Филиалы", 90}
	}
	for _, s := range sites {
		if strings.Contains(b, s.match) {
			return s.site
		}
	}
	return SiteUnknown
}

// SiteLabel — короткая подпись площадки по её адресу.
func SiteLabel(building string) string { return SiteOf(building).Label }
