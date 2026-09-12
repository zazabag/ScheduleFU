package web

import "time"

// Русские названия месяцев и дней недели.
//
// Отдельная таблица нужна потому, что раскладка time.Format понимает только
// английские названия: строка "2 января" не подставляет месяц, а печатает
// слово «января» буквально — любая дата выглядела бы январской.
var monthsRu = [...]string{
	"", "января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря",
}

var weekdaysRu = [...]string{
	"воскресенье", "понедельник", "вторник", "среда",
	"четверг", "пятница", "суббота",
}

var weekdaysShortRu = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

// FormatDateRu — «14 сентября».
func FormatDateRu(t time.Time) string {
	return itoa(t.Day()) + " " + monthsRu[int(t.Month())]
}

// FormatDateTimeRu — «14 сентября в 15:04».
func FormatDateTimeRu(t time.Time) string {
	return FormatDateRu(t) + " в " + t.Format("15:04")
}

// WeekdayRu — «понедельник».
func WeekdayRu(t time.Time) string { return weekdaysRu[int(t.Weekday())] }

// WeekdayShortRu — «пн».
func WeekdayShortRu(t time.Time) string { return weekdaysShortRu[int(t.Weekday())] }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [3]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
