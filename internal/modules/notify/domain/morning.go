package domain

import (
	"sort"
	"strconv"
	"strings"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// BuildMorning — утренняя сводка одного расписания: сколько пар, где и во
// сколько первая, до скольки последняя. place — короткая подпись корпуса
// по адресу; её знает адаптер источника, а не notify. Пар нет —
// сводки нет: сообщение «сегодня пар нет» каждое воскресенье никому не
// нужно.
func BuildMorning(lessons []sched.Lesson, place func(string) string, url string) (Notification, bool) {
	if len(lessons) == 0 {
		return Notification{}, false
	}
	ls := append([]sched.Lesson(nil), lessons...)
	sort.Slice(ls, func(i, j int) bool { return ls[i].BeginsAt < ls[j].BeginsAt })
	// Пары одного времени — подгруппы и потоки — одна пара.
	slots := map[string]bool{}
	last := ""
	for _, l := range ls {
		slots[l.BeginsAt] = true
		if l.EndsAt > last {
			last = l.EndsAt
		}
	}
	first := ls[0]
	var where []string
	if room := roomNumber(first.Auditorium); room != "" {
		where = append(where, room)
	}
	if p := place(first.Building); p != "" {
		where = append(where, p)
	}
	body := "Первая в " + first.BeginsAt + " — " + first.Discipline
	if len(where) > 0 {
		body += ", " + strings.Join(where, " · ")
	}
	if len(slots) > 1 {
		body += ". Последняя до " + last + "."
	}
	n := len(slots)
	return Notification{
		Title: "Сегодня " + strconv.Itoa(n) + " " + pairsWord(n),
		Body:  body,
		URL:   url,
		Tag:   "morning-" + first.Date.Format("2006-01-02"),
	}, true
}

// pairsWord — «пара», «пары», «пар» по числу.
func pairsWord(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return "пара"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return "пары"
	}
	return "пар"
}

// roomNumber — номер из имени аудитории источника: «ЛП51_1/0412» → «0412».
func roomNumber(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Today — полночь сегодняшнего дня в поясе вуза.
func Today(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
