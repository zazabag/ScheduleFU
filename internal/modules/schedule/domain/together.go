package domain

import "sort"

// CommonCell — пара сетки глазами нескольких расписаний сразу: кто занят и
// насколько удобно собраться.
type CommonCell struct {
	Slot Slot
	// Busy — индексы занятых в порядке расписаний; пусто — свободны все.
	Busy []int
	// AllIn — у каждого в этот день есть пары, то есть все и так в вузе.
	// Свободная пара в день, когда у половины выходной, собрать людей не
	// поможет: ехать ради встречи никто не будет.
	AllIn bool
	// Inside — у скольких эта пара приходится внутри дня, между первой и
	// последней парой: для них это окно, которое всё равно надо чем-то занять.
	Inside int
}

// Free — свободны ли все.
func (c CommonCell) Free() bool { return len(c.Busy) == 0 }

// CommonDay раскладывает день нескольких расписаний по сетке. each[i] — пары
// i-го расписания в этот день. Занятость — по пересечению интервалов, как и
// у аудиторий: пары вне сетки иначе прятались бы.
func CommonDay(each [][]Lesson) []CommonCell {
	allIn := len(each) > 0
	for _, ls := range each {
		if len(ls) == 0 {
			allIn = false
		}
	}
	cells := make([]CommonCell, len(Slots))
	for si, s := range Slots {
		c := CommonCell{Slot: s, AllIn: allIn}
		for i, ls := range each {
			busy, first, last := false, "", ""
			for _, l := range ls {
				if l.Overlaps(s.Begins, s.Ends) {
					busy = true
				}
				if first == "" || l.BeginsAt < first {
					first = l.BeginsAt
				}
				if l.EndsAt > last {
					last = l.EndsAt
				}
			}
			if busy {
				c.Busy = append(c.Busy, i)
			} else if first != "" && first < s.Begins && s.Ends < last {
				c.Inside++
			}
		}
		cells[si] = c
	}
	return cells
}

// CommonSlot — предложение, когда собраться: день недели (0 — понедельник)
// и непрерывный отрезок свободных для всех пар.
type CommonSlot struct {
	Day       int
	From, To  string
	Pairs     int
	AllInside bool // у всех это окно между парами: идеальное время
}

// BestCommon — лучшие отрезки недели, когда свободны все и все в вузе.
// Сначала те, где отрезок у всех приходится на окно между парами, затем
// более длинные, затем более ранние в неделе.
func BestCommon(week [][]CommonCell, people, limit int) []CommonSlot {
	var out []CommonSlot
	for d, cells := range week {
		for i := 0; i < len(cells); {
			if !cells[i].Free() || !cells[i].AllIn {
				i++
				continue
			}
			j, inside := i, true
			for j < len(cells) && cells[j].Free() && cells[j].AllIn {
				if cells[j].Inside < people {
					inside = false
				}
				j++
			}
			out = append(out, CommonSlot{Day: d, From: cells[i].Slot.Begins, To: cells[j-1].Slot.Ends, Pairs: j - i, AllInside: inside})
			i = j
		}
	}
	sort.SliceStable(out, func(a, b int) bool {
		if out[a].AllInside != out[b].AllInside {
			return out[a].AllInside
		}
		return out[a].Pairs > out[b].Pairs
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
