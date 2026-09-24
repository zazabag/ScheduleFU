package domain

import (
	"strconv"
	"time"
)

// Tally — итог семестра одного расписания: сколько пар и часов, где и
// чего больше всего. Копится по дню и хранится только суммами — сырых пар
// за прошлое мы не держим (ARCHITECTURE.md § 5): по итогу нельзя
// восстановить, что было в какой день, и архивом базы вуза он не становится.
type Tally struct {
	Lessons     int
	Minutes     int
	Days        int
	Buildings   map[string]int // адрес → пар
	Rooms       map[string]int // аудитория → пар
	Disciplines map[string]int // дисциплина → минут
}

// Attributed — пара, отнесённая к расписанию: группе (с языковыми
// подгруппами по связям) или преподавателю.
type Attributed struct {
	SubjectKey string
	Lesson     Lesson
}

// SemesterOf — семестр даты: осенний — с сентября по январь, весенний — с
// февраля по август; год — год начала учебного года. «2026-1», «2026-2».
func SemesterOf(t time.Time) string {
	y := t.Year()
	switch m := t.Month(); {
	case m >= time.September:
		return strconv.Itoa(y) + "-1"
	case m == time.January:
		return strconv.Itoa(y-1) + "-1"
	default:
		return strconv.Itoa(y-1) + "-2"
	}
}

// TallyDay — итог одного дня по расписаниям. Пары одного времени у одного
// расписания — подгруппы или поток в нескольких аудиториях — одна пара:
// человек один, и час один.
func TallyDay(items []Attributed) map[string]Tally {
	out := map[string]Tally{}
	seen := map[string]bool{}
	for _, it := range items {
		l := it.Lesson
		t, ok := out[it.SubjectKey]
		if !ok {
			t = Tally{Days: 1, Buildings: map[string]int{}, Rooms: map[string]int{}, Disciplines: map[string]int{}}
		}
		slot := it.SubjectKey + "\x1f" + l.BeginsAt
		if !seen[slot] {
			seen[slot] = true
			mins := Minutes(l.EndsAt) - Minutes(l.BeginsAt)
			if mins < 0 {
				mins = 0
			}
			t.Lessons++
			t.Minutes += mins
			if l.Building != "" {
				t.Buildings[l.Building]++
			}
			if l.Auditorium != "" {
				t.Rooms[l.Auditorium]++
			}
			if l.Discipline != "" {
				t.Disciplines[l.Discipline] += mins
			}
		}
		out[it.SubjectKey] = t
	}
	return out
}

// Add складывает итоги.
func (t Tally) Add(o Tally) Tally {
	sum := func(a, b map[string]int) map[string]int {
		out := map[string]int{}
		for k, v := range a {
			out[k] += v
		}
		for k, v := range b {
			out[k] += v
		}
		return out
	}
	return Tally{Lessons: t.Lessons + o.Lessons, Minutes: t.Minutes + o.Minutes, Days: t.Days + o.Days,
		Buildings: sum(t.Buildings, o.Buildings), Rooms: sum(t.Rooms, o.Rooms), Disciplines: sum(t.Disciplines, o.Disciplines)}
}
