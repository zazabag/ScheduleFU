// Package export — выгрузка расписания наружу: календарь ICS.
//
// Читает schedule через порт, знает только его домен.
package export

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/export/ical"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// ScheduleReader — то, что export хочет от schedule.
type ScheduleReader interface {
	ScheduleFor(ctx context.Context, s sched.Subject, from, to time.Time) ([]sched.Lesson, error)
	LecturerName(ctx context.Context, oid int64, lessons []sched.Lesson) string
}

// RoomLabeler даёт короткие подписи корпусов; живёт у площадок источника.
type RoomLabeler func(building string) string

// Calendar собирает календарь владельца.
//
// Две недели назад и месяц вперёд: назад — чтобы оставалась прошедшая
// неделя, вперёд — насколько вуз вообще публикует. Больше — уже копирование
// базы, а не расписание для человека.
func Calendar(ctx context.Context, r ScheduleReader, subj sched.Subject, now time.Time, loc *time.Location, label RoomLabeler) (string, error) {
	from, to := now.AddDate(0, 0, -14), now.AddDate(0, 0, 30)
	lessons, err := r.ScheduleFor(ctx, subj, from, to)
	if err != nil {
		return "", err
	}
	name := subj.Group
	if subj.Kind == sched.SubjectLecturer {
		name = r.LecturerName(ctx, subj.LecturerOid, lessons)
	}
	cal := ical.Calendar{
		Name:        "Расписание " + name,
		Description: "ScheduleFU · данные из расписания Финансового университета",
		Location:    loc,
		Events:      make([]ical.Event, 0, len(lessons)),
	}
	for _, l := range lessons {
		start, end, ok := lessonTimes(l, loc)
		if !ok {
			continue // пара с нечитаемым временем стала бы событием на полночь
		}
		cal.Events = append(cal.Events, ical.Event{
			// Устойчивый UID: по нему календарь понимает, что это та же пара.
			// Иначе при каждом обновлении подписки события дублировались бы.
			UID:         fmt.Sprintf("lesson-%d@schedulefu", l.LessonOid),
			Start:       start,
			End:         end,
			Summary:     l.Discipline,
			Location:    place(l, label),
			Description: details(l, subj),
		})
	}
	return cal.Render(time.Now()), nil
}

func lessonTimes(l sched.Lesson, loc *time.Location) (time.Time, time.Time, bool) {
	b, err1 := time.ParseInLocation("15:04", l.BeginsAt, loc)
	e, err2 := time.ParseInLocation("15:04", l.EndsAt, loc)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, false
	}
	y, m, d := l.Date.Date()
	start := time.Date(y, m, d, b.Hour(), b.Minute(), 0, 0, loc)
	finish := time.Date(y, m, d, e.Hour(), e.Minute(), 0, 0, loc)
	return start, finish, finish.After(start)
}

func place(l sched.Lesson, label RoomLabeler) string {
	room := l.Auditorium
	if i := strings.LastIndex(room, "/"); i >= 0 && i+1 < len(room) {
		room = room[i+1:]
	}
	parts := []string{room}
	if label != nil {
		if p := label(l.Building); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

// details: студенту важен преподаватель, преподавателю — состав групп.
func details(l sched.Lesson, subj sched.Subject) string {
	var parts []string
	if k := strings.TrimSpace(l.KindOfWork); k != "" {
		parts = append(parts, k)
	}
	if subj.Kind == sched.SubjectLecturer {
		if g := strings.Join(l.GroupNames, ", "); g != "" {
			parts = append(parts, g)
		}
	} else if l.LecturerName != "" {
		parts = append(parts, l.LecturerName)
	}
	return strings.Join(parts, " · ")
}
