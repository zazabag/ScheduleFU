package domain

import "sort"

// DaySpan — день человека в вузе: с какой пары по какую, сколько пар и
// окна между ними. Отвечает на вопрос «когда застать преподавателя»: в
// окно между парами его проще всего поймать за подписью или вопросом.
type DaySpan struct {
	From, To string
	Pairs    int
	Windows  []Slot // окна: разрыв хотя бы в одну пару (IsWindow)
}

// BuildDaySpan собирает день из пар. Пары одного времени — подгруппы или
// поток в нескольких аудиториях — считаются одной: человек один.
func BuildDaySpan(lessons []Lesson) DaySpan {
	ls := append([]Lesson(nil), lessons...)
	sort.Slice(ls, func(i, j int) bool { return ls[i].BeginsAt < ls[j].BeginsAt })
	var d DaySpan
	end := ""
	for _, l := range ls {
		switch {
		case d.Pairs == 0:
			d.From = l.BeginsAt
		case l.BeginsAt < end:
			// пересекается с уже учтённой — то же время, другая подгруппа
			if l.EndsAt > end {
				end = l.EndsAt
			}
			continue
		case IsWindow(end, l.BeginsAt):
			d.Windows = append(d.Windows, Slot{Begins: end, Ends: l.BeginsAt})
		}
		d.Pairs++
		if l.EndsAt > end {
			end = l.EndsAt
		}
	}
	d.To = end
	return d
}
