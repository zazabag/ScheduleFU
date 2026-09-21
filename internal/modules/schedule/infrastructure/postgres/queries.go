package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

// branchSite — филиалы в других городах: дойти до Омского филиала между
// парами нельзя, а переключатель они занимали целиком.
const branchSite = "branch"

// Sites — московские площадки с числом учебных аудиторий.
func (r *Repo) Sites(ctx context.Context) ([]schedule.SiteRow, error) {
	rows, err := r.pool.Query(ctx, `SELECT site, min(site_label), min(site_order), count(*)
		FROM auditoriums WHERE is_study_space AND site <> $1
		GROUP BY site ORDER BY min(site_order), count(*) DESC`, branchSite)
	if err != nil {
		return nil, fmt.Errorf("список площадок: %w", err)
	}
	defer rows.Close()
	var out []schedule.SiteRow
	for rows.Next() {
		var s schedule.SiteRow
		if err := rows.Scan(&s.Site.Slug, &s.Site.Label, &s.Site.Order, &s.Rooms); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SiteDay — учебные аудитории площадки с парами на дату, одним запросом:
// полоса занятости отдельным запросом на аудиторию — сотни обращений на
// страницу.
func (r *Repo) SiteDay(ctx context.Context, site string, date time.Time) ([]schedule.RoomDay, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT a.oid, a.name, a.room, a.building, a.site, a.site_label, a.site_order, a.kind, a.floor, a.capacity,
		       l.lesson_oid, l.lesson_date, to_char(l.begins_at,'HH24:MI'), to_char(l.ends_at,'HH24:MI'),
		       l.discipline, l.kind_of_work, l.lecturer_oid, l.lecturer_name, l.group_names
		  FROM auditoriums a
		  LEFT JOIN lessons l ON l.auditorium_oid = a.oid AND l.lesson_date = $2::date
		 WHERE a.is_study_space AND a.site <> $3 AND ($1 = '' OR a.site = $1)
		 ORDER BY a.floor NULLS LAST, a.room, l.begins_at`,
		site, date.Format("2006-01-02"), branchSite)
	if err != nil {
		return nil, fmt.Errorf("день площадки: %w", err)
	}
	defer rows.Close()
	var out []schedule.RoomDay
	var cur *schedule.RoomDay
	for rows.Next() {
		var a domain.Auditorium
		var oid *int64
		var l domain.Lesson
		var date *time.Time
		var begins, ends, disc, kind, lecName *string
		var lecOid *int64
		var groups []string
		if err := rows.Scan(&a.Oid, &a.Name, &a.Room, &a.Building, &a.Site.Slug, &a.Site.Label, &a.Site.Order,
			&a.Kind, &a.Floor, &a.Capacity,
			&oid, &date, &begins, &ends, &disc, &kind, &lecOid, &lecName, &groups); err != nil {
			return nil, err
		}
		if cur == nil || cur.Auditorium.Oid != a.Oid {
			a.IsStudySpace = true
			out = append(out, schedule.RoomDay{Auditorium: a})
			cur = &out[len(out)-1]
		}
		// LEFT JOIN даёт пустую строку аудитории без пар — самый интересный
		// случай: свободна весь день.
		if oid == nil {
			continue
		}
		l.LessonOid, l.Date = *oid, *date
		l.BeginsAt, l.EndsAt = deref(begins), deref(ends)
		l.Discipline, l.KindOfWork, l.LecturerName = deref(disc), deref(kind), deref(lecName)
		l.LecturerOid, l.GroupNames = lecOid, groups
		l.Auditorium, l.Building = a.Name, a.Building
		aoid := a.Oid
		l.AuditoriumOid = &aoid
		cur.Lessons = append(cur.Lessons, l)
	}
	return out, rows.Err()
}

// ScheduleFor — расписание владельца за период.
func (r *Repo) ScheduleFor(ctx context.Context, s domain.Subject, from, to time.Time) ([]domain.Lesson, error) {
	var rows pgx.Rows
	var err error
	switch s.Kind {
	case domain.SubjectGroup:
		rows, err = r.pool.Query(ctx, `SELECT `+lessonColumns+` FROM lessons
			WHERE $1 = ANY(group_names) AND lesson_date BETWEEN $2 AND $3 ORDER BY lesson_date, begins_at`,
			s.Group, from, to)
	case domain.SubjectLecturer:
		rows, err = r.pool.Query(ctx, `SELECT `+lessonColumns+` FROM lessons
			WHERE lecturer_oid = $1 AND lesson_date BETWEEN $2 AND $3 ORDER BY lesson_date, begins_at`,
			s.LecturerOid, from, to)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("расписание: %w", err)
	}
	return collectLessons(rows)
}

// OccupancyForAuditorium — пары аудитории на дату.
func (r *Repo) OccupancyForAuditorium(ctx context.Context, oid int64, date time.Time) ([]domain.Lesson, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+lessonColumns+` FROM lessons
		WHERE auditorium_oid = $1 AND lesson_date = $2::date ORDER BY begins_at`, oid, date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("занятость: %w", err)
	}
	return collectLessons(rows)
}

// ─── справочники ─────────────────────────────────────────────────────────────

func (r *Repo) UpsertAuditoriums(ctx context.Context, items []domain.Auditorium) error {
	return db.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, a := range items {
			batch.Queue(`INSERT INTO auditoriums (oid, name, room, building, site, site_label, site_order,
				kind, floor, capacity, is_study_space, last_seen_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
			ON CONFLICT (oid) DO UPDATE SET name=EXCLUDED.name, room=EXCLUDED.room, building=EXCLUDED.building,
				site=EXCLUDED.site, site_label=EXCLUDED.site_label, site_order=EXCLUDED.site_order,
				kind=CASE WHEN EXCLUDED.kind <> '' THEN EXCLUDED.kind ELSE auditoriums.kind END,
				floor=EXCLUDED.floor,
				capacity=COALESCE(EXCLUDED.capacity, auditoriums.capacity),
				-- Признак по типу знает только посев; пара его не несёт и не затирает.
				is_study_space=CASE WHEN EXCLUDED.kind <> '' THEN EXCLUDED.is_study_space ELSE auditoriums.is_study_space END,
				last_seen_at=now()`,
				a.Oid, a.Name, a.Room, a.Building, a.Site.Slug, a.Site.Label, a.Site.Order,
				a.Kind, a.Floor, a.Capacity, a.IsStudySpace)
		}
		return runBatch(ctx, tx, batch, len(items), "справочник аудиторий")
	})
}

func (r *Repo) UpsertGroups(ctx context.Context, items []domain.Group) error {
	return db.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, g := range items {
			batch.Queue(`INSERT INTO groups (id, name, faculty_oid, admission_year, last_seen_at) VALUES ($1,$2,$3,$4,now())
				ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, faculty_oid=EXCLUDED.faculty_oid,
				admission_year=EXCLUDED.admission_year, last_seen_at=now()`,
				g.ID, g.Name, g.FacultyOid, g.AdmissionYear)
		}
		return runBatch(ctx, tx, batch, len(items), "справочник групп")
	})
}

func (r *Repo) UpsertLecturers(ctx context.Context, items []domain.Lecturer) error {
	return db.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, l := range items {
			batch.Queue(`INSERT INTO lecturers (oid, name, last_seen_at) VALUES ($1,$2,now())
				ON CONFLICT (oid) DO UPDATE SET name=EXCLUDED.name, last_seen_at=now()`, l.Oid, l.Name)
		}
		return runBatch(ctx, tx, batch, len(items), "справочник преподавателей")
	})
}

func (r *Repo) AuditoriumOids(ctx context.Context, onlyStudySpaces bool) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT oid FROM auditoriums WHERE ($1 = false OR is_study_space) ORDER BY oid`, onlyStudySpaces)
	if err != nil {
		return nil, fmt.Errorf("список аудиторий: %w", err)
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

func (r *Repo) LecturerName(ctx context.Context, oid int64) (string, error) {
	var name string
	err := r.pool.QueryRow(ctx, `SELECT name FROM lecturers WHERE oid = $1`, oid).Scan(&name)
	return name, err
}

func (r *Repo) SearchGroups(ctx context.Context, query string, limit int) ([]domain.Group, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT id, name, faculty_oid, admission_year FROM groups
		WHERE $1 = '' OR lower(name) LIKE '%' || lower($1) || '%' ORDER BY name LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск групп: %w", err)
	}
	defer rows.Close()
	var out []domain.Group
	for rows.Next() {
		var g domain.Group
		if err := rows.Scan(&g.ID, &g.Name, &g.FacultyOid, &g.AdmissionYear); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repo) SearchLecturers(ctx context.Context, query string, limit int) ([]domain.Lecturer, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := r.pool.Query(ctx, `SELECT oid, name FROM lecturers
		WHERE $1 = '' OR lower(name) LIKE '%' || lower($1) || '%' ORDER BY name LIMIT $2`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск преподавателей: %w", err)
	}
	defer rows.Close()
	var out []domain.Lecturer
	for rows.Next() {
		var l domain.Lecturer
		if err := rows.Scan(&l.Oid, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ─── проходы сборщика ────────────────────────────────────────────────────────

func (r *Repo) StartRun(ctx context.Context, from, to time.Time) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO collector_runs (period_from, period_to) VALUES ($1,$2) RETURNING id`, from, to).Scan(&id)
	return id, err
}

func (r *Repo) FinishRun(ctx context.Context, id int64, requests, errs, lessons, changes int, failure error) error {
	var msg *string
	if failure != nil {
		m := failure.Error()
		msg = &m
	}
	_, err := r.pool.Exec(ctx, `UPDATE collector_runs SET finished_at=now(), requests=$2, errors=$3,
		lessons_seen=$4, changes_found=$5, failure=$6 WHERE id=$1`, id, requests, errs, lessons, changes, msg)
	return err
}

func (r *Repo) LastSuccessfulRun(ctx context.Context) (time.Time, bool, error) {
	var at time.Time
	err := r.pool.QueryRow(ctx, `SELECT finished_at FROM collector_runs
		WHERE finished_at IS NOT NULL AND failure IS NULL ORDER BY finished_at DESC LIMIT 1`).Scan(&at)
	if err == pgx.ErrNoRows {
		return time.Time{}, false, nil
	}
	return at, err == nil, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

var _ schedule.Repository = (*Repo)(nil)

// ─── выгрузка ────────────────────────────────────────────────────────────────

func (r *Repo) Days(ctx context.Context) ([]time.Time, error) {
	rows, err := r.pool.Query(ctx, `SELECT lesson_date FROM lessons GROUP BY 1 ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("дни: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repo) LessonsOn(ctx context.Context, date time.Time) ([]domain.Lesson, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+lessonColumns+` FROM lessons WHERE lesson_date=$1::date ORDER BY begins_at`, date.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("пары дня: %w", err)
	}
	return collectLessons(rows)
}

func (r *Repo) Auditoriums(ctx context.Context) ([]domain.Auditorium, error) {
	rows, err := r.pool.Query(ctx, `SELECT oid, name, room, building, site, site_label, site_order, kind, floor, capacity, is_study_space
		FROM auditoriums WHERE is_study_space AND site <> $1 ORDER BY site_order, floor NULLS LAST, room`, branchSite)
	if err != nil {
		return nil, fmt.Errorf("аудитории: %w", err)
	}
	defer rows.Close()
	var out []domain.Auditorium
	for rows.Next() {
		var a domain.Auditorium
		if err := rows.Scan(&a.Oid, &a.Name, &a.Room, &a.Building, &a.Site.Slug, &a.Site.Label, &a.Site.Order,
			&a.Kind, &a.Floor, &a.Capacity, &a.IsStudySpace); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GroupNames — настоящие группы из пар. Половина значений поля группы у
// источника — склеенные строки вроде «006886_1 Иностранный язык (КАЯиПК)-1»:
// у языковых занятий поток пуст и туда попадает код дисциплины. В список
// для выбора им не место.
func (r *Repo) GroupNames(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT g FROM (SELECT unnest(group_names) g FROM lessons) t
		WHERE g ~ '^[[:alpha:]]+[0-9]{2}-[0-9]+[[:alpha:]]*$' ORDER BY g`)
	if err != nil {
		return nil, fmt.Errorf("группы: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Repo) Lecturers(ctx context.Context) ([]domain.Lecturer, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT lecturer_oid, lecturer_name FROM lessons
		WHERE lecturer_oid IS NOT NULL AND lecturer_name <> '' ORDER BY lecturer_name`)
	if err != nil {
		return nil, fmt.Errorf("преподаватели: %w", err)
	}
	defer rows.Close()
	var out []domain.Lecturer
	for rows.Next() {
		var l domain.Lecturer
		if err := rows.Scan(&l.Oid, &l.Name); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
