// Package domain — язык модуля notify: подписка, письмо, доставка.
package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// Notification — то, что увидит человек, независимо от транспорта.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	// Tag позволяет системе заменить прежнее уведомление новым, а не копить
	// их стопкой на экране блокировки.
	Tag string `json:"tag"`
}

// Subscription — адресат одного расписания через один транспорт.
//
// Транспорт — часть ключа: одно устройство слушает группу через push, а
// человек — ту же группу в Telegram. Из данных адресата хранится только то,
// без чего доставка невозможна: никаких досье.
type Subscription struct {
	ID          int64
	SubjectKey  string
	Transport   string
	Target      string            // endpoint | chat_id | адрес
	Credentials map[string]string // ключи шифрования push и т. п.
}

// Delivery — письмо в очереди.
type Delivery struct {
	ID           int64
	Subscription Subscription
	Payload      []byte
	Attempts     int
}

// Outcome — итог попытки доставки.
type Outcome int

const (
	Delivered Outcome = iota // доставлено
	Retry                    // временная неудача, повторить позже
	Dead                     // адресат исчез: подписку удалить
	Failed                   // не доставить никогда, письмо закрыть
)

// Build превращает изменения в письма по адресатам.
//
// Главное здесь — группировка. За проход сборщик находит сотни правок (в
// живой выгрузке — 734 добавления и 49 отмен разом); по уведомлению на
// изменение телефон превратится в пулемёт, и уведомления выключат в тот же
// день. Поэтому на каждое отслеживаемое расписание — одно сообщение с итогом.
func Build(changes []sched.Change, loc *time.Location) map[string]Notification {
	type tally struct {
		added, removed, changed int
		dates                   map[string]bool
	}
	by := map[string]*tally{}
	for _, c := range changes {
		for _, s := range c.Subjects() {
			key := s.Key()
			t, ok := by[key]
			if !ok {
				t = &tally{dates: map[string]bool{}}
				by[key] = t
			}
			switch c.Kind {
			case sched.ChangeAdded:
				t.added++
			case sched.ChangeRemoved:
				t.removed++
			case sched.ChangeChanged:
				t.changed++
			}
			t.dates[c.LessonDate.In(loc).Format("2006-01-02")] = true
		}
	}
	out := make(map[string]Notification, len(by))
	for key, t := range by {
		dates := make([]string, 0, len(t.dates))
		for d := range t.dates {
			dates = append(dates, d)
		}
		sort.Strings(dates)
		out[key] = Notification{
			Title: "Расписание изменилось",
			Body:  describe(t.added, t.removed, t.changed, dates, loc),
			URL:   "/schedule",
			Tag:   "schedule-" + key,
		}
	}
	return out
}

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
	switch len(dates) {
	case 0:
		return body
	case 1:
		if d, err := time.ParseInLocation("2006-01-02", dates[0], loc); err == nil {
			return body + " — " + fmt.Sprintf("%d %s", d.Day(), months[int(d.Month())])
		}
		return body
	default:
		return body + fmt.Sprintf(" — на %d дн.", len(dates))
	}
}

var months = [...]string{"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}

// plural выбирает форму: «1 пара», «2 пары», «5 пар», «21 пара», «11 пар».
func plural(n int, one, few, many string) string {
	m100, m10 := n%100, n%10
	switch {
	case n == 1:
		return one
	case m100 >= 11 && m100 <= 14:
		return fmt.Sprintf(many, n)
	case m10 >= 2 && m10 <= 4:
		return fmt.Sprintf(few, n)
	case m10 == 1:
		return fmt.Sprintf(strings.Replace(one, "пара", "%d пара", 1), n)
	default:
		return fmt.Sprintf(many, n)
	}
}
