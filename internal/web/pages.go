package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

// ——— Расписание группы ———

type dayView struct {
	Dow  string
	Num  string
	Href string
	On   bool
	Has  bool
}

type lessonRow struct {
	BeginsAt     string
	EndsAt       string
	Discipline   string
	KindOfWork   string
	LecturerName string
	Room         string
	Place        string
}

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	subject, err := SubjectFromQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Закрепление: параметр pin сохраняет выбор и уводит на чистый адрес,
	// чтобы ссылка в истории браузера не повторяла действие при каждом
	// возврате назад.
	if !subject.IsZero() && r.URL.Query().Get("pin") == "1" {
		SetSubjectCookie(w, subject, r.TLS != nil)
		http.Redirect(w, r, "/schedule?"+subject.Query(), http.StatusSeeOther)
		return
	}
	if r.URL.Query().Get("unpin") == "1" {
		ClearSubjectCookie(w, r.TLS != nil)
		http.Redirect(w, r, "/schedule", http.StatusSeeOther)
		return
	}
	// Без параметров показываем закреплённое ранее.
	if subject.IsZero() {
		subject = SubjectFromCookie(r)
	}

	data := map[string]any{
		"Title": "Расписание", "Tab": "schedule",
		"Subtitle":  "Выберите группу или преподавателя",
		"Freshness": s.freshness(r),
	}
	if subject.IsZero() {
		s.render(w, "schedule", data)
		return
	}

	date := s.now()
	if v := r.URL.Query().Get("date"); v != "" {
		parsed, err := time.ParseInLocation("2006-01-02", v, s.loc)
		if err != nil {
			http.Error(w, "дата должна быть в формате ГГГГ-ММ-ДД", http.StatusBadRequest)
			return
		}
		date = parsed
	}

	// Неделя показывается целиком, от понедельника: так виден и сегодняшний
	// день, и то, что будет дальше, без лишних переходов.
	weekStart := startOfWeek(date)
	weekEnd := weekStart.AddDate(0, 0, 5) // пн—сб; воскресенье у вуза почти пустое

	var lessons []store.LessonView
	switch subject.Kind {
	case SubjectGroup:
		lessons, err = s.store.ScheduleForGroup(r.Context(), subject.Group, weekStart, weekEnd)
		subject.Label = subject.Group
	case SubjectLecturer:
		lessons, err = s.store.ScheduleForLecturer(r.Context(), subject.LecturerOid, weekStart, weekEnd)
		subject.Label = s.lecturerName(r, subject.LecturerOid, lessons)
	}
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}

	hasLessons := map[string]bool{}
	for _, l := range lessons {
		hasLessons[l.Date.Format("2006-01-02")] = true
	}

	var days []dayView
	for i := 0; i < 6; i++ {
		d := weekStart.AddDate(0, 0, i)
		key := d.Format("2006-01-02")
		days = append(days, dayView{
			Dow:  WeekdayShortRu(d),
			Num:  strconv.Itoa(d.Day()),
			Href: "/schedule?" + subject.Query() + "&date=" + key,
			On:   key == date.Format("2006-01-02"),
			Has:  hasLessons[key],
		})
	}

	var rows []lessonRow
	for _, l := range lessons {
		if l.Date.Format("2006-01-02") != date.Format("2006-01-02") {
			continue
		}
		row := lessonRow{
			BeginsAt: l.BeginsAt, EndsAt: l.EndsAt,
			Discipline: l.Discipline, KindOfWork: shortKind(l.KindOfWork),
			LecturerName: l.LecturerName,
			Room:         roomShort(l.Auditorium),
			Place:        placeOf(l),
		}
		// У преподавателя в строке пары полезен состав групп, а не его
		// собственное имя: оно и так в заголовке страницы.
		if subject.Kind == SubjectLecturer {
			row.LecturerName = strings.Join(l.Groups, ", ")
		}
		rows = append(rows, row)
	}

	pinned := SubjectFromCookie(r)
	data["Pinned"] = pinned.Key() == subject.Key()
	data["PinHref"] = "/schedule?" + subject.Query() + "&pin=1"
	data["Subject"] = subject
	data["SubjectKey"] = subject.Key()
	data["SubjectQuery"] = subject.Query()
	data["Group"] = subject.Label
	data["IsLecturer"] = subject.Kind == SubjectLecturer
	data["ChangeHref"] = changeHref(subject)
	data["Subtitle"] = FormatDateRu(date) + " · " + WeekdayRu(date)
	data["Days"] = days
	data["Lessons"] = rows
	s.render(w, "schedule", data)
}

// changeHref ведёт туда, где меняют закреплённое расписание: студента — к
// списку групп, преподавателя — к поиску по фамилии.
func changeHref(s Subject) string {
	if s.Kind == SubjectLecturer {
		return "/lecturers"
	}
	return "/groups"
}

// lecturerName определяет, как подписать страницу.
//
// Имя берётся из пар, а если их нет — из справочника: у преподавателя
// может не быть занятий на этой неделе, и страница всё равно должна быть
// подписана его фамилией, а не номером.
func (s *Server) lecturerName(r *http.Request, oid int64, lessons []store.LessonView) string {
	for _, l := range lessons {
		if l.LecturerName != "" {
			return l.LecturerName
		}
	}
	if name, err := s.store.LecturerName(r.Context(), oid); err == nil && name != "" {
		return name
	}
	return "Преподаватель"
}

func startOfWeek(t time.Time) time.Time {
	offset := (int(t.Weekday()) + 6) % 7 // понедельник — начало недели
	d := t.AddDate(0, 0, -offset)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, t.Location())
}

// shortKind сокращает казённое название вида занятия: «Практические
// (семинарские) занятия» в карточке не помещается и ничего не добавляет.
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

// roomShort оставляет от имени аудитории её номер: корпус показывается
// отдельно, а «ЛП49/2/313» в строке занимает место без пользы.
func roomShort(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 && i+1 < len(name) {
		return name[i+1:]
	}
	return name
}

func placeOf(l store.LessonView) string {
	place := ShortBuilding(l.Building)
	if l.Floor != nil {
		place += " · " + strconv.Itoa(*l.Floor) + " эт"
	}
	return place
}

// ——— Выбор группы ———

type groupRow struct {
	Name        string
	NameEscaped string
	Meta        string
	On          bool
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	course := r.URL.Query().Get("course")
	chosen := r.URL.Query().Get("group")

	found, err := s.store.SearchGroups(r.Context(), query, 200)
	if err != nil {
		http.Error(w, "не удалось найти группы", http.StatusInternalServerError)
		return
	}

	academicYear := academicYearOf(s.now())
	var rows []groupRow
	for _, g := range found {
		c := courseOf(g, academicYear)
		if course != "" {
			n, err := strconv.Atoi(course)
			if err != nil || c != n {
				continue
			}
		}
		meta := "факультет " + g.FacultyOid
		if c > 0 {
			meta = strconv.Itoa(c) + " курс · " + meta
		}
		rows = append(rows, groupRow{
			Name: g.Name, NameEscaped: url.QueryEscape(g.Name),
			Meta: meta, On: g.Name == chosen,
		})
	}

	base := "/groups?q=" + url.QueryEscape(query)
	chips := []chipView{{Label: "все", Href: base, On: course == ""}}
	for i := 1; i <= 5; i++ {
		v := strconv.Itoa(i)
		chips = append(chips, chipView{
			Label: v, Href: base + "&course=" + v, On: course == v,
		})
	}

	s.render(w, "groups", map[string]any{
		"Title": "Выбор группы", "Tab": "schedule",
		"Query": query, "Courses": chips, "Groups": rows,
		"ResultLabel": fmt.Sprintf("найдено: %d", len(rows)),
	})
}

// academicYearOf определяет учебный год: он начинается в сентябре, поэтому
// с января по август номер года на единицу меньше календарного.
func academicYearOf(t time.Time) int {
	if int(t.Month()) >= 9 {
		return t.Year()
	}
	return t.Year() - 1
}

func courseOf(g store.Group, academicYear int) int {
	if g.AdmissionYear == nil {
		return 0
	}
	c := academicYear - *g.AdmissionYear + 1
	if c < 1 || c > 6 {
		return 0
	}
	return c
}

// ——— Преподаватели ———

func (s *Server) handleLecturers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	clock := NowClock(s.loc)

	data := map[string]any{
		"Title": "Преподаватели", "Tab": "lecturers",
		"Query": query, "Clock": clock,
		"Subtitle": "Где преподаватель сейчас",
	}

	oidParam := r.URL.Query().Get("oid")
	if oidParam == "" {
		if query != "" {
			found, err := s.store.SearchLecturers(r.Context(), query, 30)
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

	today := s.now()
	lessons, err := s.store.ScheduleForLecturer(r.Context(), oid, today, today)
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}

	name := query
	if len(lessons) > 0 {
		name = lessons[0].LecturerName
	} else {
		found, err := s.store.SearchLecturers(r.Context(), query, 30)
		if err == nil {
			for _, l := range found {
				if l.Oid == oid {
					name = l.Name
				}
			}
		}
	}

	var rows []lessonRow
	var now *store.LessonView
	for i, l := range lessons {
		if l.BeginsAt <= clock && clock < l.EndsAt {
			now = &lessons[i]
		}
		rows = append(rows, lessonRow{
			BeginsAt: l.BeginsAt, EndsAt: l.EndsAt,
			Discipline: l.Discipline, KindOfWork: shortKind(l.KindOfWork),
			Room:  roomShort(l.Auditorium),
			Place: placeOf(l),
		})
	}

	data["Selected"] = store.Lecturer{Oid: oid, Name: name}
	data["Today"] = FormatDateRu(today) + " · " + WeekdayRu(today)
	data["Lessons"] = rows
	data["InClass"] = now != nil
	if now != nil {
		data["NowRoom"] = roomShort(now.Auditorium)
		data["NowPlace"] = placeOf(*now)
		data["NowTime"] = now.BeginsAt + "—" + now.EndsAt
		data["NowWhat"] = now.Discipline
		data["NowGroups"] = strings.Join(now.Groups, ", ")
	} else {
		// Честный ответ важнее красивого: вне пар источник о человеке
		// ничего не сообщает, и делать вид, что мы знаем больше, нельзя.
		next := ""
		for _, l := range lessons {
			if l.BeginsAt > clock {
				next = l.BeginsAt
				break
			}
		}
		switch {
		case next != "":
			data["IdleText"] = "Сейчас пары нет. Ближайшая в " + next + "."
		case len(lessons) > 0:
			data["IdleText"] = "Пары на сегодня закончились."
		default:
			data["IdleText"] = "Сегодня пар нет."
		}
	}
	s.render(w, "lecturers", data)
}
