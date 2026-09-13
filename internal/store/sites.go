package store

import (
	"context"
	"fmt"
	"time"
)

// SiteRow — площадка для переключателя.
type SiteRow struct {
	Site  string
	Label string
	Rooms int
	Order int
}

// branchSite — ключ филиалов в других городах.
//
// Они исключаются из выдачи: студенту, который ищет, где сесть между
// парами, дойти до Омского филиала невозможно, а переключатель они
// занимали целиком.
const branchSite = "branch"

// Sites возвращает московские площадки с числом учебных аудиторий.
//
// Группировка идёт по site, а не по campus: campus отвечает лишь на вопрос
// «Ленинградский или нет», и двенадцать прочих адресов схлопывались в одно
// значение — фильтр по нему показывал Масловку, Вешняковский и ещё десять
// зданий вперемешку.
func (s *Store) Sites(ctx context.Context) ([]SiteRow, error) {
	const q = `
		SELECT site, min(site_label), count(*), min(site_order)
		  FROM auditoriums
		 WHERE is_study_space AND site <> $1
		 GROUP BY site
		 ORDER BY min(site_order), count(*) DESC`
	rows, err := s.pool.Query(ctx, q, branchSite)
	if err != nil {
		return nil, fmt.Errorf("список площадок: %w", err)
	}
	defer rows.Close()

	var out []SiteRow
	for rows.Next() {
		var r SiteRow
		if err := rows.Scan(&r.Site, &r.Label, &r.Rooms, &r.Order); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SiteDay отдаёт учебные аудитории площадки вместе с парами на дату.
//
// Один запрос на весь экран: рисовать полосу занятости отдельным запросом
// по каждой аудитории — это сотни обращений к базе на одну страницу.
func (s *Store) SiteDay(ctx context.Context, site string, date time.Time) ([]RoomDay, error) {
	const q = `
		SELECT a.oid, a.name, a.room, a.building, a.campus, a.site, a.site_label,
		       a.kind, a.floor, a.capacity,
		       l.lesson_oid, l.lesson_date,
		       to_char(l.begins_at, 'HH24:MI'), to_char(l.ends_at, 'HH24:MI'),
		       l.discipline, l.kind_of_work, l.lecturer_name, l.group_names
		  FROM auditoriums a
		  LEFT JOIN lessons l
		         ON l.auditorium_oid = a.oid AND l.lesson_date = $2::date
		 WHERE a.is_study_space
		   AND a.site <> $3
		   AND ($1 = '' OR a.site = $1)
		 ORDER BY a.floor NULLS LAST, a.room, l.begins_at`

	rows, err := s.pool.Query(ctx, q, site, date.Format("2006-01-02"), branchSite)
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
		if err := rows.Scan(&a.Oid, &a.Name, &a.Room, &a.Building, &a.Campus,
			&a.Site, &a.SiteLabel, &a.Kind, &a.Floor, &a.Capacity,
			&lessonOid, &lessonDate, &begins, &ends,
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

// LecturerName возвращает имя преподавателя из справочника.
func (s *Store) LecturerName(ctx context.Context, oid int64) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM lecturers WHERE oid = $1`, oid).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}
