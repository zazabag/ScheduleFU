package domain

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Reminder — напоминание одному устройству о заданиях к завтрашним парам.
type Reminder struct {
	OwnerKey string
	Title    string
	Body     string
}

// reminderBody — сколько символов тела уведомления показывают телефоны;
// длиннее — обрежут сами и посреди слова.
const reminderBody = 170

// DueOn сообщает, относится ли задание ко дню: срок стоит датой и она
// совпадает — да; срока датой нет, а в этот день пара предмета — тоже да,
// «к следующему занятию» и значит «к ближайшей паре».
func (h Homework) DueOn(day time.Time, hasLesson bool) bool {
	if h.DueDate != nil {
		return h.DueDate.Format("2006-01-02") == day.Format("2006-01-02")
	}
	return hasLesson
}

// BuildReminders группирует задания по устройствам: одно письмо на
// устройство, в нём — все предметы. По уведомлению на задание телефон
// превратился бы в пулемёт, и уведомления выключили бы в тот же день.
func BuildReminders(hws []Homework) []Reminder {
	by := map[string][]Homework{}
	var owners []string
	for _, h := range hws {
		if _, ok := by[h.OwnerKey]; !ok {
			owners = append(owners, h.OwnerKey)
		}
		by[h.OwnerKey] = append(by[h.OwnerKey], h)
	}
	sort.Strings(owners)
	out := make([]Reminder, 0, len(owners))
	for _, o := range owners {
		list := by[o]
		sort.SliceStable(list, func(i, j int) bool { return list[i].Lesson.Discipline < list[j].Lesson.Discipline })
		disciplines := map[string]bool{}
		var parts []string
		for _, h := range list {
			disciplines[h.Lesson.Discipline] = true
			parts = append(parts, h.Lesson.Discipline+" — "+strings.TrimSpace(h.Body))
		}
		title := "Завтра: не сделано задание к паре «" + list[0].Lesson.Discipline + "»"
		if len(list) > 1 {
			title = "Завтра: не сделано " + strconv.Itoa(len(list)) + " " + plural(len(list), "задание", "задания", "заданий") +
				" к " + strconv.Itoa(len(disciplines)) + " " + plural(len(disciplines), "паре", "парам", "парам")
		}
		out = append(out, Reminder{OwnerKey: o, Title: title, Body: clip(strings.Join(parts, "; "), reminderBody)})
	}
	return out
}

func plural(n int, one, few, many string) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		return few
	}
	return many
}

// clip обрезает по слову и ставит многоточие.
func clip(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := max - 1
	for cut > max/2 && r[cut] != ' ' {
		cut--
	}
	return strings.TrimRight(string(r[:cut]), " ;,—") + "…"
}
