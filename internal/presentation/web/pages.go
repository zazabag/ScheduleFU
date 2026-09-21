package web

import (
	"fmt"
	"net/http"
	"net/url"
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
		tabs = append(tabs, chip{Label: x.Site.Label, Href: "/?site=" + url.QueryEscape(x.Site.Slug), On: x.Site.Slug == site})
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
		rows = append(rows, row{Room: v.Auditorium.Room, Meta: s.roomMeta(v.Auditorium), Until: until, UntilClass: cls, Cells: v.Cells, Lessons: v.Lessons})
	}
	floors := make([]int, 0, len(floorSet))
	for f := range floorSet {
		floors = append(floors, f)
	}
	sort.Ints(floors)
	chips := []chip{{Label: "все", Href: "/?site=" + url.QueryEscape(site), On: floor == ""}}
	for _, f := range floors {
		v := strconv.Itoa(f)
		chips = append(chips, chip{Label: v, Href: "/?site=" + url.QueryEscape(site) + "&floor=" + v, On: floor == v})
	}

	s.render(w, "rooms", map[string]any{
		"Title": "Свободные аудитории", "Tab": "rooms", "Clock": s.d.Clock.HHMM(),
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

type lessonRow struct{ BeginsAt, EndsAt, Discipline, KindOfWork, LecturerName, Room, Place string }

func (s *Server) lessonRow(l sched.Lesson, subj sched.Subject) lessonRow {
	row := lessonRow{BeginsAt: l.BeginsAt, EndsAt: l.EndsAt, Discipline: l.Discipline,
		KindOfWork: shortKind(l.KindOfWork), LecturerName: l.LecturerName, Room: roomShort(l.Auditorium)}
	row.Place = s.d.BuildingLabel(l.Building)
	// У преподавателя в строке пары полезен состав групп, а не его имя.
	if subj.Kind == sched.SubjectLecturer {
		row.LecturerName = strings.Join(l.GroupNames, ", ")
	}
	return row
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	subj, err := sched.SubjectFromValues(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	secure := r.TLS != nil
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
	data := map[string]any{"Title": "Расписание", "Tab": "schedule", "Freshness": s.freshness(r),
		"Subtitle": "Выберите группу или преподавателя"}
	if subj.IsZero() {
		s.render(w, "schedule", data)
		return
	}

	date := s.d.Clock.Today()
	if v := r.URL.Query().Get("date"); v != "" {
		if date, err = time.ParseInLocation("2006-01-02", v, s.d.Clock.Location()); err != nil {
			http.Error(w, "дата должна быть в формате ГГГГ-ММ-ДД", http.StatusBadRequest)
			return
		}
	}
	// Неделя целиком от понедельника: и сегодня, и дальше без переходов.
	weekStart := clock.StartOfWeek(date)
	lessons, err := s.d.Schedule.ScheduleFor(r.Context(), subj, weekStart, weekStart.AddDate(0, 0, 5))
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}
	label := subj.Group
	if subj.Kind == sched.SubjectLecturer {
		label = s.d.Schedule.LecturerName(r.Context(), subj.LecturerOid, lessons)
	}

	has := map[string]bool{}
	for _, l := range lessons {
		has[l.DateKey()] = true
	}
	type dayView struct {
		Dow, Num, Href string
		On, Has        bool
	}
	var days []dayView
	for i := 0; i < 6; i++ {
		d := weekStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		days = append(days, dayView{Dow: clock.WeekdayShortRu(d), Num: strconv.Itoa(d.Day()),
			Href: "/schedule?" + subj.Query() + "&date=" + key, On: key == date.Format("2006-01-02"), Has: has[key]})
	}
	var rows []lessonRow
	for _, l := range lessons {
		if l.DateKey() == date.Format("2006-01-02") {
			rows = append(rows, s.lessonRow(l, subj))
		}
	}
	pinned := SubjectFromCookie(r)
	data["Group"], data["IsLecturer"] = label, subj.Kind == sched.SubjectLecturer
	data["Subtitle"] = clock.DateRu(date) + " · " + clock.WeekdayRu(date)
	data["Days"], data["Lessons"] = days, rows
	data["SubjectKey"], data["SubjectQuery"] = subj.Key(), subj.Query()
	data["Pinned"], data["PinHref"] = pinned.Key() == subj.Key(), "/schedule?"+subj.Query()+"&pin=1"
	data["ChangeHref"] = map[bool]string{true: "/lecturers", false: "/groups"}[subj.Kind == sched.SubjectLecturer]
	data["PushEnabled"] = s.d.Notify != nil && s.d.Notify.Enabled()
	s.render(w, "schedule", data)
}

// ─── выбор группы ────────────────────────────────────────────────────────────

func (s *Server) groups(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	course := r.URL.Query().Get("course")
	chosen := SubjectFromCookie(r)
	found, err := s.d.Schedule.Repo().SearchGroups(r.Context(), q, 200)
	if err != nil {
		http.Error(w, "не удалось найти группы", http.StatusInternalServerError)
		return
	}
	year := academicYear(s.d.Clock.Now())
	type row struct {
		Name, NameEscaped, Meta string
		On                      bool
	}
	var rows []row
	for _, g := range found {
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
	base := "/groups?q=" + url.QueryEscape(q)
	chips := []chip{{Label: "все", Href: base, On: course == ""}}
	for i := 1; i <= 5; i++ {
		v := strconv.Itoa(i)
		chips = append(chips, chip{Label: v, Href: base + "&course=" + v, On: course == v})
	}
	s.render(w, "groups", map[string]any{"Title": "Выбор группы", "Tab": "schedule", "Query": q,
		"Courses": chips, "Groups": rows, "ResultLabel": fmt.Sprintf("найдено: %d", len(rows))})
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
	data := map[string]any{"Title": "Преподаватели", "Tab": "lecturers", "Query": q,
		"Clock": s.d.Clock.HHMM(), "Subtitle": "Где преподаватель сейчас"}
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
		s.render(w, "lecturers", data)
		return
	}
	oid, err := strconv.ParseInt(oidParam, 10, 64)
	if err != nil || oid <= 0 {
		http.Error(w, "некорректный идентификатор преподавателя", http.StatusBadRequest)
		return
	}
	now := s.d.Clock.Now()
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
	data["Selected"] = sched.Lecturer{Oid: oid, Name: s.d.Schedule.LecturerName(r.Context(), oid, lessons)}
	data["Today"] = clock.DateRu(now) + " · " + clock.WeekdayRu(now)
	data["Lessons"], data["InClass"] = rows, current != nil
	if current != nil {
		data["NowRoom"], data["NowPlace"] = roomShort(current.Auditorium), s.d.BuildingLabel(current.Building)
		data["NowTime"], data["NowWhat"] = current.BeginsAt+"—"+current.EndsAt, current.Discipline
		data["NowGroups"] = strings.Join(current.GroupNames, ", ")
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
	s.render(w, "lecturers", data)
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

// roomShort оставляет номер: корпус показывается отдельно.
func roomShort(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}
