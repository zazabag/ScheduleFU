package store

import (
	"context"
	"fmt"
	"time"
)

// RoomDay — аудитория вместе с её парами на дату.
type RoomDay struct {
	Auditorium
	Lessons []LessonView
}

// CampusRow — площадка для переключателя.
type CampusRow struct {
	Campus   string
	Building string
	Rooms    int
}

// branchFilter отсекает филиалы в других городах.
//
// Источник держит в одном справочнике и московские корпуса, и филиалы —
// Пенза, Владимир, Челябинск и ещё десяток. Для студента, ищущего, где
// сесть между парами, это шум: дойти до Омского филиала он не сможет.
const branchFilter = `building NOT LIKE 'Филиалы%'`

// Campuses возвращает площадки с числом учебных аудиторий.
//
// Ленинградские адреса склеены в один кампус ещё на разборе (пакет ruz),
// поэтому здесь достаточно группировки по полю campus, а внутри «прочих» —
// по адресу: у них связи между зданиями нет и склеивать нечего.
func (s *Store) Campuses(ctx context.Context) ([]CampusRow, error) {
	const q = `
		SELECT campus,
		       CASE WHEN campus = 'leningradsky' THEN 'Ленинградский' ELSE building END AS label,
		       count(*) AS rooms
		  FROM auditoriums
		 WHERE is_study_space AND ` + branchFilter + `
		 GROUP BY 1, 2
		 ORDER BY rooms DESC`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("список площадок: %w", err)
	}
	defer rows.Close()

	var out []CampusRow
	for rows.Next() {
		var c CampusRow
		if err := rows.Scan(&c.Campus, &c.Building, &c.Rooms); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CampusDay отдаёт учебные аудитории площадки вместе с парами на дату.
//
// Один запрос на весь экран: рисовать полосу занятости по каждой аудитории
// отдельным запросом — это 37 обращений к базе на одну страницу.
// building непустой сужает выдачу до одного адреса внутри площадки.
func (s *Store) CampusDay(ctx context.Context, campus, building string, date time.Time) ([]RoomDay, error) {
	const q = `
		SELECT a.oid, a.name, a.room, a.building, a.campus, a.kind, a.floor, a.capacity,
		       l.lesson_oid, l.lesson_date,
		       to_char(l.begins_at, 'HH24:MI'), to_char(l.ends_at, 'HH24:MI'),
		       l.discipline, l.kind_of_work, l.lecturer_name, l.group_names
		  FROM auditoriums a
		  LEFT JOIN lessons l
		         ON l.auditorium_oid = a.oid AND l.lesson_date = $3::date
		 WHERE a.is_study_space
		   AND a.building NOT LIKE 'Филиалы%'
		   AND ($1 = '' OR a.campus = $1)
		   AND ($2 = '' OR a.building = $2)
		 ORDER BY a.building, a.floor NULLS LAST, a.room, l.begins_at`

	rows, err := s.pool.Query(ctx, q, campus, building, date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("день площадки: %w", err)
	}
	defer rows.Close()

	var (
		out     []RoomDay
		current *RoomDay
	)
	for rows.Next() {
		var (
			a                                    Auditorium
			lessonOid                            *int64
			lessonDate                           *time.Time
			begins, ends                         *string
			discipline, kindOfWork, lecturerName *string
			groups                               []string
		)
		if err := rows.Scan(&a.Oid, &a.Name, &a.Room, &a.Building, &a.Campus, &a.Kind,
			&a.Floor, &a.Capacity, &lessonOid, &lessonDate, &begins, &ends,
			&discipline, &kindOfWork, &lecturerName, &groups); err != nil {
			return nil, err
		}
		if current == nil || current.Oid != a.Oid {
			a.IsStudySpace = true
			out = append(out, RoomDay{Auditorium: a})
			current = &out[len(out)-1]
		}
		// LEFT JOIN даёт пустую строку для аудитории без пар — это не ошибка,
		// а самый интересный случай: такая свободна весь день.
		if lessonOid == nil {
			continue
		}
		current.Lessons = append(current.Lessons, LessonView{
			LessonOid:    *lessonOid,
			Date:         *lessonDate,
			BeginsAt:     deref(begins),
			EndsAt:       deref(ends),
			Discipline:   deref(discipline),
			KindOfWork:   deref(kindOfWork),
			LecturerName: deref(lecturerName),
			Auditorium:   a.Name,
			Building:     a.Building,
			Floor:        a.Floor,
			Groups:       groups,
		})
	}
	return out, rows.Err()
}

// SearchGroups ищет группы по подстроке названия.
func (s *Store) SearchGroups(ctx context.Context, query string, limit int) ([]Group, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, faculty_oid, admission_year
		  FROM groups
		 WHERE $1 = '' OR lower(name) LIKE '%' || lower($1) || '%'
		 ORDER BY name
		 LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск групп: %w", err)
	}
	defer rows.Close()

	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.FacultyOid, &g.AdmissionYear); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SearchLecturers ищет преподавателей по подстроке фамилии.
func (s *Store) SearchLecturers(ctx context.Context, query string, limit int) ([]Lecturer, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.pool.Query(ctx, `
		SELECT oid, name
		  FROM lecturers
		 WHERE $1 = '' OR lower(name) LIKE '%' || lower($1) || '%'
		 ORDER BY name
		 LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск преподавателей: %w", err)
	}
	defer rows.Close()

	var out []Lecturer
	for rows.Next() {
		var l Lecturer
		if err := rows.Scan(&l.Oid, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
