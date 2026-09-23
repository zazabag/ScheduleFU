package domain

import "sort"

// WindowMinutes — с какого разрыва между парами он считается окном.
// Большой перерыв в сетке — сорок минут (13:20—14:00), и его все знают;
// окно появляется, когда между парами выпадает хотя бы одна пара целиком.
const WindowMinutes = 60

// IsWindow — окно ли разрыв между концом одной пары и началом следующей.
func IsWindow(ends, begins string) bool {
	return Minutes(begins)-Minutes(ends) >= WindowMinutes
}

// Minutes — ЧЧ:ММ в минутах от полуночи; мусор — ноль.
func Minutes(hhmm string) int {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return 0
	}
	d := func(b byte) int { return int(b - '0') }
	return (d(hhmm[0])*10+d(hhmm[1]))*60 + d(hhmm[3])*10 + d(hhmm[4])
}

// FreeThrough — свободна ли аудитория весь интервал [from, to), а не только
// в его начале: в окно человек садится надолго, и пара, начавшаяся через
// двадцать минут, выгонит его посреди работы.
func FreeThrough(lessons []Lesson, from, to string) bool {
	for _, l := range lessons {
		if l.Overlaps(from, to) {
			return false
		}
	}
	return true
}

// Nearness — насколько аудитория далеко от якоря (обычно — аудитории
// следующей пары): меньше — ближе. Тот же этаж того же здания, затем
// соседние этажи по расстоянию, затем то же здание с неизвестным этажом,
// затем соседние здания площадки. Этаж, который мы не знаем, не угадывается:
// такая аудитория идёт после всех известных.
func Nearness(a, anchor Auditorium) int {
	if a.Building != anchor.Building {
		return 100
	}
	if a.Floor == nil || anchor.Floor == nil {
		return 50
	}
	d := *a.Floor - *anchor.Floor
	if d < 0 {
		d = -d
	}
	return d * 2
}

// SortNear упорядочивает аудитории по близости к якорю; при равной
// близости первой идёт более вместительная — в ней скорее найдётся место.
func SortNear(views []RoomView, anchor *Auditorium) {
	if anchor == nil {
		return
	}
	sort.SliceStable(views, func(i, j int) bool {
		ni, nj := Nearness(views[i].Auditorium, *anchor), Nearness(views[j].Auditorium, *anchor)
		if ni != nj {
			return ni < nj
		}
		return capOf(views[i].Auditorium) > capOf(views[j].Auditorium)
	})
}

func capOf(a Auditorium) int {
	if a.Capacity == nil {
		return 0
	}
	return *a.Capacity
}
