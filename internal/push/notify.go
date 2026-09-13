// Package push — уведомления об изменениях расписания.
package push

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

// Notification — то, что увидит человек.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	// Tag позволяет системе заменить предыдущее уведомление новым, а не
	// копить их стопкой на экране блокировки.
	Tag string `json:"tag"`
}

// Build превращает изменения расписания в уведомления по адресатам.
//
// Главное здесь — группировка. За один проход сборщик находит сотни правок
// (в реальной выгрузке было 734 добавления и 49 удалений разом): если
// отправлять по уведомлению на изменение, телефон превратится в пулемёт, и
// уведомления выключат в тот же день. Поэтому на каждое отслеживаемое
// расписание уходит ровно одно сообщение с итогом.
//
// Ключ адресата — тот же, что у закрепления: «group:ПИ24-1» или
// «lecturer:46674».
func Build(changes []store.Change, loc *time.Location) map[string]Notification {
	type tally struct {
		added, removed, changed int
		dates                   map[string]bool
	}
	bySubject := map[string]*tally{}

	note := func(key string, c store.Change) {
		t, ok := bySubject[key]
		if !ok {
			t = &tally{dates: map[string]bool{}}
			bySubject[key] = t
		}
		switch c.Kind {
		case store.ChangeAdded:
			t.added++
		case store.ChangeRemoved:
			t.removed++
		case store.ChangeChanged:
			t.changed++
		}
		t.dates[c.LessonDate.In(loc).Format("2006-01-02")] = true
	}

	for _, c := range changes {
		for _, g := range c.GroupNames {
			if g = strings.TrimSpace(g); g != "" {
				note("group:"+g, c)
			}
		}
		if c.LecturerOid != nil && *c.LecturerOid > 0 {
			note(fmt.Sprintf("lecturer:%d", *c.LecturerOid), c)
		}
	}

	out := make(map[string]Notification, len(bySubject))
	for key, t := range bySubject {
		out[key] = Notification{
			Title: "Расписание изменилось",
			Body:  describe(t.added, t.removed, t.changed, sortedDates(t.dates), loc),
			URL:   "./#/schedule",
			// Один тег на адресата: новое уведомление вытесняет прежнее,
			// вместо того чтобы висеть рядом с ним.
			Tag: "schedule-" + key,
		}
	}
	return out
}

func sortedDates(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// describe составляет человеческую фразу вместо перечисления цифр.
func describe(added, removed, changed int, dates []string, loc *time.Location) string {
	var parts []string
	if added > 0 {
		parts = append(parts, plural(added, "добавлена пара", "добавлены %d пары", "добавлено %d пар"))
	}
	if changed > 0 {
		parts = append(parts, plural(changed, "изменена пара", "изменены %d пары", "изменено %d пар"))
	}
	if removed > 0 {
		parts = append(parts, plural(removed, "отменена пара", "отменены %d пары", "отменено %d пар"))
	}
	if len(parts) == 0 {
		return "Проверьте расписание"
	}

	body := strings.Join(parts, ", ")
	if len(dates) == 1 {
		if d, err := time.ParseInLocation("2006-01-02", dates[0], loc); err == nil {
			return body + " — " + formatDay(d)
		}
	}
	if len(dates) > 1 {
		return body + fmt.Sprintf(" — на %d дн.", len(dates))
	}
	return body
}

var monthsRu = [...]string{
	"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

func formatDay(t time.Time) string {
	return fmt.Sprintf("%d %s", t.Day(), monthsRu[int(t.Month())])
}

// plural выбирает форму числительного: «1 пара», «2 пары», «5 пар».
func plural(n int, one, few, many string) string {
	mod100 := n % 100
	mod10 := n % 10
	switch {
	case n == 1:
		return one
	case mod100 >= 11 && mod100 <= 14:
		return fmt.Sprintf(many, n)
	case mod10 >= 2 && mod10 <= 4:
		return fmt.Sprintf(few, n)
	case mod10 == 1:
		// «21 пара» — форма единственного числа, но с числом.
		return fmt.Sprintf(strings.Replace(one, "пара", "%d пара", 1), n)
	default:
		return fmt.Sprintf(many, n)
	}
}
