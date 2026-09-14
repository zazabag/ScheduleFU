// Package ical — выгрузка расписания в формат календарей.
//
// Формат описан в RFC 5545. Он капризен к мелочам: строки складываются по
// 75 октетов, запятые и точки с запятой экранируются, переводы строк
// заменяются. Календарь, собранный «на глаз», часть приложений открывает,
// а часть молча отвергает — поэтому мелочи соблюдаются буквально.
package ical

import (
	"fmt"
	"strings"
	"time"
)

// Event — одна пара в календаре.
type Event struct {
	UID         string
	Start       time.Time
	End         time.Time
	Summary     string
	Location    string
	Description string
}

// Calendar — календарь целиком.
type Calendar struct {
	Name        string
	Description string
	Events      []Event
	// Location — часовой пояс занятий. Время выгружается с указанием
	// пояса, а не в UTC: так календарь переживает переезд владельца в
	// другой часовой пояс и не показывает пары среди ночи.
	Location *time.Location
}

const (
	// prodID сообщает календарным приложениям, кто создал файл.
	prodID = "-//ScheduleFU//Расписание Финансового университета//RU"
	// crlf — перевод строки, требуемый форматом. Обычный \n часть
	// приложений не принимает.
	crlf = "\r\n"
)

// Render собирает текст календаря.
func (c Calendar) Render(now time.Time) string {
	loc := c.Location
	if loc == nil {
		loc = time.UTC
	}
	tz := loc.String()

	var b strings.Builder
	write := func(line string) {
		b.WriteString(fold(line))
		b.WriteString(crlf)
	}

	write("BEGIN:VCALENDAR")
	write("VERSION:2.0")
	write("PRODID:" + prodID)
	write("CALSCALE:GREGORIAN")
	write("METHOD:PUBLISH")
	if c.Name != "" {
		// X-WR-CALNAME не входит в стандарт, но именно его читают Apple,
		// Google и почти все остальные, показывая имя подписки.
		write("X-WR-CALNAME:" + escape(c.Name))
		write("NAME:" + escape(c.Name))
	}
	if c.Description != "" {
		write("X-WR-CALDESC:" + escape(c.Description))
	}
	write("X-WR-TIMEZONE:" + tz)
	// Как часто календарю стоит перечитывать подписку. Расписание вуза
	// меняется задним числом, поэтому раз в час, а не раз в сутки.
	write("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	write("X-PUBLISHED-TTL:PT1H")

	stamp := now.UTC().Format("20060102T150405Z")
	for _, e := range c.Events {
		write("BEGIN:VEVENT")
		write("UID:" + e.UID)
		write("DTSTAMP:" + stamp)
		write(fmt.Sprintf("DTSTART;TZID=%s:%s", tz, e.Start.In(loc).Format("20060102T150405")))
		write(fmt.Sprintf("DTEND;TZID=%s:%s", tz, e.End.In(loc).Format("20060102T150405")))
		write("SUMMARY:" + escape(e.Summary))
		if e.Location != "" {
			write("LOCATION:" + escape(e.Location))
		}
		if e.Description != "" {
			write("DESCRIPTION:" + escape(e.Description))
		}
		write("END:VEVENT")
	}
	write("END:VCALENDAR")
	return b.String()
}

// escape готовит текст к вставке в поле календаря.
//
// Незаэкранированная запятая превращает одно поле в список, а точка с
// запятой — в набор параметров: название вроде «Экономика, ч. 2» без этого
// ломает событие целиком.
func escape(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		";", "\\;",
		",", "\\,",
		"\n", "\\n",
		"\r", "",
	)
	return r.Replace(s)
}

// fold складывает длинную строку по правилам формата.
//
// Ограничение — 75 октетов, причём именно октетов, а не символов: русский
// текст в UTF-8 занимает по два байта на букву, и разрыв посреди буквы
// превратил бы её в мусор. Поэтому длина считается по байтам, а рвётся
// строка только на границе символа.
func fold(line string) string {
	const limit = 75
	if len(line) <= limit {
		return line
	}
	var b strings.Builder
	count := 0
	for _, r := range line {
		size := len(string(r))
		// Продолжение строки начинается с пробела и потому вмещает на
		// символ меньше.
		max := limit
		if count > 0 && b.Len() > 0 {
			max = limit
		}
		if count+size > max {
			b.WriteString(crlf + " ")
			count = 1
		}
		b.WriteString(string(r))
		count += size
	}
	return b.String()
}
