// Package postgres — хранилище модуля schedule.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/db"
)

// Repo — реализация schedule.Repository.
type Repo struct{ pool *pgxpool.Pool }

// New создаёт хранилище.
func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

const lessonColumns = `lesson_oid, lesson_date, to_char(begins_at,'HH24:MI'), to_char(ends_at,'HH24:MI'),
	auditorium_oid, auditorium, building, discipline, kind_of_work,
	lecturer_oid, lecturer_name, stream, group_names, subgroup, note, source_modified_at`

func scanLesson(row pgx.Row) (domain.Lesson, error) {
	var l domain.Lesson
	err := row.Scan(&l.LessonOid, &l.Date, &l.BeginsAt, &l.EndsAt,
		&l.AuditoriumOid, &l.Auditorium, &l.Building, &l.Discipline, &l.KindOfWork,
		&l.LecturerOid, &l.LecturerName, &l.Stream, &l.GroupNames, &l.Subgroup, &l.Note,
		&l.SourceModifiedAt)
	return l, err
}

func collectLessons(rows pgx.Rows) ([]domain.Lesson, error) {
	defer rows.Close()
	var out []domain.Lesson
	for rows.Next() {
		l, err := scanLesson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ApplySnapshot — ядро всей затеи: источник правит расписание задним числом
// молча, поэтому изменения приходится обнаруживать сравнением слепков по
// lesson_oid. Всё в одной транзакции: наполовину применённый слепок
// породил бы шквал ложных уведомлений.
func (r *Repo) ApplySnapshot(ctx context.Context, from, to time.Time, lessons []domain.Lesson) (domain.ApplyResult, error) {
	res := domain.ApplyResult{Seen: len(lessons)}
	err := db.InTx(ctx, r.pool, func(tx pgx.Tx) error {
		prevRows, err := tx.Query(ctx, `SELECT fingerprint, `+lessonColumns+`
			FROM lessons WHERE lesson_date BETWEEN $1 AND $2`, from, to)
		if err != nil {
			return fmt.Errorf("чтение текущего окна: %w", err)
		}
		type existing struct {
			fp     string
			lesson domain.Lesson
		}
		prev := map[int64]existing{}
		// Дни, о которых мы уже что-то знали: появление пар в новом дне —
		// не правка вуза, а расширение окна.
		knownDays := map[string]bool{}
		for prevRows.Next() {
			var e existing
			var l domain.Lesson
			if err := prevRows.Scan(&e.fp, &l.LessonOid, &l.Date, &l.BeginsAt, &l.EndsAt,
				&l.AuditoriumOid, &l.Auditorium, &l.Building, &l.Discipline, &l.KindOfWork,
				&l.LecturerOid, &l.LecturerName, &l.Stream, &l.GroupNames, &l.Subgroup, &l.Note,
				&l.SourceModifiedAt); err != nil {
				prevRows.Close()
				return err
			}
			e.lesson = l
			prev[l.LessonOid] = e
			knownDays[l.DateKey()] = true
		}
		prevRows.Close()
		if err := prevRows.Err(); err != nil {
			return err
		}

		now := time.Now()
		var (
			untouched []int64
			upserts   []domain.Lesson
			fps       []string
			changes   []pending
			seen      = make(map[int64]bool, len(lessons))
		)
		for i := range lessons {
			l := lessons[i]
			seen[l.LessonOid] = true
			fp := l.Fingerprint()
			old, existed := prev[l.LessonOid]
			switch {
			case !existed:
				upserts = append(upserts, l)
				fps = append(fps, fp)
				// Журналируем только то, что появилось в дне, который мы
				// уже видели. Первое наполнение и день, впервые вошедший в
				// окно, — не изменения: «изменение» существует лишь
				// относительно того, что мы знали.
				if knownDays[l.DateKey()] {
					changes = append(changes, pending{kind: domain.ChangeAdded, addr: l, after: &lessons[i]})
				}
				res.Added++
			case old.fp != fp:
				upserts = append(upserts, l)
				fps = append(fps, fp)
				before := old.lesson
				changes = append(changes, pending{kind: domain.ChangeChanged, addr: l, before: &before, after: &lessons[i]})
				res.Changed++
			default:
				untouched = append(untouched, l.LessonOid)
			}
		}

		if err := upsertLessons(ctx, tx, upserts, fps); err != nil {
			return err
		}
		if len(untouched) > 0 {
			if _, err := tx.Exec(ctx, `UPDATE lessons SET last_seen_at=$2 WHERE lesson_oid = ANY($1)`, untouched, now); err != nil {
				return fmt.Errorf("отметка неизменившихся: %w", err)
			}
		}
		var removed []int64
		for oid, old := range prev {
			if seen[oid] {
				continue
			}
			before := old.lesson
			changes = append(changes, pending{kind: domain.ChangeRemoved, addr: before, before: &before})
			removed = append(removed, oid)
			res.Removed++
		}
		if len(removed) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM lessons WHERE lesson_oid = ANY($1)`, removed); err != nil {
				return fmt.Errorf("удаление пропавших: %w", err)
			}
		}
		return recordChanges(ctx, tx, changes, now)
	})
	return res, err
}

type pending struct {
	kind          domain.ChangeKind
	addr          domain.Lesson
	before, after *domain.Lesson
}

// Записи идут пачками через pgx.Batch: построчно это одиннадцать тысяч
// обращений за проход, из которых почти все — «ничего не изменилось».
const chunk = 500

func upsertLessons(ctx context.Context, tx pgx.Tx, lessons []domain.Lesson, fps []string) error {
	for start := 0; start < len(lessons); start += chunk {
		end := min(start+chunk, len(lessons))
		batch := &pgx.Batch{}
		for i := start; i < end; i++ {
			l := lessons[i]
			batch.Queue(`INSERT INTO lessons (lesson_oid, lesson_date, begins_at, ends_at,
				auditorium_oid, auditorium, building, discipline, kind_of_work, lecturer_oid, lecturer_name,
				stream, group_names, subgroup, note, fingerprint, source_modified_at, last_seen_at)
			VALUES ($1,$2,$3::time,$4::time,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,now())
			ON CONFLICT (lesson_oid) DO UPDATE SET
				lesson_date=EXCLUDED.lesson_date, begins_at=EXCLUDED.begins_at, ends_at=EXCLUDED.ends_at,
				auditorium_oid=EXCLUDED.auditorium_oid, auditorium=EXCLUDED.auditorium, building=EXCLUDED.building,
				discipline=EXCLUDED.discipline, kind_of_work=EXCLUDED.kind_of_work,
				lecturer_oid=EXCLUDED.lecturer_oid, lecturer_name=EXCLUDED.lecturer_name,
				stream=EXCLUDED.stream, group_names=EXCLUDED.group_names, subgroup=EXCLUDED.subgroup, note=EXCLUDED.note,
				fingerprint=EXCLUDED.fingerprint, source_modified_at=EXCLUDED.source_modified_at, last_seen_at=now()`,
				l.LessonOid, l.Date, l.BeginsAt, l.EndsAt, l.AuditoriumOid, l.Auditorium, l.Building,
				l.Discipline, l.KindOfWork, l.LecturerOid, l.LecturerName, l.Stream, l.GroupNames,
				l.Subgroup, l.Note, fps[i], l.SourceModifiedAt)
		}
		if err := runBatch(ctx, tx, batch, end-start, "сохранение пар"); err != nil {
			return err
		}
	}
	return nil
}

func recordChanges(ctx context.Context, tx pgx.Tx, changes []pending, at time.Time) error {
	for start := 0; start < len(changes); start += chunk {
		end := min(start+chunk, len(changes))
		batch := &pgx.Batch{}
		for i := start; i < end; i++ {
			c := changes[i]
			before, after, err := marshalPair(c.before, c.after)
			if err != nil {
				return err
			}
			batch.Queue(`INSERT INTO lesson_changes (lesson_oid, kind, detected_at, lesson_date,
				group_names, lecturer_oid, auditorium_oid, before, after) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				c.addr.LessonOid, string(c.kind), at, c.addr.Date,
				c.addr.GroupNames, c.addr.LecturerOid, c.addr.AuditoriumOid, before, after)
		}
		if err := runBatch(ctx, tx, batch, end-start, "запись журнала"); err != nil {
			return err
		}
	}
	return nil
}

func runBatch(ctx context.Context, tx pgx.Tx, batch *pgx.Batch, n int, what string) error {
	if n == 0 {
		return nil
	}
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < n; i++ {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return fmt.Errorf("%s: %w", what, err)
		}
	}
	return br.Close()
}

func marshalPair(before, after *domain.Lesson) ([]byte, []byte, error) {
	var b, a []byte
	var err error
	if before != nil {
		if b, err = json.Marshal(before); err != nil {
			return nil, nil, err
		}
	}
	if after != nil {
		if a, err = json.Marshal(after); err != nil {
			return nil, nil, err
		}
	}
	return b, a, nil
}

// ChangesSince — изменения после момента, от новых к старым.
func (r *Repo) ChangesSince(ctx context.Context, since time.Time, limit int) ([]domain.Change, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id, lesson_oid, kind::text, detected_at, lesson_date,
		group_names, lecturer_oid, auditorium_oid, before, after
		FROM lesson_changes WHERE detected_at >= $1 ORDER BY detected_at DESC, id DESC LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("чтение журнала: %w", err)
	}
	defer rows.Close()
	var out []domain.Change
	for rows.Next() {
		var c domain.Change
		var kind string
		var b, a []byte
		if err := rows.Scan(&c.ID, &c.LessonOid, &kind, &c.DetectedAt, &c.LessonDate,
			&c.GroupNames, &c.LecturerOid, &c.AuditoriumOid, &b, &a); err != nil {
			return nil, err
		}
		c.Kind = domain.ChangeKind(kind)
		if len(b) > 0 {
			c.Before = &domain.Lesson{}
			if err := json.Unmarshal(b, c.Before); err != nil {
				return nil, err
			}
		}
		if len(a) > 0 {
			c.After = &domain.Lesson{}
			if err := json.Unmarshal(a, c.After); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CleanupChanges — журнал живёт дни, не месяцы; хранить его вечно значит
// копить мегабайты ради истории, в которую никто не заглянет, и заодно
// держать архив вузовских данных.
func (r *Repo) CleanupChanges(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM lesson_changes WHERE detected_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("чистка журнала: %w", err)
	}
	return tag.RowsAffected(), nil
}
