package domain

// BreakLoad — перемена между парами сетки: сколько пар на площадке к ней
// заканчивается и сколько после неё начинается. Людей не считаем: сколько
// человек в группе, источник не говорит, а выдуманное число хуже честного
// «расходятся 64 пары».
type BreakLoad struct {
	From, To string // конец пары и начало следующей
	Ending   int
	Starting int
	State    string // past · now · next · later
	// Peak — самая людная из ещё не прошедших перемен.
	Peak bool
}

// breakSlack — насколько пара вне сетки может разойтись с её границей и
// всё ещё считаться «к этой перемене». Около двух процентов пар идут вне
// сетки, и чаще всего это сдвиг на пять-десять минут.
const breakSlack = 15

// BuildBreaks раскладывает пары площадки по переменам. Пара относится к
// перемене, ближайшей к её концу (для «расходятся») и к её началу (для
// «сходятся»), если граница не дальше breakSlack минут.
func BuildBreaks(views []RoomView, now string) []BreakLoad {
	out := make([]BreakLoad, 0, len(Slots)-1)
	for i := 0; i+1 < len(Slots); i++ {
		out = append(out, BreakLoad{From: Slots[i].Ends, To: Slots[i+1].Begins, State: "later"})
	}
	nearest := func(t string, edge func(BreakLoad) string) int {
		best, bd := -1, breakSlack+1
		for i, b := range out {
			d := Minutes(t) - Minutes(edge(b))
			if d < 0 {
				d = -d
			}
			if d < bd {
				best, bd = i, d
			}
		}
		return best
	}
	for _, v := range views {
		for _, l := range v.Lessons {
			if i := nearest(l.EndsAt, func(b BreakLoad) string { return b.From }); i >= 0 {
				out[i].Ending++
			}
			if i := nearest(l.BeginsAt, func(b BreakLoad) string { return b.To }); i >= 0 {
				out[i].Starting++
			}
		}
	}
	nextSet := false
	peak := -1
	for i := range out {
		switch {
		case out[i].To <= now:
			out[i].State = "past"
		case out[i].From <= now:
			out[i].State = "now"
		case !nextSet:
			out[i].State, nextSet = "next", true
		}
		if out[i].State != "past" && out[i].Ending > 0 && (peak < 0 || out[i].Ending > out[peak].Ending) {
			peak = i
		}
	}
	if peak >= 0 {
		out[peak].Peak = true
	}
	return out
}
