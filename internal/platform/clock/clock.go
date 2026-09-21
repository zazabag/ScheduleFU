// Package clock — время вуза и русские даты.
//
// Источник отдаёт время без пояса, и трактовать его нужно как московское;
// сервер же может стоять где угодно. Всё, что печатает или сравнивает время,
// берёт пояс отсюда, а не из time.Local.
package clock

import (
	"strconv"
	"time"
)

// Clock — часы вуза.
type Clock struct {
	loc *time.Location
	now func() time.Time
}

// New создаёт часы в указанном поясе. Пустое имя — Europe/Moscow.
func New(tz string) (*Clock, error) {
	if tz == "" {
		tz = "Europe/Moscow"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		// Системы без базы поясов встречаются в контейнерах: фиксированный
		// UTC+3 лучше, чем упасть на старте.
		loc = time.FixedZone("MSK", 3*60*60)
	}
	return &Clock{loc: loc, now: time.Now}, nil
}

// Fixed — часы, остановленные на момент t; для тестов.
func Fixed(t time.Time) *Clock {
	return &Clock{loc: t.Location(), now: func() time.Time { return t }}
}

// Now — текущее время в поясе вуза.
func (c *Clock) Now() time.Time { return c.now().In(c.loc) }

// Location — пояс вуза.
func (c *Clock) Location() *time.Location { return c.loc }

// Today — начало сегодняшнего дня.
func (c *Clock) Today() time.Time {
	n := c.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, c.loc)
}

// HHMM — текущее время как «ЧЧ:ММ». Строковое сравнение таких значений
// упорядочено так же, как хронологическое, — этим пользуется сетка пар.
func (c *Clock) HHMM() string { return c.Now().Format("15:04") }

// Русские названия — своей таблицей: раскладка time.Format понимает только
// английские, строка «2 января» печатает слово «января» буквально, и любая
// дата выглядела бы январской.
var months = [...]string{
	"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}
var weekdays = [...]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
var weekdaysShort = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

// DateRu — «14 сентября».
func DateRu(t time.Time) string { return strconv.Itoa(t.Day()) + " " + months[int(t.Month())] }

// DateTimeRu — «14 сентября в 15:04».
func DateTimeRu(t time.Time) string { return DateRu(t) + " в " + t.Format("15:04") }

// WeekdayRu — «понедельник».
func WeekdayRu(t time.Time) string { return weekdays[int(t.Weekday())] }

// WeekdayShortRu — «пн».
func WeekdayShortRu(t time.Time) string { return weekdaysShort[int(t.Weekday())] }

// StartOfWeek — понедельник недели, в которой лежит t.
func StartOfWeek(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7
	d := t.AddDate(0, 0, -offset)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}
