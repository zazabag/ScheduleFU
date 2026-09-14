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

		// Первое наполнение — не изменение расписания.
		//
		// Когда за период в базе нет ни одной пары, а источник принёс
		// тысячи, это означает лишь, что мы узнали расписание, а не что
		// вуз его переписал. Журналировать такое бессмысленно: история
		// раздувается на десяток мегабайт, а смотреть в ней нечего.
		// «Изменение» имеет смысл только относительно того, что мы знали.
		firstFill := len(prev) == 0 && len(lessons) > 0

		// Пары раскладываются по трём корзинам и записываются пачками.
		// Построчная запись означала бы по одному обращению к базе на
		// каждую пару: одиннадцать тысяч обращений за проход, из которых
		// подавляющее большинство — «ничего не изменилось».
		var (
			untouched []int64  // только отметить, что пара всё ещё есть
			upserts   []Lesson // новые и изменившиеся
			fps       []string
			changes   []pendingChange
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
				if !firstFill {
					changes = append(changes, pendingChange{kind: ChangeAdded, addr: l, after: &lessons[i]})
				}
				res.Added++

			case old.fingerprint != fp:
				upserts = append(upserts, l)
				fps = append(fps, fp)
				before := old.lesson
				changes = append(changes, pendingChange{
					kind: ChangeChanged, addr: l, before: &before, after: &lessons[i],
				})
				res.Changed++

			default:
				untouched = append(untouched, l.LessonOid)
			}
		}

		if err := upsertLessons(ctx, tx, upserts, fps); err != nil {
			return err
		}
		if len(untouched) > 0 {
			if _, err := tx.Exec(ctx,
				`UPDATE lessons SET last_seen_at = $2 WHERE lesson_oid = ANY($1)`,
				untouched, now); err != nil {
				return fmt.Errorf("отметка неизменившихся пар: %w", err)
			}
		}

		// Пары, пропавшие из источника.
		var removed []int64
		for oid, old := range prev {
			if seen[oid] {
				continue
			}
			before := old.lesson
			changes = append(changes, pendingChange{
				kind: ChangeRemoved, addr: before, before: &before,
			})
			removed = append(removed, oid)
			res.Removed++
		}
		if len(removed) > 0 {
			if _, err := tx.Exec(ctx,
				`DELETE FROM lessons WHERE lesson_oid = ANY($1)`, removed); err != nil {
				return fmt.Errorf("удаление пропавших пар: %w", err)
			}
		}

		if err := recordChanges(ctx, tx, changes, now); err != nil {
			return err
		}

		return nil
	})

	return res, err
}

// upsertLessons записывает пары одной пачкой.
//
// pgx.Batch отправляет все команды за один обмен с базой: время перестаёт
// зависеть от задержки соединения, которая на построчной записи и съедала
// почти всё.
func upsertLessons(ctx context.Context, tx pgx.Tx, lessons []Lesson, fingerprints []string) error {
	if len(lessons) == 0 {
		return nil
	}
	const chunk = 500 // пачками, чтобы не держать в памяти всю неделю сразу
	for start := 0; start < len(lessons); start += chunk {
		end := start + chunk
		if end > len(lessons) {
			end = len(lessons)
		}
		batch := &pgx.Batch{}
		for i := start; i < end; i++ {
			queueLesson(batch, lessons[i], fingerprints[i])
		}
		br := tx.SendBatch(ctx, batch)
		for i := start; i < end; i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("сохранение пары %d: %w", lessons[i].LessonOid, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("запись пачки пар: %w", err)
		}
	}
	return nil
}

func queueLesson(batch *pgx.Batch, l Lesson, fingerprint string) {
	batch.Queue(`
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
}

// pendingChange — изменение, ожидающее записи в журнал.
type pendingChange struct {
	kind   ChangeKind
	addr   Lesson // откуда берутся адресные поля
	before *Lesson
	after  *Lesson
}

// recordChanges пишет журнал одной пачкой.
//
// В первый проход изменений столько же, сколько пар, и построчная запись
// удваивала бы стоимость всей операции.
func recordChanges(ctx context.Context, tx pgx.Tx, changes []pendingChange, at time.Time) error {
	if len(changes) == 0 {
		return nil
	}
	const chunk = 500
	for start := 0; start < len(changes); start += chunk {
		end := start + chunk
		if end > len(changes) {
			end = len(changes)
		}
		batch := &pgx.Batch{}
		for i := start; i < end; i++ {
			c := changes[i]
			beforeJSON, afterJSON, err := marshalStates(c.before, c.after)
			if err != nil {
				return err
			}
			batch.Queue(`
				INSERT INTO lesson_changes (
					lesson_oid, kind, detected_at, lesson_date,
					group_names, lecturer_oid, auditorium_oid, before, after
				) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
				c.addr.LessonOid, string(c.kind), at, c.addr.Date,
				c.addr.GroupNames, c.addr.LecturerOid, c.addr.AuditoriumOid,
				beforeJSON, afterJSON)
		}
		br := tx.SendBatch(ctx, batch)
		for i := start; i < end; i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("запись изменения пары %d: %w", changes[i].addr.LessonOid, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("запись журнала изменений: %w", err)
		}
	}
	return nil
}

func marshalStates(before, after *Lesson) ([]byte, []byte, error) {
	var beforeJSON, afterJSON []byte
	var err error
	if before != nil {
		if beforeJSON, err = json.Marshal(before); err != nil {
			return nil, nil, err
		}
	}
	if after != nil {
		if afterJSON, err = json.Marshal(after); err != nil {
			return nil, nil, err
		}
	}
	return beforeJSON, afterJSON, nil
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

// CleanupChanges убирает записи журнала старше указанного срока.
//
// Журнал нужен, чтобы показать «что изменилось» и разослать уведомления;
// то и другое живёт днями, а не месяцами. Хранить его вечно значит
// медленно копить мегабайты ради истории, в которую никто не заглянет — и
// заодно держать у себя архив вузовских данных, чего мы делать не
// собирались (docs/03-legal-risks.md).
func (s *Store) CleanupChanges(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM lesson_changes WHERE detected_at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("чистка журнала изменений: %w", err)
	}
	return tag.RowsAffected(), nil
}
