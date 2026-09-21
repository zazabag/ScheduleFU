package domain

import (
	"sort"
	"strings"
)

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
	Now   bool   // пара идёт прямо сейчас: полоса подсвечивает её отдельно
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
		cell.Now = s.Begins <= now && now < s.Ends
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

// SlotStat — сколько аудиторий площадки свободно в пару.
type SlotStat struct {
	Slot  Slot
	Label string
	Free  int
	Total int
	State string // past · now · later
}

// FloorStat — сколько свободно на этаже. Floor -1 — этаж неизвестен:
// правило нумерации корпуса не подтверждено, и мы не угадываем.
type FloorStat struct {
	Floor int
	Free  int
	Total int
}

// SiteSummary — сводка площадки на момент: свободно сейчас, по парам и по
// этажам. Считается из тех же полос, что показываются в списке, поэтому
// цифры сходятся с ним всегда.
type SiteSummary struct {
	Total   int
	FreeNow int
	Slots   []SlotStat
	Floors  []FloorStat
	// BestSlot — пара с наибольшим числом свободных аудиторий из ещё не
	// прошедших; пусто, если день закончился.
	BestSlot *SlotStat
	// NextSlot — ближайшая пара после текущей: к ней меняется картина.
	NextSlot *SlotStat
}

// BuildSiteSummary складывает сводку из полос аудиторий.
func BuildSiteSummary(views []RoomView, now string) SiteSummary {
	sum := SiteSummary{Total: len(views)}
	stats := make([]SlotStat, len(Slots))
	for i, s := range Slots {
		stats[i] = SlotStat{Slot: s, Label: strings.TrimPrefix(s.Begins, "0"), Total: len(views), State: "later"}
		switch {
		case s.Ends <= now:
			stats[i].State = "past"
		case s.Begins <= now:
			stats[i].State = "now"
		}
	}
	floors := map[int]*FloorStat{}
	for _, v := range views {
		if v.FreeNow {
			sum.FreeNow++
		}
		for i, c := range v.Cells {
			if c.State != SlotBusy {
				stats[i].Free++
			}
		}
		f := -1
		if v.Auditorium.Floor != nil {
			f = *v.Auditorium.Floor
		}
		fs, ok := floors[f]
		if !ok {
			fs = &FloorStat{Floor: f}
			floors[f] = fs
		}
		fs.Total++
		if v.FreeNow {
			fs.Free++
		}
	}
	sum.Slots = stats
	for i := range stats {
		if stats[i].State == "past" {
			continue
		}
		if sum.BestSlot == nil || stats[i].Free > sum.BestSlot.Free {
			sum.BestSlot = &stats[i]
		}
		if stats[i].State == "later" && sum.NextSlot == nil {
			sum.NextSlot = &stats[i]
		}
	}
	for f := range floors {
		sum.Floors = append(sum.Floors, *floors[f])
	}
	sort.Slice(sum.Floors, func(i, j int) bool {
		// Неизвестный этаж — в конец.
		if (sum.Floors[i].Floor < 0) != (sum.Floors[j].Floor < 0) {
			return sum.Floors[j].Floor < 0
		}
		return sum.Floors[i].Floor < sum.Floors[j].Floor
	})
	return sum
}
