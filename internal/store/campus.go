package store

import (
	"context"
	"fmt"
)

// RoomDay — аудитория вместе с её парами на дату.
type RoomDay struct {
	Auditorium
	Lessons []LessonView
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
