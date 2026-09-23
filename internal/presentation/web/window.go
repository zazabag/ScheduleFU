package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// ─── свободно в интервал: окно между парами и планирование ───────────────────

// capacities — пороги вместимости в фильтре. Круглые числа, под которые
// ищут: мини-группа, группа, поток.
var capacities = []int{0, 30, 60, 100}

// roomsWindow — аудитории, свободные весь интервал [from, to) выбранного дня.
// Один экран на два вопроса: «где пересидеть окно» (есть near — аудитория
// следующей пары, от неё считается «рядом») и «найти аудиторию на четверг,
// третью пару, человек на тридцать» — старосте, клубу, преподавателю под
// консультацию.
func (s *Server) roomsWindow(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	today := s.d.Clock.Today()
	date := today
	if v := q.Get("date"); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, s.d.Clock.Location())
		if err != nil {
			http.Error(w, "дата должна быть в формате ГГГГ-ММ-ДД", http.StatusBadRequest)
			return
		}
		date = d
	}
	from, to := q.Get("from"), q.Get("to")
	if !validHHMM(from) || !validHHMM(to) || from >= to {
		http.Error(w, "интервал задаётся как from=ЧЧ:ММ&to=ЧЧ:ММ", http.StatusBadRequest)
		return
	}
	site := q.Get("site")
	near, _ := strconv.ParseInt(q.Get("near"), 10, 64)
	minCap, _ := strconv.Atoi(q.Get("cap"))
	ctx := r.Context()

	win, err := s.d.Schedule.FreeWindow(ctx, site, date, from, to, near, minCap)
	if err != nil {
		http.Error(w, "не удалось получить занятость", http.StatusInternalServerError)
		return
	}
	if win.Site.Slug != "" {
		site = win.Site.Slug
	}
	if win.Anchor == nil {
		near = 0
	}

	// Ссылка на тот же экран с одной заменой: чипы дня, пары и вместимости
	// меняют своё и сохраняют остальное.
	link := func(set map[string]string) template.URL {
		v := url.Values{"date": {date.Format("2006-01-02")}, "from": {from}, "to": {to}}
		if near > 0 {
			v.Set("near", strconv.FormatInt(near, 10))
		} else if site != "" {
			v.Set("site", site)
		}
		if minCap > 0 {
			v.Set("cap", strconv.Itoa(minCap))
		}
		for k, x := range set {
			if x == "" || x == "0" {
				v.Del(k)
			} else {
				v.Set(k, x)
			}
		}
		return template.URL("/rooms?" + v.Encode())
	}

	// Дни — от сегодня на неделю: дальше окна сбора данных нет.
	var days []chip
	for i := 0; i < 7; i++ {
		d := today.AddDate(0, 0, i)
		if d.Weekday() == time.Sunday {
			continue
		}
		label := clock.WeekdayShortRu(d) + " " + strconv.Itoa(d.Day())
		if i == 0 {
			label = "сегодня"
		}
		days = append(days, chip{Label: label, Href: link(map[string]string{"date": d.Format("2006-01-02")}), On: d.Equal(date)})
	}
	var slots []chip
	for i, sl := range sched.Slots {
		slots = append(slots, chip{Label: strconv.Itoa(i+1) + " · " + sl.Begins, Href: link(map[string]string{"from": sl.Begins, "to": sl.Ends}),
			On: from == sl.Begins && to == sl.Ends})
	}
	var caps []chip
	for _, c := range capacities {
		label := "любая"
		if c > 0 {
			label = "от " + strconv.Itoa(c)
		}
		caps = append(caps, chip{Label: label, Href: link(map[string]string{"cap": strconv.Itoa(c)}), On: c == minCap})
	}
	// Переключатель площадок — только без якоря: с якорем площадка уже
	// выбрана тем, куда человеку идти дальше.
	var tabs []chip
	if near == 0 {
		if sites, err := s.d.Schedule.Sites(ctx); err == nil {
			for _, x := range sites {
				tabs = append(tabs, chip{Label: x.Site.Label, Href: link(map[string]string{"site": x.Site.Slug}), On: x.Site.Slug == site})
			}
		}
	}

	// Группы по зданию и этажу в порядке близости: первым идёт этаж якоря,
	// потом соседние. Порядок задал сервис, здесь он только сохраняется.
	type row struct {
		Room, Meta string
		Cells      []sched.SlotCell
		Lessons    []sched.Lesson
	}
	type floorGroup struct {
		Label string
		Rooms []row
	}
	var groups []floorGroup
	index := map[string]int{}
	for _, v := range win.Rooms {
		a := v.Auditorium
		key, title := a.Building+"|?", s.d.BuildingLabel(a.Building)+" · этаж не определён"
		if a.Floor != nil {
			key = a.Building + "|" + strconv.Itoa(*a.Floor)
			title = s.d.BuildingLabel(a.Building) + " · " + strconv.Itoa(*a.Floor) + " этаж"
		}
		if win.Anchor != nil && sched.Nearness(a, *win.Anchor) == 0 {
			title = "рядом · " + title
		}
		gi, ok := index[key]
		if !ok {
			gi = len(groups)
			index[key] = gi
			groups = append(groups, floorGroup{Label: title})
		}
		groups[gi].Rooms = append(groups[gi].Rooms, row{Room: roomShort(a.Room), Meta: s.roomMeta(a), Cells: v.Cells, Lessons: v.Lessons})
	}

	title := "Свободно " + from + "—" + to
	sub := win.Site.Label
	if win.Anchor != nil {
		title = "Где пересидеть окно"
		sub = from + "—" + to + " · рядом с " + roomShort(win.Anchor.Room)
	}
	s.render(w, r, "window", map[string]any{
		"Title": title, "Tab": "rooms", "Heading": title, "Sub": sub,
		"Date": dateInfo(date, today), "Days": days, "Slots": slots, "Caps": caps, "Sites": tabs,
		"Groups": groups, "Count": pluralN(len(win.Rooms), "аудитория", "аудитории", "аудиторий"), "Total": win.Total,
		"NowHref": template.URL("/rooms?site=" + url.QueryEscape(site)), "Freshness": s.freshness(r),
	})
}

// validHHMM — строго ЧЧ:ММ: значение уходит в сравнение строк, и «9:30»
// оказалось бы позже «14:00».
func validHHMM(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return v[0] <= '2' && v[3] <= '5'
}

// planHref — ссылка «другая пара или день» с экрана «сейчас»: ближайшая
// пара сегодня, а если день кончился — первая пара завтра.
func planHref(site string, at time.Time, sum sched.SiteSummary) template.URL {
	slot, date := sched.Slots[0], at.AddDate(0, 0, 1)
	if sum.NextSlot != nil {
		slot, date = sum.NextSlot.Slot, at
	}
	v := url.Values{"site": {site}, "date": {date.Format("2006-01-02")}, "from": {slot.Begins}, "to": {slot.Ends}}
	return template.URL("/rooms?" + v.Encode())
}
