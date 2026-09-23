package web

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// maxTogether — потолок групп на экране общих окон. Больше шести в одну
// сетку не помещается глазами, а каждая группа — это ещё и раз в день
// запрос к вузу за языковыми подгруппами.
const maxTogether = 6

// together — общие окна нескольких групп на неделю: когда собраться другу с
// другом, команде курсового проекта, клубу. Входа не нужно: вся компания
// живёт в адресе, и ссылкой на экран делятся в чате.
func (s *Server) together(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var names []string
	seen := map[string]bool{}
	for _, raw := range q["g"] {
		// Поле ввода принимает и «ПИ24-1, ПИ24-2» одной строкой.
		for _, g := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
			// Регистр не трогаем: в именах групп вуза бывают строчные хвосты,
			// и «исправленное» имя не нашлось бы. Строчный ввод целиком —
			// «пи24-1» — поднимается ниже, если как есть ничего не нашлось.
			g = strings.TrimSpace(g)
			if g == "" || seen[g] || len(names) >= maxTogether {
				continue
			}
			seen[g] = true
			names = append(names, g)
		}
	}
	// Своя группа подставляется сама: экран открывают, чтобы сравнить с ней.
	if len(names) == 0 {
		if p := SubjectFromCookie(r); p.Kind == sched.SubjectGroup {
			names = append(names, p.Group)
		}
	}

	today := s.d.Clock.Today()
	date := today
	if v := q.Get("date"); v != "" {
		if d, err := time.ParseInLocation("2006-01-02", v, s.d.Clock.Location()); err == nil {
			date = d
		}
	}
	weekStart := clock.StartOfWeek(date)
	ctx := r.Context()

	type person struct {
		Name   string
		Remove template.URL
		Empty  bool
	}
	people := make([]person, len(names))
	byDay := make([][][]sched.Lesson, 6) // понедельник — суббота
	for d := range byDay {
		byDay[d] = make([][]sched.Lesson, len(names))
	}
	for i, g := range names {
		subj := sched.GroupSubject(g)
		lessons, err := s.d.Schedule.ScheduleFor(ctx, subj, weekStart, weekStart.AddDate(0, 0, 5))
		if err == nil && len(lessons) == 0 && strings.ToUpper(g) != g {
			g = strings.ToUpper(g)
			names[i], subj = g, sched.GroupSubject(g)
			lessons, err = s.d.Schedule.ScheduleFor(ctx, subj, weekStart, weekStart.AddDate(0, 0, 5))
		}
		if err != nil {
			http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
			return
		}
		// Языковые подгруппы дотягиваются только для группы, которая уже
		// есть в слепке: имя вводят руками, и опечатка «ПИ99-9» иначе
		// превращалась бы в поисковый запрос к вузу при каждом открытии.
		if len(lessons) > 0 {
			linkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			if err := s.d.Schedule.EnsureGroupLinks(linkCtx, g, today, today.AddDate(0, 0, 13)); err != nil {
				fmt.Printf("web: привязка группы %s: %v\n", g, err)
			}
			cancel()
			if lessons, err = s.d.Schedule.ScheduleFor(ctx, subj, weekStart, weekStart.AddDate(0, 0, 5)); err != nil {
				http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
				return
			}
		}
		people[i] = person{Name: g, Empty: len(lessons) == 0}
		for _, l := range lessons {
			if d := int(l.Date.Sub(weekStart).Hours() / 24); d >= 0 && d < len(byDay) {
				byDay[d][i] = append(byDay[d][i], l)
			}
		}
	}

	// Ссылка «убрать» — после цикла: имена в нём ещё могли поменять регистр.
	for i := range people {
		rest := url.Values{"date": {date.Format("2006-01-02")}}
		for j, other := range names {
			if j != i {
				rest.Add("g", other)
			}
		}
		people[i].Remove = template.URL("/together?" + rest.Encode())
	}

	// Считаются только группы с парами на этой неделе: пустая — опечатка
	// или неделя без занятий, свободна она всегда, а правило «все в вузе»
	// из-за неё не выполнилось бы ни разу. На экране она остаётся с пометкой.
	var active []int
	for i, p := range people {
		if !p.Empty {
			active = append(active, i)
		}
	}
	type cell struct {
		State, Title string
		Busy         int
	}
	type dayRow struct {
		Label string
		Today bool
		Cells []cell
	}
	var grid []dayRow
	var week [][]sched.CommonCell
	for d := range byDay {
		day := weekStart.AddDate(0, 0, d)
		each := make([][]sched.Lesson, len(active))
		for k, i := range active {
			each[k] = byDay[d][i]
		}
		cells := sched.CommonDay(each)
		week = append(week, cells)
		row := dayRow{Label: clock.WeekdayShortRu(day) + " " + strconv.Itoa(day.Day()), Today: day.Equal(today)}
		for _, c := range cells {
			x := cell{Busy: len(c.Busy)}
			switch {
			case !c.Free():
				x.State = "busy"
				var who []string
				for _, i := range c.Busy {
					who = append(who, names[active[i]])
				}
				x.Title = c.Slot.Begins + " · заняты: " + strings.Join(who, ", ")
			case c.AllIn:
				x.State = "meet"
				x.Title = c.Slot.Begins + " · свободны все, все в вузе"
			default:
				x.State = "open"
				x.Title = c.Slot.Begins + " · свободны все, но не у всех есть пары"
			}
			row.Cells = append(row.Cells, x)
		}
		grid = append(grid, row)
	}
	var best []string
	if len(active) > 1 {
		for _, b := range sched.BestCommon(week, len(active), 3) {
			day := weekStart.AddDate(0, 0, b.Day)
			line := clock.WeekdayRu(day) + ", " + b.From + "—" + b.To
			if b.AllInside {
				line += " — у всех окно между парами"
			}
			best = append(best, line)
		}
	}

	hrefFor := func(d time.Time) template.URL {
		v := url.Values{"g": names, "date": {d.Format("2006-01-02")}}
		return template.URL("/together?" + v.Encode())
	}
	var slotLabels []string
	for _, sl := range sched.Slots {
		slotLabels = append(slotLabels, strings.TrimPrefix(sl.Begins, "0"))
	}
	s.render(w, r, "together", map[string]any{
		"Title": "Общие окна", "Tab": "schedule", "People": people, "Names": names, "Full": len(names) >= maxTogether,
		"Grid": grid, "Slots": slotLabels, "Best": best, "Range": weekRange(weekStart, weekStart.AddDate(0, 0, 6)),
		"PrevHref": hrefFor(weekStart.AddDate(0, 0, -7)), "NextHref": hrefFor(weekStart.AddDate(0, 0, 7)),
		"ShareHref": hrefFor(weekStart), "Freshness": s.freshness(r),
	})
}
