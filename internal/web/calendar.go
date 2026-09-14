package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/ical"
	"github.com/zazabag/schedulefu/internal/store"
)

// handleCalendar отдаёт расписание в формате календарей.
//
// Ссылка рассчитана на подписку, а не на разовое скачивание: календарь
// перечитывает её раз в час и сам показывает изменения, внесённые вузом.
// Поэтому ответ отдаётся свежим всегда, без кэширования.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	subject, err := SubjectFromQuery(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if subject.IsZero() {
		subject = SubjectFromCookie(r)
	}
	if subject.IsZero() {
		http.Error(w, "не указано, чьё расписание выгружать", http.StatusBadRequest)
		return
	}

	now := s.now()
	// Две недели назад и месяц вперёд: назад — чтобы в календаре
	// оставалась только что прошедшая неделя, вперёд — насколько вуз
	// вообще публикует расписание. Больше выгружать незачем, это уже
	// копирование базы, а не расписание для человека.
	from := now.AddDate(0, 0, -14)
	to := now.AddDate(0, 0, 30)

	var lessons []store.LessonView
	var name string
	switch subject.Kind {
	case SubjectGroup:
		lessons, err = s.store.ScheduleForGroup(r.Context(), subject.Group, from, to)
		name = subject.Group
	case SubjectLecturer:
		lessons, err = s.store.ScheduleForLecturer(r.Context(), subject.LecturerOid, from, to)
		name = s.lecturerName(r, subject.LecturerOid, lessons)
	}
	if err != nil {
		http.Error(w, "не удалось получить расписание", http.StatusInternalServerError)
		return
	}

	cal := ical.Calendar{
		Name:        "Расписание " + name,
		Description: "ScheduleFU · данные из расписания Финансового университета",
		Location:    s.loc,
		Events:      make([]ical.Event, 0, len(lessons)),
	}
	for _, l := range lessons {
		start, end, ok := lessonTimes(l, s.loc)
		if !ok {
			// Пара с нечитаемым временем в календаре превратилась бы в
			// событие на полночь: лучше пропустить.
			continue
		}
		cal.Events = append(cal.Events, ical.Event{
			// UID должен быть устойчивым: по нему календарь понимает, что
			// это та же пара, а не новая. Иначе при каждом обновлении
			// подписки события дублировались бы.
			UID:         fmt.Sprintf("lesson-%d@schedulefu", l.LessonOid),
			Start:       start,
			End:         end,
			Summary:     l.Discipline,
			Location:    calendarPlace(l),
			Description: calendarDetails(l, subject),
		})
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`inline; filename="schedulefu.ics"`)
	noStore(w)
	fmt.Fprint(w, cal.Render(time.Now()))
}

// lessonTimes собирает начало и конец пары в один момент времени.
func lessonTimes(l store.LessonView, loc *time.Location) (time.Time, time.Time, bool) {
	begin, err := time.ParseInLocation("15:04", l.BeginsAt, loc)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	end, err := time.ParseInLocation("15:04", l.EndsAt, loc)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := l.Date.Date()
	start := time.Date(y, m, d, begin.Hour(), begin.Minute(), 0, 0, loc)
	finish := time.Date(y, m, d, end.Hour(), end.Minute(), 0, 0, loc)
	if !finish.After(start) {
		return time.Time{}, time.Time{}, false
	}
	return start, finish, true
}

func calendarPlace(l store.LessonView) string {
	parts := []string{roomShort(l.Auditorium)}
	if p := ShortBuilding(l.Building); p != "" {
		parts = append(parts, p)
	}
	if l.Floor != nil {
		parts = append(parts, fmt.Sprintf("%d этаж", *l.Floor))
	}
	return strings.Join(parts, ", ")
}

// calendarDetails пишет в описание то, чего нет в названии.
//
// Студенту важен преподаватель, преподавателю — состав групп: одно и то же
// событие читается ими по-разному.
func calendarDetails(l store.LessonView, subject Subject) string {
	var parts []string
	if k := shortKind(l.KindOfWork); k != "" {
		parts = append(parts, k)
	}
	if subject.Kind == SubjectLecturer {
		if g := strings.Join(l.Groups, ", "); g != "" {
			parts = append(parts, g)
		}
	} else if l.LecturerName != "" {
		parts = append(parts, l.LecturerName)
	}
	return strings.Join(parts, " · ")
}
