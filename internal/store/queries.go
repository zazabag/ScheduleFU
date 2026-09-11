package store

import (
	"context"
	"fmt"
	"time"
)

// FreeAuditorium — свободная аудитория в запрошенный момент.
type FreeAuditorium struct {
	Oid      int64
	Name     string
	Room     string
	Building string
	Campus   string
	Kind     string
	Floor    *int
	Capacity *int

	// FreeUntil — когда начнётся ближайшая пара. nil означает, что до конца
	// дня занятий нет.
	FreeUntil *string
}

// FreeAuditoriums возвращает аудитории, свободные в указанный момент.
//
// «Свободна» здесь означает ровно одно: в расписании нет пары, накрывающей
// этот момент. Это не то же самое, что «свободна на самом деле»:
// мероприятия, экзамены и ремонт в ruz.fa.ru не попадают, и знать о них мы
// не можем. Ограничение обязано быть видно пользователю в интерфейсе, см.
// docs/04-architecture-mvp.md.
func (s *Store) FreeAuditoriums(ctx context.Context, at time.Time, campus string) ([]FreeAuditorium, error) {
	date := at.Format("2006-01-02")
	clock := at.Format("15:04")

	// Аудитория свободна, если ни одна пара этого дня не накрывает момент.
	// Ближайшая пара после момента даёт границу «свободна до».
	const q = `
		SELECT a.oid, a.name, a.room, a.building, a.campus, a.kind, a.floor, a.capacity,
		       (SELECT to_char(min(l.begins_at), 'HH24:MI')
		          FROM lessons l
		         WHERE l.auditorium_oid = a.oid
		           AND l.lesson_date = $1::date
		           AND l.begins_at > $2::time) AS free_until
		  FROM auditoriums a
		 WHERE a.is_study_space
		   AND ($3 = '' OR a.campus = $3)
		   AND NOT EXISTS (
		       SELECT 1 FROM lessons l
		        WHERE l.auditorium_oid = a.oid
		          AND l.lesson_date = $1::date
		          AND l.begins_at <= $2::time
		          AND l.ends_at > $2::time
		   )
		 ORDER BY a.building, a.floor NULLS LAST, a.room`

	rows, err := s.pool.Query(ctx, q, date, clock, campus)
	if err != nil {
		return nil, fmt.Errorf("поиск свободных аудиторий: %w", err)
	}
	defer rows.Close()

	var out []FreeAuditorium
	for rows.Next() {
		var f FreeAuditorium
		if err := rows.Scan(&f.Oid, &f.Name, &f.Room, &f.Building, &f.Campus,
			&f.Kind, &f.Floor, &f.Capacity, &f.FreeUntil); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// LessonView — пара в том виде, в каком она показывается человеку.
type LessonView struct {
	LessonOid    int64
	Date         time.Time
	BeginsAt     string
	EndsAt       string
	Discipline   string
	KindOfWork   string
	Auditorium   string
	Building     string
	Floor        *int
	LecturerName string
	LecturerOid  *int64
	Groups       []string
	Subgroup     string
	Note         string
}

// ScheduleForGroup возвращает расписание группы за период.
func (s *Store) ScheduleForGroup(ctx context.Context, group string, from, to time.Time) ([]LessonView, error) {
	const q = `
		SELECT l.lesson_oid, l.lesson_date, to_char(l.begins_at,'HH24:MI'), to_char(l.ends_at,'HH24:MI'),
		       l.discipline, l.kind_of_work, l.auditorium, l.building, a.floor,
		       l.lecturer_name, l.lecturer_oid, l.group_names, l.subgroup, l.note
		  FROM lessons l
		  LEFT JOIN auditoriums a ON a.oid = l.auditorium_oid
		 WHERE $1 = ANY (l.group_names)
		   AND l.lesson_date BETWEEN $2 AND $3
		 ORDER BY l.lesson_date, l.begins_at`
	return s.queryLessons(ctx, q, group, from, to)
}

// ScheduleForLecturer возвращает расписание преподавателя за период.
func (s *Store) ScheduleForLecturer(ctx context.Context, lecturerOid int64, from, to time.Time) ([]LessonView, error) {
	const q = `
		SELECT l.lesson_oid, l.lesson_date, to_char(l.begins_at,'HH24:MI'), to_char(l.ends_at,'HH24:MI'),
		       l.discipline, l.kind_of_work, l.auditorium, l.building, a.floor,
		       l.lecturer_name, l.lecturer_oid, l.group_names, l.subgroup, l.note
		  FROM lessons l
		  LEFT JOIN auditoriums a ON a.oid = l.auditorium_oid
		 WHERE l.lecturer_oid = $1
		   AND l.lesson_date BETWEEN $2 AND $3
		 ORDER BY l.lesson_date, l.begins_at`
	return s.queryLessons(ctx, q, lecturerOid, from, to)
}

// WhereIsLecturer отвечает на вопрос «где преподаватель сейчас».
//
// Возвращает пару, идущую в указанный момент, если она есть.
func (s *Store) WhereIsLecturer(ctx context.Context, lecturerOid int64, at time.Time) (*LessonView, error) {
	const q = `
		SELECT l.lesson_oid, l.lesson_date, to_char(l.begins_at,'HH24:MI'), to_char(l.ends_at,'HH24:MI'),
		       l.discipline, l.kind_of_work, l.auditorium, l.building, a.floor,
		       l.lecturer_name, l.lecturer_oid, l.group_names, l.subgroup, l.note
		  FROM lessons l
		  LEFT JOIN auditoriums a ON a.oid = l.auditorium_oid
		 WHERE l.lecturer_oid = $1
		   AND l.lesson_date = $2::date
		   AND l.begins_at <= $3::time
		   AND l.ends_at > $3::time
		 LIMIT 1`
	rows, err := s.pool.Query(ctx, q, lecturerOid, at.Format("2006-01-02"), at.Format("15:04"))
	if err != nil {
		return nil, fmt.Errorf("поиск преподавателя: %w", err)
	}
	defer rows.Close()
	views, err := scanLessons(rows)
	if err != nil || len(views) == 0 {
		return nil, err
	}
	return &views[0], nil
}

// OccupancyForAuditorium возвращает занятость аудитории на дату.
func (s *Store) OccupancyForAuditorium(ctx context.Context, oid int64, date time.Time) ([]LessonView, error) {
	const q = `
		SELECT l.lesson_oid, l.lesson_date, to_char(l.begins_at,'HH24:MI'), to_char(l.ends_at,'HH24:MI'),
		       l.discipline, l.kind_of_work, l.auditorium, l.building, a.floor,
		       l.lecturer_name, l.lecturer_oid, l.group_names, l.subgroup, l.note
		  FROM lessons l
		  LEFT JOIN auditoriums a ON a.oid = l.auditorium_oid
		 WHERE l.auditorium_oid = $1 AND l.lesson_date = $2::date
		 ORDER BY l.begins_at`
	rows, err := s.pool.Query(ctx, q, oid, date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("занятость аудитории: %w", err)
	}
	defer rows.Close()
	return scanLessons(rows)
}

func (s *Store) queryLessons(ctx context.Context, q string, args ...any) ([]LessonView, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("чтение расписания: %w", err)
	}
	defer rows.Close()
	return scanLessons(rows)
}

type scannable interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanLessons(rows scannable) ([]LessonView, error) {
	var out []LessonView
	for rows.Next() {
		var v LessonView
		if err := rows.Scan(&v.LessonOid, &v.Date, &v.BeginsAt, &v.EndsAt,
			&v.Discipline, &v.KindOfWork, &v.Auditorium, &v.Building, &v.Floor,
			&v.LecturerName, &v.LecturerOid, &v.Groups, &v.Subgroup, &v.Note); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
