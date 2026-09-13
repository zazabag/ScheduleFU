package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// UpsertAuditoriumsFromLessons пополняет справочник аудиторий из слепка.
//
// Отдельных запросов к источнику для этого не нужно: в каждой паре уже есть
// название аудитории, адрес и вместимость. Разбор (этаж, кампус) делает
// пакет ruz, здесь только сохранение.
func (s *Store) UpsertAuditoriumsFromLessons(ctx context.Context, lessons []Lesson) error {
	type row struct {
		oid      int64
		name     string
		building string
		capacity *int
	}
	uniq := map[int64]row{}
	for _, l := range lessons {
		if l.AuditoriumOid == nil {
			continue
		}
		r := row{oid: *l.AuditoriumOid, name: l.Auditorium, building: l.Building}
		uniq[r.oid] = r
	}
	if len(uniq) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for _, r := range uniq {
			a := parseForStore(r.name, r.building)
			// ParseAuditorium разбирает только имя и адрес; идентификатор
			// известен из пары и проставляется здесь.
			a.Oid = r.oid
			_, err := tx.Exec(ctx, `
				INSERT INTO auditoriums (oid, name, prefix, room, building, campus,
				                         site, site_label, site_order,
				                         floor, is_study_space, last_seen_at)
				VALUES ($1,$2,$3,$4,$5,$6,$9,$10,$11,$7,$8, now())
				ON CONFLICT (oid) DO UPDATE SET
					name = EXCLUDED.name,
					prefix = EXCLUDED.prefix,
					room = EXCLUDED.room,
					building = EXCLUDED.building,
					campus = EXCLUDED.campus,
					site = EXCLUDED.site,
					site_label = EXCLUDED.site_label,
					site_order = EXCLUDED.site_order,
					floor = EXCLUDED.floor,
					last_seen_at = now()`,
				a.Oid, a.Name, a.Prefix, a.Room, a.Building, a.Campus, a.Floor,
				a.IsStudySpace, a.Site, a.SiteLabel, a.SiteOrder)
			if err != nil {
				return fmt.Errorf("сохранение аудитории %d: %w", r.oid, err)
			}
		}
		return nil
	})
}

// UpsertLecturersFromLessons пополняет справочник преподавателей из слепка.
func (s *Store) UpsertLecturersFromLessons(ctx context.Context, lessons []Lesson) error {
	uniq := map[int64]string{}
	for _, l := range lessons {
		if l.LecturerOid != nil && l.LecturerName != "" {
			uniq[*l.LecturerOid] = l.LecturerName
		}
	}
	if len(uniq) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for oid, name := range uniq {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lecturers (oid, name, last_seen_at) VALUES ($1,$2, now())
				ON CONFLICT (oid) DO UPDATE SET name = EXCLUDED.name, last_seen_at = now()`,
				oid, name); err != nil {
				return fmt.Errorf("сохранение преподавателя %d: %w", oid, err)
			}
		}
		return nil
	})
}

// UpsertAuditoriums сохраняет аудитории из внешнего справочника
// (data/auditoriums.json): там есть тип и вместимость, которых в парах нет.
func (s *Store) UpsertAuditoriums(ctx context.Context, auds []Auditorium) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for _, a := range auds {
			_, err := tx.Exec(ctx, `
				INSERT INTO auditoriums (oid, name, prefix, room, building, campus,
				                         site, site_label, site_order,
				                         kind, floor, capacity, is_study_space, last_seen_at)
				VALUES ($1,$2,$3,$4,$5,$6,$11,$12,$13,$7,$8,$9,$10, now())
				ON CONFLICT (oid) DO UPDATE SET
					name = EXCLUDED.name, prefix = EXCLUDED.prefix, room = EXCLUDED.room,
					building = EXCLUDED.building, campus = EXCLUDED.campus,
					site = EXCLUDED.site, site_label = EXCLUDED.site_label,
					site_order = EXCLUDED.site_order,
					kind = EXCLUDED.kind, floor = EXCLUDED.floor,
					-- Вместимость известна не всегда; уже известную не затираем.
					capacity = COALESCE(EXCLUDED.capacity, auditoriums.capacity),
					is_study_space = EXCLUDED.is_study_space,
					last_seen_at = now()`,
				a.Oid, a.Name, a.Prefix, a.Room, a.Building, a.Campus, a.Kind,
				a.Floor, a.Capacity, a.IsStudySpace, a.Site, a.SiteLabel, a.SiteOrder)
			if err != nil {
				return fmt.Errorf("сохранение аудитории %d: %w", a.Oid, err)
			}
		}
		return nil
	})
}

// UpsertGroups сохраняет справочник групп.
func (s *Store) UpsertGroups(ctx context.Context, groups []Group) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for _, g := range groups {
			if _, err := tx.Exec(ctx, `
				INSERT INTO groups (id, name, faculty_oid, admission_year, last_seen_at)
				VALUES ($1,$2,$3,$4, now())
				ON CONFLICT (id) DO UPDATE SET
					name = EXCLUDED.name, faculty_oid = EXCLUDED.faculty_oid,
					admission_year = EXCLUDED.admission_year, last_seen_at = now()`,
				g.ID, g.Name, g.FacultyOid, g.AdmissionYear); err != nil {
				return fmt.Errorf("сохранение группы %d: %w", g.ID, err)
			}
		}
		return nil
	})
}

// AuditoriumOids возвращает аудитории, которые нужно опрашивать.
// onlyStudySpaces отсекает спортзалы и чужие помещения.
func (s *Store) AuditoriumOids(ctx context.Context, onlyStudySpaces bool) ([]int64, error) {
	q := `SELECT oid FROM auditoriums`
	if onlyStudySpaces {
		q += ` WHERE is_study_space`
	}
	q += ` ORDER BY oid`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("чтение списка аудиторий: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var oid int64
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		out = append(out, oid)
	}
	return out, rows.Err()
}

// StartRun открывает запись о проходе сборщика.
func (s *Store) StartRun(ctx context.Context, from, to time.Time) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO collector_runs (period_from, period_to) VALUES ($1,$2) RETURNING id`,
		from, to).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("начало прохода: %w", err)
	}
	return id, nil
}

// FinishRun закрывает запись о проходе.
func (s *Store) FinishRun(ctx context.Context, id int64, requests, errs, lessons, changes int, failure error) error {
	var msg *string
	if failure != nil {
		m := failure.Error()
		msg = &m
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE collector_runs
		   SET finished_at = now(), requests = $2, errors = $3,
		       lessons_seen = $4, changes_found = $5, failure = $6
		 WHERE id = $1`, id, requests, errs, lessons, changes, msg)
	return err
}

// LastSuccessfulRun возвращает время последнего удачного прохода — по нему
// в интерфейсе показывается свежесть данных.
func (s *Store) LastSuccessfulRun(ctx context.Context) (time.Time, bool, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT finished_at FROM collector_runs
		 WHERE finished_at IS NOT NULL AND failure IS NULL
		 ORDER BY finished_at DESC LIMIT 1`).Scan(&at)
	if err == pgx.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return at, true, nil
}
