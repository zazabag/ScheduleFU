package domain

import "strings"

// Slot — пара в сетке занятий.
type Slot struct{ Begins, Ends string }

// Slots — сетка, снятая с данных источника, а не придуманная: восемь пар с
// 08:30 до 22:00. Вечерние 18:55 и 20:30 обязательны — на них приходится
// больше двух тысяч занятий в неделю, и без них день выглядел бы обрезанным.
var Slots = []Slot{
	{"08:30", "10:00"}, {"10:10", "11:40"}, {"11:50", "13:20"}, {"14:00", "15:30"},
	{"15:40", "17:10"}, {"17:20", "18:50"}, {"18:55", "20:25"}, {"20:30", "22:00"},
}

// SlotState — состояние клетки полосы занятости.
type SlotState string

const (
	SlotFree SlotState = "free"
	SlotBusy SlotState = "busy"
	SlotPast SlotState = "past"
)

// SlotCell — клетка полосы.
type SlotCell struct {
	Slot  Slot
	State SlotState
	Label string // время начала без ведущего нуля: под узкой клеткой каждый символ на счету
	Title string // чем занято
}

// RoomView — аудитория с полосой занятости на день и ответом «свободна ли
// сейчас».
type RoomView struct {
	Auditorium Auditorium
	Lessons    []Lesson
	Cells      []SlotCell
	FreeNow    bool
	FreeUntil  string // начало ближайшей пары; пусто — до конца дня
}

// BuildRoomView раскладывает пары по сетке на момент now (ЧЧ:ММ).
//
// Занятость клетки считается по ПЕРЕСЕЧЕНИЮ интервалов: около двух процентов
// занятий идут вне сетки, вплоть до 08:00—22:00, и сравнение по времени
// начала показало бы занятую аудиторию свободной весь день.
func BuildRoomView(a Auditorium, lessons []Lesson, now string) RoomView {
	v := RoomView{Auditorium: a, Lessons: lessons, Cells: make([]SlotCell, 0, len(Slots)), FreeNow: true}
	for _, s := range Slots {
		cell := SlotCell{Slot: s, Label: strings.TrimPrefix(s.Begins, "0"), State: SlotFree}
		for _, l := range lessons {
			if l.Overlaps(s.Begins, s.Ends) {
				cell.State = SlotBusy
				cell.Title = l.BeginsAt + "—" + l.EndsAt + " · " + l.Discipline
				if l.LecturerName != "" {
					cell.Title += " · " + l.LecturerName
				}
				break
			}
		}
		if cell.State == SlotFree && s.Ends <= now {
			cell.State = SlotPast
		}
		v.Cells = append(v.Cells, cell)
	}
	for _, l := range lessons {
		if l.Covers(now) {
			v.FreeNow = false
		}
		if l.BeginsAt > now && (v.FreeUntil == "" || l.BeginsAt < v.FreeUntil) {
			v.FreeUntil = l.BeginsAt
		}
	}
	return v
}
