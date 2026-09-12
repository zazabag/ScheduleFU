// Package web — веб-интерфейс ScheduleFU.
package web

import (
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

// Slot — пара в сетке расписания вуза.
type Slot struct {
	Begins string
	Ends   string
}

// Slots — реальная сетка занятий, снятая с данных источника, а не
// придуманная: 08:30—22:00, восемь пар, включая вечерние (на 18:55 и 20:30
// приходится больше двух тысяч занятий в неделю — заочники и вечерники,
// и без этих слотов картина дня была бы обрезана).
var Slots = []Slot{
	{"08:30", "10:00"},
	{"10:10", "11:40"},
	{"11:50", "13:20"},
	{"14:00", "15:30"},
	{"15:40", "17:10"},
	{"17:20", "18:50"},
	{"18:55", "20:25"},
	{"20:30", "22:00"},
}

// SlotState — состояние клетки полосы.
type SlotState string

const (
	SlotFree SlotState = "free" // свободна и ещё впереди
	SlotBusy SlotState = "busy" // занята парой
	SlotPast SlotState = "past" // время прошло
)

// SlotCell — одна клетка полосы занятости.
type SlotCell struct {
	Slot  Slot
	State SlotState
	Label string // время начала пары, подписанное под клеткой
	Title string // всплывающая подсказка: чем занято
}

// RoomView — аудитория, подготовленная к отрисовке.
type RoomView struct {
	store.RoomDay
	Cells []SlotCell
	// FreeNow сообщает, свободна ли аудитория в запрошенный момент.
	FreeNow bool
	// FreeUntil — время начала ближайшей пары; пусто, если до конца дня
	// занятий больше нет.
	FreeUntil string
}

// BuildRoomView раскладывает пары аудитории по сетке.
//
// Занятость клетки считается по ПЕРЕСЕЧЕНИЮ интервалов, а не по совпадению
// времени начала: около двух процентов занятий идут вне сетки (встречаются
// пары 08:30—16:45 и даже 08:00—22:00), и сравнение по началу пропустило бы
// их, показав занятую аудиторию свободной.
func BuildRoomView(day store.RoomDay, now string) RoomView {
	v := RoomView{RoomDay: day, Cells: make([]SlotCell, 0, len(Slots))}

	for _, s := range Slots {
		cell := SlotCell{Slot: s, Label: trimLeadingZero(s.Begins)}

		for _, l := range day.Lessons {
			if overlaps(s.Begins, s.Ends, l.BeginsAt, l.EndsAt) {
				cell.State = SlotBusy
				cell.Title = l.BeginsAt + "—" + l.EndsAt + " · " + l.Discipline
				if l.LecturerName != "" {
					cell.Title += " · " + l.LecturerName
				}
				break
			}
		}
		if cell.State == "" {
			if s.Ends <= now {
				cell.State = SlotPast
			} else {
				cell.State = SlotFree
			}
		}
		v.Cells = append(v.Cells, cell)
	}

	v.FreeNow = true
	for _, l := range day.Lessons {
		if l.BeginsAt <= now && now < l.EndsAt {
			v.FreeNow = false
			break
		}
	}
	for _, l := range day.Lessons {
		if l.BeginsAt > now {
			v.FreeUntil = l.BeginsAt
			break
		}
	}
	return v
}

// overlaps сообщает, пересекаются ли два интервала времени «ЧЧ:ММ».
// Строковое сравнение здесь корректно: формат с ведущими нулями
// упорядочен лексикографически так же, как хронологически.
func overlaps(aFrom, aTo, bFrom, bTo string) bool {
	return aFrom < bTo && bFrom < aTo
}

// trimLeadingZero сокращает «08:30» до «8:30»: в подписи под узкой клеткой
// каждый символ на счету.
func trimLeadingZero(t string) string {
	return strings.TrimPrefix(t, "0")
}

// NowClock возвращает текущее время вуза в формате «ЧЧ:ММ».
func NowClock(loc *time.Location) string {
	return time.Now().In(loc).Format("15:04")
}
