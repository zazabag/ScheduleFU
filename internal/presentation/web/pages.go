package web

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

type chip struct {
	Label, Href string
	On          bool
}

// ─── свободные аудитории ─────────────────────────────────────────────────────

func (s *Server) rooms(w http.ResponseWriter, r *http.Request) {
	site := r.URL.Query().Get("site")
	if site == "" {
		site = "leningradsky"
	}
	floor := r.URL.Query().Get("floor")
	ctx := r.Context()

	sites, err := s.d.Schedule.Sites(ctx)
	if err != nil {
		http.Error(w, "не удалось получить список корпусов", http.StatusInternalServerError)
		return
	}
	free, total, err := s.d.Schedule.FreeRooms(ctx, site, s.d.Clock.Now())
	if err != nil {
		http.Error(w, "не удалось получить занятость", http.StatusInternalServerError)
		return
	}

	label := site
	tabs := make([]chip, 0, len(sites))
	for _, x := range sites {
		if x.Site.Slug == site {
			label = x.Site.Label
		}
		tabs = append(tabs, chip{Label: x.Site.Label, Href: "/rooms?site=" + url.QueryEscape(x.Site.Slug), On: x.Site.Slug == site})
	}

	// Этажи — из того, что реально есть на площадке: нумерация в корпусах
	// разная, общего списка не существует.
	floorSet := map[int]bool{}
	type row struct {
		Room, Meta, Until, UntilClass string
		Cells                         []sched.SlotCell
		Lessons                       []sched.Lesson
	}
	var rows []row
	for _, v := range free {
		if v.Auditorium.Floor != nil {
			floorSet[*v.Auditorium.Floor] = true
		}
		if floor != "" && (v.Auditorium.Floor == nil || strconv.Itoa(*v.Auditorium.Floor) != floor) {
			continue
		}
		until, cls := "до конца дня", ""
		if v.FreeUntil != "" {
			until, cls = "до "+v.FreeUntil, "warn"
		}
		rows = append(rows, row{Room: roomShort(v.Auditorium.Room), Meta: s.roomMeta(v.Auditorium), Until: until, UntilClass: cls, Cells: v.Cells, Lessons: v.Lessons})
	}
	floors := make([]int, 0, len(floorSet))
	for f := range floorSet {
		floors = append(floors, f)
	}
	sort.Ints(floors)
	chips := []chip{{Label: "все", Href: "/rooms?site=" + url.QueryEscape(site), On: floor == ""}}
	for _, f := range floors {
		v := strconv.Itoa(f)
		chips = append(chips, chip{Label: v, Href: "/rooms?site=" + url.QueryEscape(site) + "&floor=" + v, On: floor == v})
	}

	now := s.d.Clock.Now()
	s.render(w, r, "rooms", map[string]any{
		"Title": "Свободные аудитории", "Tab": "rooms", "Clock": s.d.Clock.HHMM(),
		"Today":     clock.DateRu(now) + " · " + clock.WeekdayRu(now),
		"SiteLabel": label, "FreeCount": len(free), "TotalCount": total,
		"Sites": tabs, "Floors": chips, "Rooms": rows, "Freshness": s.freshness(r),
	})
}

func (s *Server) roomMeta(a sched.Auditorium) string {
	parts := []string{s.d.BuildingLabel(a.Building)}
	if a.Floor != nil {
		parts = append(parts, strconv.Itoa(*a.Floor)+" эт")
	}
	if a.Capacity != nil && *a.Capacity > 0 {
		parts = append(parts, strconv.Itoa(*a.Capacity)+" мест")
	}
	return strings.Join(parts, " · ")
}

// ─── расписание ──────────────────────────────────────────────────────────────

// lessonRow — пара в том виде, в каком её показывают все оформления.
// Здесь есть всё, что нужно любому из них: одни рисуют статус словом,
// другие — точкой, третьи — положением на маршруте.
type lessonRow struct {
	Index                                                        int
	BeginsAt, EndsAt, Discipline, KindOfWork, LecturerName, Room string
	Place                                                        string
	Status                                                       string // past · now · next · later
	StatusLabel                                                  string
	Changed                                                      bool // деканат правил пару недавно
	LongRoom                                                     bool // название, а не номер: показывать мельче
	Groups                                                       string
}

func (s *Server) lessonRow(l sched.Lesson, subj sched.Subject) lessonRow {
	row := lessonRow{BeginsAt: l.BeginsAt, EndsAt: l.EndsAt, Discipline: l.Discipline,
		KindOfWork: shortKind(l.KindOfWork), LecturerName: l.LecturerName, Room: roomShort(l.Auditorium),
		Groups: strings.Join(l.GroupNames, ", ")}
	row.Place = s.d.BuildingLabel(l.Building)
	row.LongRoom = len([]rune(row.Room)) > 6
	// У преподавателя в строке пары полезен состав групп, а не его имя.
	if subj.Kind == sched.SubjectLecturer {
		row.LecturerName = row.Groups
	}
	// «Изменено» держится три дня: за это время человек успевает увидеть
	// правку, а дальше она уже просто расписание.
	if l.SourceModifiedAt != nil && s.d.Clock.Now().Sub(*l.SourceModifiedAt) < 72*time.Hour {
		row.Changed = true
	}
	return row
}

// hero — верхний блок экрана дня: то, что оформления показывают по-разному
// (гигантская дата, кольцо отсчёта, табло, обложка), но считают одинаково.
type hero struct {
	State      string // none · before · now · between · after · day
	Label      string
	Lesson     *lessonRow // текущая либо ближайшая пара
	Next       *lessonRow
	Sentence   string
	Mood       string
	Progress   int // процент текущей пары
	RemainMin  int
	Count      int
	CountLabel string
	Stops      string
	First      string
	Last       string
	RoomCount  int
	Route      routeView
	Plan       planView
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	subj, err := sched.SubjectFromValues(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	// Закрепление уводит на чистый адрес, чтобы «назад» в браузере не
	// повторяло действие.
	if !subj.IsZero() && r.URL.Query().Get("pin") == "1" {
		SetSubjectCookie(w, subj, secure)
		http.Redirect(w, r, "/schedule?"+subj.Query(), http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("unpin") == "1" {
		ClearSubjectCookie(w, secure)
		http.Redirect(w, r, "/schedule", http.StatusSeeOther)
		return
	}
	if subj.IsZero() {
		subj = SubjectFromCookie(r)
	}
	data := map[string]any{"Title": "Расписание", "Tab": "schedule", "Freshness": s.freshness(r)}
	if subj.IsZero() {
		// Ничего не закреплено — первый экран и есть выбор группы.
		s.pickerData(r, data)
		data["Picker"] = true
		s.render(w, r, "schedule", data)
		return
	}

	today := s.d.Clock.Today()
	date := today
	if v := r.URL.Query().Get("date"); v != "" {
		if date, err = time.ParseInLocation("2006-01-02", v, s.d.Clock.Location()); err != nil {
			http.Error(w, "дата должна быть в формате ГГГГ-ММ-ДД", http.StatusBadRequest)
			return
		}
	}
	// Неделя целиком от понедельника: и сегодня, и дальше без переходов.
	weekStart := clock.StartOfWeek(date)
	lessons, err := s.d.Schedule.ScheduleFor(r.Context(), subj, weekStart, weekStart.AddDate(0, 0, 6))
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}
	label := subj.Group
	if subj.Kind == sched.SubjectLecturer {
		label = s.d.Schedule.LecturerName(r.Context(), subj.LecturerOid, lessons)
	}

	has := map[string]int{}
	for _, l := range lessons {
		has[l.DateKey()]++
	}
	type dayView struct {
		Dow, Num, Href string
		On, Has, Today bool
		Count          int
	}
	dateKey := date.Format("2006-01-02")
	todayKey := today.Format("2006-01-02")
	hrefFor := func(d time.Time) string { return "/schedule?" + subj.Query() + "&date=" + d.Format("2006-01-02") }
	var days []dayView
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		days = append(days, dayView{Dow: clock.WeekdayShortRu(d), Num: strconv.Itoa(d.Day()), Href: hrefFor(d),
			On: key == dateKey, Has: has[key] > 0, Today: key == todayKey, Count: has[key]})
	}
	var rows []lessonRow
	for _, l := range lessons {
		if l.DateKey() == dateKey {
			rows = append(rows, s.lessonRow(l, subj))
		}
	}
	now := s.d.Clock.HHMM()
	if v := r.URL.Query().Get("now"); s.d.Dev && len(v) == 5 {
		now, todayKey = v, dateKey
	}
	markStatuses(rows, dateKey, todayKey, now)
	h := s.buildHero(rows, dateKey == todayKey, now)

	pinned := SubjectFromCookie(r)
	_, week := date.ISOWeek()
	weekEnd := weekStart.AddDate(0, 0, 6)
	data["Group"], data["IsLecturer"] = label, subj.Kind == sched.SubjectLecturer
	data["Date"] = map[string]any{
		"Num": strconv.Itoa(date.Day()), "Dow": clock.WeekdayRu(date), "DowShort": clock.WeekdayShortRu(date),
		"Month": clock.MonthRu(date), "MonthGen": strings.TrimPrefix(clock.DateRu(date), strconv.Itoa(date.Day())+" "),
		"Year": strconv.Itoa(date.Year()), "Week": strconv.Itoa(week), "Key": dateKey, "MM": date.Format("01"),
		"IsToday": dateKey == today.Format("2006-01-02"), "Label": clock.DateRu(date) + " · " + clock.WeekdayRu(date),
		"Range": weekRange(weekStart, weekEnd),
	}
	data["Subtitle"] = clock.DateRu(date) + " · " + clock.WeekdayRu(date)
	data["Days"], data["Lessons"], data["Hero"] = days, rows, h
	data["PrevWeekHref"], data["NextWeekHref"], data["TodayHref"] = hrefFor(weekStart.AddDate(0, 0, -7)), hrefFor(weekStart.AddDate(0, 0, 7)), hrefFor(today)
	data["PrevDayHref"], data["NextDayHref"] = hrefFor(date.AddDate(0, 0, -1)), hrefFor(date.AddDate(0, 0, 1))
	data["SubjectKey"], data["SubjectQuery"] = subj.Key(), subj.Query()
	data["Pinned"], data["PinHref"] = pinned.Key() == subj.Key(), "/schedule?"+subj.Query()+"&pin=1"
	data["ChangeHref"] = map[bool]string{true: "/lecturers", false: "/groups"}[subj.Kind == sched.SubjectLecturer]
	data["Clock"] = now
	s.render(w, r, "schedule", data)
}

// weekRange: «21—27 сентября», а на стыке месяцев — «28 сентября — 4 октября».
func weekRange(from, to time.Time) string {
	if from.Month() == to.Month() {
		return strconv.Itoa(from.Day()) + "—" + clock.DateRu(to)
	}
	return clock.DateRu(from) + " — " + clock.DateRu(to)
}

// markStatuses расставляет прошла/идёт/следующая/позже. Для другого дня
// всё либо прошло, либо впереди — «сейчас» бывает только сегодня.
func markStatuses(rows []lessonRow, dateKey, todayKey, now string) {
	nextSet := false
	for i := range rows {
		rows[i].Index = i + 1
		switch {
		case dateKey < todayKey, dateKey == todayKey && rows[i].EndsAt <= now:
			rows[i].Status, rows[i].StatusLabel = "past", "прошла"
		case dateKey == todayKey && rows[i].BeginsAt <= now:
			rows[i].Status, rows[i].StatusLabel = "now", "сейчас"
		case dateKey == todayKey && !nextSet:
			rows[i].Status, rows[i].StatusLabel, nextSet = "next", "следующая", true
		default:
			rows[i].Status, rows[i].StatusLabel = "later", ""
		}
	}
}

func (s *Server) buildHero(rows []lessonRow, isToday bool, now string) hero {
	h := hero{Count: len(rows), CountLabel: "пар нет", State: "none", Label: "пар нет", Mood: "выходной", Sentence: "Сегодня пар нет."}
	if len(rows) == 0 {
		if !isToday {
			h.Sentence = "Пар нет."
		}
		return h
	}
	h.First, h.Last = rows[0].BeginsAt, rows[len(rows)-1].EndsAt
	seen := map[string]bool{}
	for _, r := range rows {
		if r.Room != "" && !seen[r.Room] {
			seen[r.Room] = true
			h.RoomCount++
		}
	}
	plural := func(n int) string {
		switch {
		case n%10 == 1 && n%100 != 11:
			return strconv.Itoa(n) + " пара"
		case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
			return strconv.Itoa(n) + " пары"
		}
		return strconv.Itoa(n) + " пар"
	}
	h.Route = buildRoute(rows)
	h.Plan = buildPlan(rows)
	h.CountLabel = plural(len(rows))
	h.Stops = stopsLabel(len(rows), 0)

	if !isToday {
		h.State, h.Lesson = "day", &rows[0]
		if len(rows) > 1 {
			h.Next = &rows[1]
		}
		h.Label = plural(len(rows)) + " · " + h.First + "—" + h.Last
		h.Mood = plural(len(rows))
		h.Sentence = fmt.Sprintf("%s: с %s до %s. Первая — %s, %s.", plural(len(rows)), h.First, h.Last, rows[0].Discipline, roomPhrase(rows[0]))
		return h
	}
	var cur, next *lessonRow
	done := 0
	for i := range rows {
		switch rows[i].Status {
		case "now":
			cur = &rows[i]
		case "next":
			next = &rows[i]
		case "past":
			done++
		}
	}
	if cur != nil {
		// Следующая после текущей: markStatuses её не помечает, потому что
		// «следующая» — это ближайшая к сейчас, а не к идущей паре.
		if cur.Index < len(rows) {
			next = &rows[cur.Index]
		}
		h.State, h.Lesson, h.Next = "now", cur, next
		h.Label = "сейчас · до " + cur.EndsAt
		h.Stops = stopsLabel(len(rows), cur.Index)
		h.Progress, h.RemainMin = progress(cur.BeginsAt, cur.EndsAt, now)
		h.Sentence = fmt.Sprintf("%s идёт до %s, %s.", cur.Discipline, cur.EndsAt, roomPhrase(*cur))
		if next != nil {
			h.Sentence += " Дальше " + next.Discipline + " в " + next.BeginsAt
			if next.LecturerName != "" {
				h.Sentence += ", ведёт " + strings.TrimSuffix(next.LecturerName, ".")
			}
			h.Sentence += "."
		}
		switch {
		case len(rows) == 1:
			h.Mood = "единственная пара — и свободен"
		case cur.Index == 1:
			h.Mood = "первая пошла — разгон"
		case cur.Index == len(rows):
			h.Mood = "последняя — почти всё"
		case float64(cur.Index) >= float64(len(rows))/2:
			h.Mood = "держись — половина позади"
		default:
			h.Mood = "набираем ход"
		}
		return h
	}
	if next != nil {
		h.Lesson, h.Next = next, nil
		if next.Index < len(rows) {
			h.Next = &rows[next.Index]
		}
		h.Stops = stopsLabel(len(rows), next.Index)
		if done == 0 {
			h.State, h.Label = "before", "первая в "+next.BeginsAt
			h.Mood = "день впереди — " + plural(len(rows))
			h.Sentence = fmt.Sprintf("Первая пара в %s — %s, %s.", next.BeginsAt, next.Discipline, roomPhrase(*next))
		} else {
			h.State, h.Label = "between", "перерыв · следующая в "+next.BeginsAt
			h.Mood = "перерыв до " + next.BeginsAt
			h.Sentence = fmt.Sprintf("Перерыв до %s. Дальше %s, %s.", next.BeginsAt, next.Discipline, roomPhrase(*next))
		}
		_, h.RemainMin = progress(now, next.BeginsAt, now)
		return h
	}
	h.State, h.Label, h.Mood = "after", "пары закончились", "всё, свободен"
	h.Lesson = &rows[len(rows)-1]
	h.Sentence = fmt.Sprintf("Пары закончились в %s. Было %s.", h.Last, plural(len(rows)))
	return h
}

// roomPhrase: «аудитория 204» для номера, само название — для «Зала
// военной подготовки»; без аудитории — «без аудитории».
func roomPhrase(r lessonRow) string {
	switch {
	case r.Room == "":
		return "без аудитории"
	case r.LongRoom:
		return r.Room
	}
	return "аудитория " + r.Room
}

// progress — доля пройденного и оставшиеся минуты между двумя ЧЧ:ММ.
func progress(from, to, now string) (int, int) {
	mins := func(hhmm string) int {
		h, _ := strconv.Atoi(hhmm[:2])
		m, _ := strconv.Atoi(hhmm[3:])
		return h*60 + m
	}
	total := mins(to) - mins(from)
	if total <= 0 {
		return 100, 0
	}
	done := mins(now) - mins(from)
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	return done * 100 / total, total - done
}

// ─── геометрия для оформлений «Карта дня» и «План корпуса» ──────────────────

// routeView — извилистая дорога с остановками: снизу вверх, по паре на
// остановку. Геометрия считается здесь, а не в CSS, потому что число пар
// известно только серверу.
type routeView struct {
	Path string
	Pins []routePin
}
type routePin struct {
	X, Y   int
	Right  bool   // остановка у правого края: подпись слева от неё
	Title  string // название, укороченное до ширины экрана
	Lesson lessonRow
}

func buildRoute(rows []lessonRow) routeView {
	n := len(rows)
	var v routeView
	if n == 0 {
		return v
	}
	const top, bottom = 60, 330
	step := 0
	if n > 1 {
		step = (bottom - top) / (n - 1)
	}
	xs := []int{110, 250, 140, 270, 120, 240, 160, 260}
	var b strings.Builder
	prevX, prevY := 90, 400
	b.WriteString(fmt.Sprintf("M%d,%d", prevX, prevY))
	for i := 0; i < n; i++ {
		x, y := xs[i%len(xs)], bottom-i*step
		midY := (prevY + y) / 2
		b.WriteString(fmt.Sprintf(" C%d,%d %d,%d %d,%d", prevX, midY, x, midY, x, y))
		v.Pins = append(v.Pins, routePin{X: x, Y: y, Right: x > 200, Title: shorten(rows[i].Discipline, 26), Lesson: rows[i]})
		prevX, prevY = x, y
	}
	b.WriteString(fmt.Sprintf(" C%d,%d %d,%d %d,%d", prevX, prevY-60, 300, 10, 330, -20))
	v.Path = b.String()
	return v
}

// planView — схема: по коробке на аудиторию дня, маршрут между парами.
// Настоящих планов корпусов у нас нет, схема условная — но порядок
// аудиторий и «ты здесь» настоящие.
type planView struct {
	Boxes []planBox
	Path  string // маршрут через все аудитории дня
	Now   string // маршрут от текущей к следующей
	Here  *planBox
	Next  *planBox
}
type planBox struct {
	X, Y, W, H int
	CX, CY     int // центр: сюда ставится метка «ты здесь»
	Room       string
	Status     string // past · now · next · later
}

func buildPlan(rows []lessonRow) planView {
	var v planView
	if len(rows) == 0 {
		return v
	}
	const cols, w, h, gap, ox, oy = 3, 106, 96, 12, 8, 8
	index := map[string]int{}
	for _, r := range rows {
		room := r.Room
		if room == "" {
			room = "—"
		}
		if _, ok := index[room]; ok {
			continue
		}
		i := len(v.Boxes)
		if i >= 6 {
			break
		}
		index[room] = i
		x, y := ox+(i%cols)*(w+gap), oy+(i/cols)*(h+gap)
		v.Boxes = append(v.Boxes, planBox{X: x, Y: y, W: w, H: h, CX: x + w/2, CY: y + h/2, Room: room, Status: "later"})
	}
	center := func(b planBox) (int, int) { return b.CX, b.CY }
	// Текущая пара и та, что идёт за ней (или ближайшая, если перерыв).
	curI, nextI := -1, -1
	for i, r := range rows {
		if r.Status == "now" {
			curI = i
		}
		if r.Status == "next" {
			nextI = i
		}
	}
	if curI >= 0 && curI+1 < len(rows) {
		nextI = curI + 1
	}
	var all strings.Builder
	for i, r := range rows {
		bi, ok := index[nz(r.Room)]
		if !ok {
			continue
		}
		b := &v.Boxes[bi]
		switch {
		case i == curI:
			b.Status = "now"
		case i == nextI && b.Status != "now":
			b.Status = "next"
		case r.Status == "past" && b.Status == "later":
			b.Status = "past"
		}
		x, y := center(*b)
		if i == 0 {
			all.WriteString(fmt.Sprintf("M%d,%d", x, y))
		} else {
			all.WriteString(fmt.Sprintf(" L%d,%d", x, y))
		}
	}
	v.Path = all.String()
	if curI >= 0 {
		v.Here = &v.Boxes[index[nz(rows[curI].Room)]]
	}
	if nextI >= 0 {
		v.Next = &v.Boxes[index[nz(rows[nextI].Room)]]
	}
	if v.Here != nil && v.Next != nil && v.Here != v.Next {
		x1, y1 := center(*v.Here)
		x2, y2 := center(*v.Next)
		v.Now = fmt.Sprintf("M%d,%d C%d,%d %d,%d %d,%d", x1, y1, x1, (y1+y2)/2+30, x2, (y1+y2)/2-30, x2, y2)
	}
	return v
}

// shorten обрезает по словам, чтобы подпись на карте не уезжала за край.
func shorten(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := max
	for cut > 8 && r[cut] != ' ' {
		cut--
	}
	return strings.TrimSpace(string(r[:cut])) + "…"
}

// stopsLabel: «4 остановки · ты на второй» — для оформления «Карта дня».
func stopsLabel(n, at int) string {
	stops := strconv.Itoa(n) + " остановок"
	switch {
	case n%10 == 1 && n%100 != 11:
		stops = strconv.Itoa(n) + " остановка"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 10 || n%100 >= 20):
		stops = strconv.Itoa(n) + " остановки"
	}
	ord := []string{"", "первой", "второй", "третьей", "четвёртой", "пятой", "шестой", "седьмой", "восьмой"}
	if at >= 1 && at < len(ord) {
		return stops + " · ты на " + ord[at]
	}
	return stops
}

func nz(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ─── выбор группы ────────────────────────────────────────────────────────────

// groupNameRe — настоящая группа: «ПИ24-1», «Ю24-5в». Именно \p{L}, а не
// [[:alpha:]]: в Go последний — только латиница, и кириллица не проходила. В справочнике источника
// рядом лежат потоки вроде «006073_2 Иностранный язык (КАЯиПК)-10 СОЦ25-6_7»:
// студент своей группой их не назовёт, в выборе им не место.
var groupNameRe = regexp.MustCompile(`^\p{L}+[0-9]{2}-[0-9]+\p{L}*$`)

func (s *Server) pickerData(r *http.Request, data map[string]any) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	course := r.URL.Query().Get("course")
	chosen := SubjectFromCookie(r)
	found, err := s.d.Schedule.Repo().SearchGroups(r.Context(), q, 200)
	if err != nil {
		found = nil
	}
	year := academicYear(s.d.Clock.Now())
	type row struct {
		Name, NameEscaped, Meta string
		On                      bool
	}
	var rows []row
	for _, g := range found {
		if !groupNameRe.MatchString(g.Name) {
			continue
		}
		c := g.Course(year)
		if course != "" && strconv.Itoa(c) != course {
			continue
		}
		meta := "факультет " + g.FacultyOid
		if c > 0 {
			meta = strconv.Itoa(c) + " курс · " + meta
		}
		rows = append(rows, row{Name: g.Name, NameEscaped: url.QueryEscape(g.Name), Meta: meta, On: chosen.Group == g.Name})
	}
	base := r.URL.Path + "?q=" + url.QueryEscape(q)
	chips := []chip{{Label: "все", Href: base, On: course == ""}}
	for i := 1; i <= 5; i++ {
		v := strconv.Itoa(i)
		chips = append(chips, chip{Label: v, Href: base + "&course=" + v, On: course == v})
	}
	data["Query"], data["Courses"], data["Groups"] = q, chips, rows
	data["ResultLabel"] = fmt.Sprintf("найдено: %d", len(rows))
	data["Action"] = r.URL.Path
}

func (s *Server) groups(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"Title": "Выбор группы", "Tab": "schedule", "Picker": true, "Freshness": s.freshness(r)}
	s.pickerData(r, data)
	s.render(w, r, "schedule", data)
}

// academicYear: учебный год начинается в сентябре.
func academicYear(t time.Time) int {
	if t.Month() >= time.September {
		return t.Year()
	}
	return t.Year() - 1
}

// ─── преподаватели ───────────────────────────────────────────────────────────

func (s *Server) lecturers(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	now := s.d.Clock.Now()
	data := map[string]any{"Title": "Преподаватели", "Tab": "lecturers", "Query": q,
		"Clock": s.d.Clock.HHMM(), "Subtitle": "Где преподаватель сейчас",
		"Today": clock.DateRu(now) + " · " + clock.WeekdayRu(now), "Freshness": s.freshness(r)}
	pinned := SubjectFromCookie(r)
	if pinned.Kind == sched.SubjectLecturer {
		data["PinnedOid"] = pinned.LecturerOid
		data["PinnedName"] = s.d.Schedule.LecturerName(r.Context(), pinned.LecturerOid, nil)
	}
	oidParam := r.URL.Query().Get("oid")
	if oidParam == "" {
		if q != "" {
			found, err := s.d.Schedule.Repo().SearchLecturers(r.Context(), q, 30)
			if err != nil {
				http.Error(w, "не удалось найти преподавателя", http.StatusInternalServerError)
				return
			}
			data["Found"] = found
		}
		s.render(w, r, "lecturers", data)
		return
	}
	oid, err := strconv.ParseInt(oidParam, 10, 64)
	if err != nil || oid <= 0 {
		http.Error(w, "некорректный идентификатор преподавателя", http.StatusBadRequest)
		return
	}
	current, lessons, err := s.d.Schedule.WhereIsLecturer(r.Context(), oid, now)
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}
	subj := sched.LecturerSubject(oid)
	var rows []lessonRow
	for _, l := range lessons {
		rows = append(rows, s.lessonRow(l, subj))
	}
	todayKey := s.d.Clock.Today().Format("2006-01-02")
	markStatuses(rows, todayKey, todayKey, s.d.Clock.HHMM())
	data["Selected"] = sched.Lecturer{Oid: oid, Name: s.d.Schedule.LecturerName(r.Context(), oid, lessons)}
	data["SelectedPinned"] = pinned.Key() == subj.Key()
	data["Lessons"], data["InClass"] = rows, current != nil
	if current != nil {
		data["NowRoom"], data["NowPlace"] = roomShort(current.Auditorium), s.d.BuildingLabel(current.Building)
		data["NowTime"], data["NowWhat"] = current.BeginsAt+"—"+current.EndsAt, current.Discipline
		data["NowGroups"] = strings.Join(current.GroupNames, ", ")
		p, rem := progress(current.BeginsAt, current.EndsAt, s.d.Clock.HHMM())
		data["NowProgress"], data["NowRemain"], data["NowEnds"], data["NowBegins"] = p, rem, current.EndsAt, current.BeginsAt
	} else {
		// Честный ответ важнее красивого: вне пар источник ничего не знает.
		text := "Сегодня пар нет."
		hhmm := s.d.Clock.HHMM()
		for _, l := range lessons {
			if l.BeginsAt > hhmm {
				text = "Сейчас пары нет. Ближайшая в " + l.BeginsAt + "."
				break
			}
			text = "Пары на сегодня закончились."
		}
		data["IdleText"] = text
	}
	s.render(w, r, "lecturers", data)
}

// ─── подписи ─────────────────────────────────────────────────────────────────

// shortKind: «Практические (семинарские) занятия» в карточке не помещается.
func shortKind(k string) string {
	switch {
	case strings.Contains(k, "Практические"), strings.Contains(k, "семинар"):
		return "Семинар"
	case strings.Contains(k, "Лекц"):
		return "Лекция"
	case strings.Contains(k, "Лаборатор"):
		return "Лабораторная"
	case strings.Contains(k, "Консультация"):
		return "Консультация"
	case strings.Contains(k, "Экзамен"):
		return "Экзамен"
	case strings.Contains(k, "Зачет"), strings.Contains(k, "Зачёт"):
		return "Зачёт"
	}
	return k
}

// roomShort оставляет номер: корпус показывается отдельно, а «ауд.» перед
// номером — слово источника, на экране оно только занимает место.
func roomShort(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
		name = name[i+1:]
	}
	return strings.TrimSpace(strings.TrimPrefix(name, "ауд."))
}
