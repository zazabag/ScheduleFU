package ruz

import (
	"strings"
	"unicode"
)

// Campus — площадка вуза в том виде, в каком её воспринимает человек.
//
// Источник знает только отдельные почтовые адреса. Ленинградский проспект
// 49, 51 и 55 — это физически одно место с внутренними переходами между
// корпусами, и показывать их студенту как четыре разных здания неправильно.
// Склейка делается здесь, потому что в данных источника этой связи нет.
type Campus string

const (
	CampusLeningradsky Campus = "leningradsky"
	CampusOther        Campus = "other"
	CampusUnknown      Campus = "unknown"
)

// campusByBuilding — адреса, которые считаем одной площадкой.
var campusByBuilding = map[string]Campus{
	"Ленинградский проспект, 49/2":           CampusLeningradsky,
	"Ленинградский проспект, 51, корп. 1":    CampusLeningradsky,
	"Ленинградский проспект, 51, строение 4": CampusLeningradsky,
	"Ленинградский проспект, 55":             CampusLeningradsky,
}

// floorRule описывает, как из номера комнаты получить этаж.
// Нумерация в каждом корпусе своя, общего правила нет.
type floorRule int

const (
	// floorUnknown — правило для корпуса неизвестно. Этаж не показываем:
	// отправить человека не на тот этаж хуже, чем не показать ничего.
	floorUnknown floorRule = iota
	// floorFirstOfThree: "423" -> 4 этаж. Ленинградский 49/2.
	floorFirstOfThree
	// floorSecondOfFourLeadingZero: "0312" -> 3 этаж. Ленинградский 51 к.1.
	floorSecondOfFourLeadingZero
)

// floorRuleByPrefix привязывает правило к префиксу имени аудитории.
// Префикс устойчивее строки адреса: у одного корпуса адрес может быть
// записан по-разному, а префикс формируется системой расписания.
var floorRuleByPrefix = map[string]floorRule{
	"ЛП49/2": floorFirstOfThree,
	"ЛП51_1": floorSecondOfFourLeadingZero,
	"ЛП51_4": floorUnknown,
	"Киб1_1": floorUnknown, // "31" — то ли 3 этаж, то ли комната 31
	"Киб1_2": floorUnknown, // "1002" — этажность здания не подтверждена
	"Веш4":   floorUnknown,
}

// floorRuleByBuilding — запасное правило по адресу.
//
// Часть аудиторий приходит без корпусного префикса в имени: на
// Ленинградском 55 это просто "326" или "ауд.213". Для них правило
// выбирается по адресу.
var floorRuleByBuilding = map[string]floorRule{
	"Ленинградский проспект, 49/2":           floorFirstOfThree,
	"Ленинградский проспект, 55":             floorFirstOfThree,
	"Ленинградский проспект, 51, корп. 1":    floorSecondOfFourLeadingZero,
	"Ленинградский проспект, 51, строение 4": floorUnknown,
}

// junkRooms — значения, которыми деканат заполняет поле аудитории, когда
// места у пары нет. Это не аудитории и в выдачу попадать не должны.
var junkRooms = map[string]bool{
	"без аудитории": true,
	"д.а.":          true,
	"тест":          true,
	"":              true,
}

// junkBuildings — «адреса», не являющиеся физическими помещениями.
var junkBuildings = map[string]bool{
	"виртуальное":             true,
	"практическая подготовка": true,
	"тест": true,
}

// Auditorium — разобранное представление аудитории.
type Auditorium struct {
	Oid      int64
	Name     string // исходное имя целиком: "ЛП49/2/313"
	Prefix   string // корпусной префикс: "ЛП49/2"
	Room     string // номер или название: "313", "Коворкинг № 1 (мал)"
	Building string // адрес из источника
	Campus   Campus
	Kind     string // тип из поиска: "Лекционная", "Коворкинг", ...
	Capacity int    // 0 — неизвестна

	// Floor — этаж. nil, если правило для корпуса неизвестно или номер
	// нечисловой. Осознанно указатель: ноль здесь означал бы первый этаж.
	Floor *int
}

// ParseAuditorium разбирает имя аудитории и адрес в структуру.
func ParseAuditorium(name, building string) Auditorium {
	a := Auditorium{Name: strings.TrimSpace(name), Building: strings.TrimSpace(building)}

	// Имя имеет вид "ПРЕФИКС/НОМЕР", но у номера тоже может быть слэш
	// ("ЛП49/2/313"), поэтому отделяем только последний сегмент.
	if i := strings.LastIndex(a.Name, "/"); i >= 0 {
		a.Prefix = a.Name[:i]
		a.Room = strings.TrimSpace(a.Name[i+1:])
	} else {
		a.Room = a.Name
	}

	a.Campus = CampusUnknown
	if a.Building != "" {
		if c, ok := campusByBuilding[a.Building]; ok {
			a.Campus = c
		} else {
			a.Campus = CampusOther
		}
	}
	a.Floor = parseFloor(a.Prefix, a.Building, a.Room)
	return a
}

// parseFloor применяет правило корпуса к номеру комнаты. Правило ищется
// сначала по префиксу имени, затем по адресу.
func parseFloor(prefix, building, room string) *int {
	rule, ok := floorRuleByPrefix[prefix]
	if !ok {
		rule, ok = floorRuleByBuilding[building]
	}
	if !ok || rule == floorUnknown {
		return nil
	}
	digits := leadingDigits(room)
	switch rule {
	case floorFirstOfThree:
		if len(digits) == 3 {
			return intPtr(int(digits[0] - '0'))
		}
	case floorSecondOfFourLeadingZero:
		if len(digits) == 4 && digits[0] == '0' {
			return intPtr(int(digits[1] - '0'))
		}
	}
	return nil
}

// leadingDigits возвращает ведущие цифры номера, пропуская типовые
// приставки вроде "ауд.".
func leadingDigits(room string) string {
	s := strings.TrimSpace(room)
	s = strings.TrimPrefix(s, "ауд.")
	s = strings.TrimPrefix(s, "!")
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if !unicode.IsDigit(r) {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// IsReal сообщает, что запись соответствует настоящему помещению, а не
// заглушке деканата («Без аудитории») и не виртуальному формату.
func (a Auditorium) IsReal() bool {
	if junkRooms[strings.ToLower(strings.TrimSpace(a.Room))] {
		return false
	}
	return !junkBuildings[strings.ToLower(strings.TrimSpace(a.Building))]
}

// IsStudySpace сообщает, что в аудитории имеет смысл искать свободное место
// для занятий. Отсекает спортивные залы и помещения, вузу не принадлежащие.
func (a Auditorium) IsStudySpace() bool {
	if !a.IsReal() {
		return false
	}
	k := strings.ToLower(a.Kind)
	switch {
	case strings.Contains(k, "не принадлежащее университету"),
		strings.Contains(k, "зал аэробики"),
		strings.Contains(k, "тренажерный"),
		strings.Contains(k, "спортивный"),
		strings.Contains(k, "online"):
		return false
	}
	return true
}

func intPtr(v int) *int { return &v }
