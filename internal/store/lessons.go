package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ApplySnapshot заменяет расписание за период на переданный слепок и
// возвращает обнаруженные изменения.
//
// Это ядро всей затеи: источник правит расписание задним числом и никого не
// уведомляет, поэтому изменения приходится обнаруживать сравнением слепков
// по lesson_oid.
//
// Период задаётся явно, а не выводится из содержимого слепка: иначе день,
// из которого источник убрал все пары, остался бы в базе навсегда — мы бы
// просто не увидели, что там что-то было.
//
// Всё выполняется в одной транзакции: наполовину применённый слепок породил
// бы шквал ложных уведомлений.
func (s *Store) ApplySnapshot(ctx context.Context, from, to time.Time, lessons []Lesson) (ApplyResult, error) {
	res := ApplyResult{Seen: len(lessons)}

	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Текущее состояние за период.
		type existing struct {
			fingerprint string
			lesson      Lesson
		}
		prev := map[int64]existing{}

		rows, err := tx.Query(ctx, `
			SELECT lesson_oid, fingerprint, lesson_date, begins_at::text, ends_at::text,
			       auditorium_oid, auditorium, building, discipline, kind_of_work,
			       lecturer_oid, lecturer_name, stream, group_names, subgroup, note
			  FROM lessons
			 WHERE lesson_date BETWEEN $1 AND $2`, from, to)
		if err != nil {
			return fmt.Errorf("чтение текущего слепка: %w", err)
		}
		for rows.Next() {
			var e existing
			var begins, ends string
			if err := rows.Scan(
				&e.lesson.LessonOid, &e.fingerprint, &e.lesson.Date, &begins, &ends,
				&e.lesson.AuditoriumOid, &e.lesson.Auditorium, &e.lesson.Building,
				&e.lesson.Discipline, &e.lesson.KindOfWork,
				&e.lesson.LecturerOid, &e.lesson.LecturerName,
				&e.lesson.Stream, &e.lesson.GroupNames, &e.lesson.Subgroup, &e.lesson.Note,
			); err != nil {
				rows.Close()
				return fmt.Errorf("разбор текущего слепка: %w", err)
			}
			// PostgreSQL отдаёт time как HH:MM:SS, храним и сравниваем HH:MM.
			e.lesson.BeginsAt = trimSeconds(begins)
			e.lesson.EndsAt = trimSeconds(ends)
			prev[e.lesson.LessonOid] = e
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		seen := make(map[int64]bool, len(lessons))
		now := time.Now()

		for _, l := range lessons {
			seen[l.LessonOid] = true
			fp := l.Fingerprint()
			old, existed := prev[l.LessonOid]

			switch {
			case !existed:
				if err := upsertLesson(ctx, tx, l, fp); err != nil {
					return err
				}
				if err := recordChange(ctx, tx, ChangeAdded, l, nil, &l, now); err != nil {
					return err
				}
				res.Added++

			case old.fingerprint != fp:
				if err := upsertLesson(ctx, tx, l, fp); err != nil {
					return err
				}
				before := old.lesson
				if err := recordChange(ctx, tx, ChangeChanged, l, &before, &l, now); err != nil {
					return err
				}
				res.Changed++

			default:
				// Ничего не изменилось — отмечаем, что пара всё ещё есть.
				if _, err := tx.Exec(ctx,
					`UPDATE lessons SET last_seen_at = $2 WHERE lesson_oid = $1`,
					l.LessonOid, now); err != nil {
					return fmt.Errorf("отметка пары %d: %w", l.LessonOid, err)
				}
			}
		}

		// Пары, пропавшие из источника.
		for oid, old := range prev {
			if seen[oid] {
				continue
			}
			before := old.lesson
			if err := recordChange(ctx, tx, ChangeRemoved, before, &before, nil, now); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM lessons WHERE lesson_oid = $1`, oid); err != nil {
				return fmt.Errorf("удаление пары %d: %w", oid, err)
			}
			res.Removed++
		}
		return nil
	})

	return res, err
}

func upsertLesson(ctx context.Context, tx pgx.Tx, l Lesson, fingerprint string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO lessons (
			lesson_oid, lesson_date, begins_at, ends_at,
			auditorium_oid, auditorium, building,
			discipline, kind_of_work, lecturer_oid, lecturer_name,
			stream, group_names, subgroup, note,
			fingerprint, source_modified_at, last_seen_at
		) VALUES ($1,$2,$3::time,$4::time,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17, now())
		ON CONFLICT (lesson_oid) DO UPDATE SET
			lesson_date = EXCLUDED.lesson_date,
			begins_at = EXCLUDED.begins_at,
			ends_at = EXCLUDED.ends_at,
			auditorium_oid = EXCLUDED.auditorium_oid,
			auditorium = EXCLUDED.auditorium,
			building = EXCLUDED.building,
			discipline = EXCLUDED.discipline,
			kind_of_work = EXCLUDED.kind_of_work,
			lecturer_oid = EXCLUDED.lecturer_oid,
			lecturer_name = EXCLUDED.lecturer_name,
			stream = EXCLUDED.stream,
			group_names = EXCLUDED.group_names,
			subgroup = EXCLUDED.subgroup,
			note = EXCLUDED.note,
			fingerprint = EXCLUDED.fingerprint,
			source_modified_at = EXCLUDED.source_modified_at,
			last_seen_at = now()`,
		l.LessonOid, l.Date, l.BeginsAt, l.EndsAt,
		l.AuditoriumOid, l.Auditorium, l.Building,
		l.Discipline, l.KindOfWork, l.LecturerOid, l.LecturerName,
		l.Stream, l.GroupNames, l.Subgroup, l.Note,
		fingerprint, l.SourceModifiedAt)
	if err != nil {
		return fmt.Errorf("сохранение пары %d: %w", l.LessonOid, err)
	}
	return nil
}

// recordChange пишет строку в журнал изменений.
//
// Адресные поля (дата, группы, преподаватель, аудитория) дублируются в
// журнал, чтобы рассылка уведомлений не зависела от того, существует ли
// пара сейчас: у удалённой пары брать их уже неоткуда.
func recordChange(ctx context.Context, tx pgx.Tx, kind ChangeKind, addr Lesson, before, after *Lesson, at time.Time) error {
	var beforeJSON, afterJSON []byte
	var err error
	if before != nil {
		if beforeJSON, err = json.Marshal(before); err != nil {
			return err
		}
	}
	if after != nil {
		if afterJSON, err = json.Marshal(after); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO lesson_changes (
			lesson_oid, kind, detected_at, lesson_date,
			group_names, lecturer_oid, auditorium_oid, before, after
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		addr.LessonOid, string(kind), at, addr.Date,
		addr.GroupNames, addr.LecturerOid, addr.AuditoriumOid, beforeJSON, afterJSON)
	if err != nil {
		return fmt.Errorf("запись изменения пары %d: %w", addr.LessonOid, err)
	}
	return nil
}

func trimSeconds(t string) string {
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

// ChangesSince возвращает изменения, обнаруженные после указанного момента,
// от новых к старым.
func (s *Store) ChangesSince(ctx context.Context, since time.Time, limit int) ([]Change, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, lesson_oid, kind::text, detected_at, lesson_date,
		       group_names, lecturer_oid, auditorium_oid, before, after
		  FROM lesson_changes
		 WHERE detected_at >= $1
		 ORDER BY detected_at DESC, id DESC
		 LIMIT $2`, since, limit)
	if err != nil {
		return nil, fmt.Errorf("чтение журнала изменений: %w", err)
	}
	defer rows.Close()

	var out []Change
	for rows.Next() {
		var c Change
		var kind string
		var beforeJSON, afterJSON []byte
		if err := rows.Scan(&c.ID, &c.LessonOid, &kind, &c.DetectedAt, &c.LessonDate,
			&c.GroupNames, &c.LecturerOid, &c.AuditoriumOid, &beforeJSON, &afterJSON); err != nil {
			return nil, err
		}
		c.Kind = ChangeKind(kind)
		if len(beforeJSON) > 0 {
			c.Before = &Lesson{}
			if err := json.Unmarshal(beforeJSON, c.Before); err != nil {
				return nil, err
			}
		}
		if len(afterJSON) > 0 {
			c.After = &Lesson{}
			if err := json.Unmarshal(afterJSON, c.After); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
