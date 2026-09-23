// Package postgres — хранилище записей, конспектов и домашних заданий.
//
// Каждый запрос чтения и правки ограничен owner_key: ключ приходит из
// cookie и подделывается так же легко, как любая cookie, поэтому «чужой
// конспект» должен не находиться, а не «находиться, но не показываться».
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

type Repo struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// recordingColumns — один список на все чтения записи: колонок много, и
// разъехавшийся порядок в Scan ловится только в бою.
const recordingColumns = `id, owner_key, subject_key, discipline, lesson_date,
	to_char(begins_at,'HH24:MI'), to_char(ends_at,'HH24:MI'), lecturer_name, auditorium,
	kind_of_work, lesson_oid, source, status, chunks, bytes, duration_sec, audio_path,
	transcript, attempts, failure, created_at, updated_at, gaps`

// scanRecording читает строку. Состояние и происхождение разбираются через
// обычные строки: в базе это текст, а domain.Status — наш тип, и полагаться
// на то, что драйвер сам сообразит, незачем.
func scanRecording(row pgx.Row) (domain.Recording, error) {
	var rec domain.Recording
	var begins, ends *string
	var origin, status string
	var gaps []byte
	err := row.Scan(&rec.ID, &rec.OwnerKey, &rec.Lesson.SubjectKey, &rec.Lesson.Discipline, &rec.Lesson.Date,
		&begins, &ends, &rec.Lesson.LecturerName, &rec.Lesson.Auditorium,
		&rec.Lesson.KindOfWork, &rec.Lesson.LessonOid, &origin, &status, &rec.Chunks, &rec.Bytes,
		&rec.DurationSec, &rec.AudioPath, &rec.Transcript, &rec.Attempts, &rec.Failure,
		&rec.CreatedAt, &rec.UpdatedAt, &gaps)
	rec.Origin, rec.Status = domain.Origin(origin), domain.Status(status)
	if err == nil && len(gaps) > 0 {
		if jerr := json.Unmarshal(gaps, &rec.Gaps); jerr != nil {
			return rec, fmt.Errorf("пропуски записи %d: %w", rec.ID, jerr)
		}
	}
	if begins != nil {
		rec.Lesson.BeginsAt = *begins
	}
	if ends != nil {
		rec.Lesson.EndsAt = *ends
	}
	return rec, err
}

// nullTime — пустое время пары пишется как NULL: у загруженного файла его
// может не быть вовсе, а «00:00» — это полночь, а не «неизвестно».
//
// Значение уходит строкой и приводится к time уже в SQL ($n::text::time):
// «10:10» — это текст, и полагаться на то, что драйвер сам догадается
// превратить его во время, незачем.
func nullTime(hhmm string) any {
	if hhmm == "" {
		return nil
	}
	return hhmm
}

func (r *Repo) CreateRecording(ctx context.Context, rec domain.Recording) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO recordings
		(owner_key, subject_key, discipline, lesson_date, begins_at, ends_at, lecturer_name,
		 auditorium, kind_of_work, lesson_oid, source, status)
		VALUES ($1,$2,$3,$4,$5::text::time,$6::text::time,$7,$8,$9,$10,$11,$12) RETURNING id`,
		rec.OwnerKey, rec.Lesson.SubjectKey, rec.Lesson.Discipline, rec.Lesson.Date,
		nullTime(rec.Lesson.BeginsAt), nullTime(rec.Lesson.EndsAt), rec.Lesson.LecturerName,
		rec.Lesson.Auditorium, rec.Lesson.KindOfWork, rec.Lesson.LessonOid,
		string(rec.Origin), string(domain.StatusUploading)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("создание записи: %w", err)
	}
	return id, nil
}

func (r *Repo) Recording(ctx context.Context, owner string, id int64) (domain.Recording, bool, error) {
	rec, err := scanRecording(r.pool.QueryRow(ctx,
		`SELECT `+recordingColumns+` FROM recordings WHERE id=$1 AND owner_key=$2`, id, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Recording{}, false, nil
	}
	if err != nil {
		return domain.Recording{}, false, fmt.Errorf("чтение записи: %w", err)
	}
	return rec, true, nil
}

// CountChunk отмечает принятый кусок. Условие chunks=$2 делает приём
// идемпотентным: повтор того же куска после обрыва не сдвинет счётчик.
// Путь к файлу проставляется здесь же — до первого куска файла нет, а
// уборщику брошенных записей знать его нужно.
func (r *Repo) CountChunk(ctx context.Context, id int64, seq int, size int64, path string) error {
	_, err := r.pool.Exec(ctx, `UPDATE recordings SET chunks=$2+1, bytes=bytes+$3, audio_path=$4, updated_at=now()
		WHERE id=$1 AND chunks=$2`, id, seq, size, path)
	if err != nil {
		return fmt.Errorf("учёт куска записи: %w", err)
	}
	return nil
}

func (r *Repo) Enqueue(ctx context.Context, id int64, gaps []domain.Gap) error {
	if gaps == nil {
		gaps = []domain.Gap{}
	}
	raw, err := json.Marshal(gaps)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE recordings SET status=$2, gaps=$4::jsonb, next_attempt_at=now(), updated_at=now()
		WHERE id=$1 AND status=$3`, id, string(domain.StatusQueued), string(domain.StatusUploading), string(raw))
	if err != nil {
		return fmt.Errorf("постановка записи в очередь: %w", err)
	}
	return nil
}

func (r *Repo) Recordings(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Recording, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+recordingColumns+` FROM recordings
		WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3
		ORDER BY lesson_date DESC, created_at DESC`, owner, subjectKey, discipline)
	if err != nil {
		return nil, fmt.Errorf("список записей: %w", err)
	}
	defer rows.Close()
	var out []domain.Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// DeleteRecording возвращает путь к файлу: удалить его с диска — дело
// сервиса, база про файловую систему ничего не знает.
func (r *Repo) DeleteRecording(ctx context.Context, owner string, id int64) (string, error) {
	var path string
	err := r.pool.QueryRow(ctx, `DELETE FROM recordings WHERE id=$1 AND owner_key=$2 RETURNING audio_path`,
		id, owner).Scan(&path)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("удаление записи: %w", err)
	}
	return path, nil
}

// Claim берёт одну запись из очереди.
//
// SKIP LOCKED, а не просто UPDATE: воркеров может быть больше одного, и
// расшифровка одной пары дважды — это десять минут процессора впустую.
// Взятые в работу строки тоже попадают в выборку по next_attempt_at: если
// воркер умер посреди расшифровки, запись иначе зависла бы навсегда.
func (r *Repo) Claim(ctx context.Context, now time.Time) (domain.Recording, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Recording{}, false, err
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM recordings
		WHERE status IN ('queued','decoding','transcribing','summarizing') AND next_attempt_at <= $1
		ORDER BY next_attempt_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Recording{}, false, nil
	}
	if err != nil {
		return domain.Recording{}, false, fmt.Errorf("выбор записи из очереди: %w", err)
	}
	// Пока обрабатываем, строка не должна снова попасть в выборку: час —
	// с запасом на самую долгую пару.
	rec, err := scanRecording(tx.QueryRow(ctx, `UPDATE recordings
		SET status=$2, attempts=attempts+1, next_attempt_at=$3, updated_at=now()
		WHERE id=$1 RETURNING `+recordingColumns,
		id, string(domain.StatusDecoding), now.Add(time.Hour)))
	if err != nil {
		return domain.Recording{}, false, fmt.Errorf("взятие записи в работу: %w", err)
	}
	// attempts уже увеличен в базе; сервису нужно значение до попытки,
	// чтобы посчитать, не пора ли сдаваться.
	rec.Attempts--
	return rec, true, tx.Commit(ctx)
}

func (r *Repo) SetStatus(ctx context.Context, id int64, st domain.Status) error {
	_, err := r.pool.Exec(ctx, `UPDATE recordings SET status=$2, updated_at=now() WHERE id=$1`, id, string(st))
	if err != nil {
		return fmt.Errorf("смена состояния записи: %w", err)
	}
	return nil
}

func (r *Repo) SetTranscript(ctx context.Context, id int64, transcript string, durationSec int) error {
	_, err := r.pool.Exec(ctx, `UPDATE recordings SET transcript=$2, duration_sec=$3, updated_at=now()
		WHERE id=$1`, id, transcript, durationSec)
	if err != nil {
		return fmt.Errorf("сохранение расшифровки: %w", err)
	}
	return nil
}

func (r *Repo) Complete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE recordings SET status=$2, failure='', audio_path='', updated_at=now()
		WHERE id=$1`, id, string(domain.StatusReady))
	if err != nil {
		return fmt.Errorf("завершение обработки: %w", err)
	}
	return nil
}

func (r *Repo) Fail(ctx context.Context, id int64, reason string, retryAt time.Time, giveUp bool) error {
	status := domain.StatusQueued
	if giveUp {
		status = domain.StatusFailed
	}
	_, err := r.pool.Exec(ctx, `UPDATE recordings SET status=$2, failure=$3, next_attempt_at=$4, updated_at=now()
		WHERE id=$1`, id, string(status), reason, retryAt)
	if err != nil {
		return fmt.Errorf("отметка неудачи: %w", err)
	}
	return nil
}

// ─── конспекты ───────────────────────────────────────────────────────────────

const noteColumns = `id, owner_key, recording_id, subject_key, discipline, lesson_date,
	to_char(begins_at,'HH24:MI'), lecturer_name, auditorium, title, body, theses,
	saved_at, created_at, updated_at, COALESCE(share_token, ''), copied_from, cards_status, cards_failure`

func scanNote(row pgx.Row) (domain.Note, error) {
	var n domain.Note
	var begins *string
	err := row.Scan(&n.ID, &n.OwnerKey, &n.RecordingID, &n.Lesson.SubjectKey, &n.Lesson.Discipline,
		&n.Lesson.Date, &begins, &n.Lesson.LecturerName, &n.Lesson.Auditorium, &n.Title, &n.Body,
		&n.Theses, &n.SavedAt, &n.CreatedAt, &n.UpdatedAt, &n.ShareToken, &n.CopiedFrom, &n.CardsStatus, &n.CardsFailure)
	if begins != nil {
		n.Lesson.BeginsAt = *begins
	}
	return n, err
}

func (r *Repo) CreateNote(ctx context.Context, n domain.Note) (int64, error) {
	// nil-срез pgx пишет как NULL, а theses — NOT NULL: конспект без тезисов
	// (модель их не нашла) иначе падал бы на последнем шаге обработки.
	if n.Theses == nil {
		n.Theses = []string{}
	}
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO notes
		(owner_key, recording_id, subject_key, discipline, lesson_date, begins_at, lecturer_name,
		 auditorium, title, body, theses, copied_from)
		VALUES ($1,$2,$3,$4,$5,$6::text::time,$7,$8,$9,$10,$11,$12) RETURNING id`,
		n.OwnerKey, n.RecordingID, n.Lesson.SubjectKey, n.Lesson.Discipline, n.Lesson.Date,
		nullTime(n.Lesson.BeginsAt), n.Lesson.LecturerName, n.Lesson.Auditorium, n.Title, n.Body,
		n.Theses, n.CopiedFrom).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("создание конспекта: %w", err)
	}
	return id, nil
}

func (r *Repo) Note(ctx context.Context, owner string, id int64) (domain.Note, bool, error) {
	n, err := scanNote(r.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM notes WHERE id=$1 AND owner_key=$2`, id, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("чтение конспекта: %w", err)
	}
	return n, true, nil
}

func (r *Repo) SetShareToken(ctx context.Context, owner string, id int64, token string, at time.Time) (bool, error) {
	var tok *string
	var when *time.Time
	if token != "" {
		tok, when = &token, &at
	}
	tag, err := r.pool.Exec(ctx, `UPDATE notes SET share_token=$3, shared_at=$4, updated_at=now()
		WHERE id=$1 AND owner_key=$2 AND saved_at IS NOT NULL`, id, owner, tok, when)
	if err != nil {
		return false, fmt.Errorf("ссылка на конспект: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repo) NoteByShare(ctx context.Context, token string) (domain.Note, bool, error) {
	n, err := scanNote(r.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM notes WHERE share_token=$1 AND saved_at IS NOT NULL`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("конспект по ссылке: %w", err)
	}
	return n, true, nil
}

func (r *Repo) CopyOf(ctx context.Context, owner string, source int64) (domain.Note, bool, error) {
	n, err := scanNote(r.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM notes WHERE owner_key=$1 AND copied_from=$2 ORDER BY id LIMIT 1`, owner, source))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("копия конспекта: %w", err)
	}
	return n, true, nil
}

func (r *Repo) NoteByRecording(ctx context.Context, owner string, recordingID int64) (domain.Note, bool, error) {
	n, err := scanNote(r.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM notes WHERE recording_id=$1 AND owner_key=$2`, recordingID, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("чтение конспекта записи: %w", err)
	}
	return n, true, nil
}

func (r *Repo) Notes(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Note, error) {
	return r.notesWhere(ctx, owner, subjectKey, discipline, "saved_at IS NOT NULL")
}

func (r *Repo) DraftNotes(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Note, error) {
	return r.notesWhere(ctx, owner, subjectKey, discipline, "saved_at IS NULL")
}

// notesWhere — конспекты предмета с одним из двух условий на сохранённость.
// Условие — константа из этого файла, не ввод человека.
func (r *Repo) notesWhere(ctx context.Context, owner, subjectKey, discipline, saved string) ([]domain.Note, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+noteColumns+` FROM notes
		WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3 AND `+saved+`
		ORDER BY lesson_date DESC, begins_at DESC NULLS LAST`, owner, subjectKey, discipline)
	if err != nil {
		return nil, fmt.Errorf("список конспектов: %w", err)
	}
	defer rows.Close()
	var out []domain.Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SearchNotes — русский полнотекстовый поиск: «дюрацию» находит по
// «дюрация», websearch_to_tsquery понимает кавычки и минус, как поисковик.
// Индекса нет сознательно: поиск идёт по конспектам одного устройства —
// десятки строк, и индекс по всей таблице ради них не окупается.
func (r *Repo) SearchNotes(ctx context.Context, owner, query string, limit int) ([]domain.NoteHit, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		WITH q AS (SELECT websearch_to_tsquery('russian', $2) AS tsq),
		     n AS (SELECT notes.*, to_tsvector('russian', title || ' ' || array_to_string(theses, ' ') || ' ' || body) AS doc
		             FROM notes WHERE owner_key = $1 AND saved_at IS NOT NULL)
		SELECT `+noteColumns+`,
		       ts_headline('russian', n.title || '. ' || array_to_string(n.theses, ' ') || ' ' || n.body, q.tsq,
		                   'StartSel=`+domain.HitStart+`, StopSel=`+domain.HitEnd+`, MaxWords=30, MinWords=12, MaxFragments=2, FragmentDelimiter=" … "')
		  FROM n, q WHERE n.doc @@ q.tsq
		 ORDER BY ts_rank(n.doc, q.tsq) DESC, n.lesson_date DESC LIMIT $3`, owner, query, limit)
	if err != nil {
		return nil, fmt.Errorf("поиск конспектов: %w", err)
	}
	defer rows.Close()
	var out []domain.NoteHit
	for rows.Next() {
		var h domain.NoteHit
		var begins *string
		n := &h.Note
		if err := rows.Scan(&n.ID, &n.OwnerKey, &n.RecordingID, &n.Lesson.SubjectKey, &n.Lesson.Discipline,
			&n.Lesson.Date, &begins, &n.Lesson.LecturerName, &n.Lesson.Auditorium, &n.Title, &n.Body,
			&n.Theses, &n.SavedAt, &n.CreatedAt, &n.UpdatedAt, &n.ShareToken, &n.CopiedFrom,
			&n.CardsStatus, &n.CardsFailure, &h.Snippet); err != nil {
			return nil, err
		}
		if begins != nil {
			n.Lesson.BeginsAt = *begins
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repo) SaveNote(ctx context.Context, owner string, id int64, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE notes SET saved_at=COALESCE(saved_at,$3), updated_at=now()
		WHERE id=$1 AND owner_key=$2`, id, owner, at)
	if err != nil {
		return fmt.Errorf("сохранение конспекта: %w", err)
	}
	return nil
}

// DeleteNote уносит с собой задания-черновики этого конспекта: они без него
// бессмысленны. Сохранённые задания остаются — человек их уже принял.
func (r *Repo) DeleteNote(ctx context.Context, owner string, id int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM homeworks WHERE note_id=$1 AND owner_key=$2 AND saved_at IS NULL`,
		id, owner); err != nil {
		return fmt.Errorf("удаление заданий конспекта: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM notes WHERE id=$1 AND owner_key=$2`, id, owner); err != nil {
		return fmt.Errorf("удаление конспекта: %w", err)
	}
	return tx.Commit(ctx)
}

// ─── домашние задания ────────────────────────────────────────────────────────

const homeworkColumns = `id, owner_key, note_id, subject_key, discipline, lesson_date,
	due_date, due_note, body, origin, done_at, saved_at, created_at`

func scanHomework(row pgx.Row) (domain.Homework, error) {
	var h domain.Homework
	err := row.Scan(&h.ID, &h.OwnerKey, &h.NoteID, &h.Lesson.SubjectKey, &h.Lesson.Discipline,
		&h.Lesson.Date, &h.DueDate, &h.DueNote, &h.Body, &h.Origin, &h.DoneAt, &h.SavedAt, &h.CreatedAt)
	return h, err
}

func (r *Repo) CreateHomework(ctx context.Context, h domain.Homework) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO homeworks
		(owner_key, note_id, subject_key, discipline, lesson_date, due_date, due_note, body, origin)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		h.OwnerKey, h.NoteID, h.Lesson.SubjectKey, h.Lesson.Discipline, h.Lesson.Date,
		h.DueDate, h.DueNote, h.Body, h.Origin).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("создание задания: %w", err)
	}
	return id, nil
}

func (r *Repo) Homeworks(ctx context.Context, owner, subjectKey, discipline string, onlySaved bool) ([]domain.Homework, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+homeworkColumns+` FROM homeworks
		WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3 AND (NOT $4 OR saved_at IS NOT NULL)
		ORDER BY done_at IS NOT NULL, COALESCE(due_date, lesson_date) DESC, id DESC`,
		owner, subjectKey, discipline, onlySaved)
	if err != nil {
		return nil, fmt.Errorf("список заданий: %w", err)
	}
	defer rows.Close()
	var out []domain.Homework
	for rows.Next() {
		h, err := scanHomework(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repo) HomeworksByNote(ctx context.Context, owner string, noteID int64) ([]domain.Homework, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+homeworkColumns+` FROM homeworks
		WHERE owner_key=$1 AND note_id=$2 ORDER BY id`, owner, noteID)
	if err != nil {
		return nil, fmt.Errorf("задания конспекта: %w", err)
	}
	defer rows.Close()
	var out []domain.Homework
	for rows.Next() {
		h, err := scanHomework(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *Repo) SaveHomework(ctx context.Context, owner string, id int64, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE homeworks SET saved_at=COALESCE(saved_at,$3)
		WHERE id=$1 AND owner_key=$2`, id, owner, at)
	if err != nil {
		return fmt.Errorf("сохранение задания: %w", err)
	}
	return nil
}

func (r *Repo) SetHomeworkDone(ctx context.Context, owner string, id int64, at *time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE homeworks SET done_at=$3 WHERE id=$1 AND owner_key=$2`, id, owner, at)
	if err != nil {
		return fmt.Errorf("отметка задания: %w", err)
	}
	return nil
}

func (r *Repo) DeleteHomework(ctx context.Context, owner string, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM homeworks WHERE id=$1 AND owner_key=$2`, id, owner)
	if err != nil {
		return fmt.Errorf("удаление задания: %w", err)
	}
	return nil
}

// ─── сводка по предметам ─────────────────────────────────────────────────────

// Disciplines собирает предметы из всего, что у человека есть: записей,
// конспектов и заданий. Один запрос вместо трёх — список открывается на
// каждом заходе в раздел.
func (r *Repo) Disciplines(ctx context.Context, owner, subjectKey string) ([]notes.Discipline, error) {
	rows, err := r.pool.Query(ctx, `
		WITH all_items AS (
		    SELECT discipline, lesson_date, 0 AS notes, 0 AS homework
		      FROM recordings WHERE owner_key=$1 AND subject_key=$2
		    UNION ALL
		    SELECT discipline, lesson_date, 1, 0
		      FROM notes WHERE owner_key=$1 AND subject_key=$2 AND saved_at IS NOT NULL
		    UNION ALL
		    SELECT discipline, lesson_date, 0, 1
		      FROM homeworks
		     WHERE owner_key=$1 AND subject_key=$2 AND saved_at IS NOT NULL AND done_at IS NULL
		)
		SELECT discipline, sum(notes), sum(homework), max(lesson_date)
		  FROM all_items GROUP BY discipline ORDER BY max(lesson_date) DESC`, owner, subjectKey)
	if err != nil {
		return nil, fmt.Errorf("предметы с конспектами: %w", err)
	}
	defer rows.Close()
	var out []notes.Discipline
	for rows.Next() {
		var d notes.Discipline
		if err := rows.Scan(&d.Name, &d.Notes, &d.Homework, &d.LastLesson); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CleanupDrafts убирает несохранённые конспекты и их задания.
func (r *Repo) CleanupDrafts(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM homeworks WHERE saved_at IS NULL AND created_at < $1`, cutoff); err != nil {
		return 0, fmt.Errorf("уборка заданий-черновиков: %w", err)
	}
	tag, err := tx.Exec(ctx, `DELETE FROM notes WHERE saved_at IS NULL AND created_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("уборка конспектов-черновиков: %w", err)
	}
	return tag.RowsAffected(), tx.Commit(ctx)
}

// StuckAudio — записи, приём которых начался и не кончился: вкладку закрыли
// посреди пары. Их файлы занимают диск, а обработать их нечем — куски
// оборваны на полуслове.
func (r *Repo) StuckAudio(ctx context.Context, olderThan time.Duration) ([]domain.Recording, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+recordingColumns+` FROM recordings
		WHERE status=$1 AND updated_at < $2`, string(domain.StatusUploading), time.Now().Add(-olderThan))
	if err != nil {
		return nil, fmt.Errorf("брошенные записи: %w", err)
	}
	defer rows.Close()
	var out []domain.Recording
	for rows.Next() {
		rec, err := scanRecording(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// LogLLMCall записывает вызов модели. Ошибка записи учёта не должна ронять
// конспект, поэтому вызывающий её только логирует.
func (r *Repo) LogLLMCall(ctx context.Context, c domain.LLMCall) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO llm_calls
		(at, kind, model, ok, prompt_tokens, completion_tokens, duration_ms, code, message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		c.At, c.Kind, c.Model, c.OK, c.PromptTokens, c.CompletionTokens, c.Duration.Milliseconds(), c.Code, c.Message)
	if err != nil {
		return fmt.Errorf("учёт вызова модели: %w", err)
	}
	return nil
}

func (r *Repo) CleanupLLMCalls(ctx context.Context, olderThan time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM llm_calls WHERE at < now() - $1::interval`,
		fmt.Sprintf("%d seconds", int64(olderThan.Seconds())))
	if err != nil {
		return 0, fmt.Errorf("уборка учёта модели: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ─── карточки ────────────────────────────────────────────────────────────────

func (r *Repo) QueueCards(ctx context.Context, owner string, id int64, now time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE notes SET cards_status='queued', cards_failure='', cards_at=$3
		WHERE id=$1 AND owner_key=$2 AND saved_at IS NOT NULL AND cards_status NOT IN ('queued','working')`, id, owner, now)
	if err != nil {
		return false, fmt.Errorf("очередь карточек: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repo) ClaimCards(ctx context.Context, now time.Time) (domain.Note, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Note{}, false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM notes
		WHERE cards_status='queued' OR (cards_status='working' AND cards_at < $1)
		ORDER BY cards_at LIMIT 1 FOR UPDATE SKIP LOCKED`, now.Add(-time.Hour)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, false, nil
	}
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("выбор конспекта для карточек: %w", err)
	}
	n, err := scanNote(tx.QueryRow(ctx, `UPDATE notes SET cards_status='working', cards_at=$2
		WHERE id=$1 RETURNING `+noteColumns, id, now))
	if err != nil {
		return domain.Note{}, false, fmt.Errorf("взятие конспекта в работу: %w", err)
	}
	return n, true, tx.Commit(ctx)
}

func (r *Repo) FinishCards(ctx context.Context, noteID int64, failure string) error {
	status := "ready"
	if failure != "" {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx, `UPDATE notes SET cards_status=$2, cards_failure=$3 WHERE id=$1`, noteID, status, failure)
	if err != nil {
		return fmt.Errorf("итог карточек: %w", err)
	}
	return nil
}

func (r *Repo) AddCards(ctx context.Context, cards []domain.Card) (int, error) {
	added := 0
	for _, c := range cards {
		tag, err := r.pool.Exec(ctx, `INSERT INTO cards
			(owner_key, note_id, subject_key, discipline, lesson_date, kind, front, back, box, due_on)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT DO NOTHING`,
			c.OwnerKey, c.NoteID, c.Lesson.SubjectKey, c.Lesson.Discipline, c.Lesson.Date, string(c.Kind),
			c.Front, c.Back, c.Box, c.DueOn)
		if err != nil {
			return added, fmt.Errorf("запись карточки: %w", err)
		}
		added += int(tag.RowsAffected())
	}
	return added, nil
}

const cardColumns = `id, owner_key, note_id, subject_key, discipline, lesson_date, kind, front, back, box, due_on`

func scanCard(row pgx.Row) (domain.Card, error) {
	var c domain.Card
	var kind string
	err := row.Scan(&c.ID, &c.OwnerKey, &c.NoteID, &c.Lesson.SubjectKey, &c.Lesson.Discipline, &c.Lesson.Date,
		&kind, &c.Front, &c.Back, &c.Box, &c.DueOn)
	c.Kind = domain.CardKind(kind)
	return c, err
}

func (r *Repo) cards(ctx context.Context, query string, args ...any) ([]domain.Card, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("карточки: %w", err)
	}
	defer rows.Close()
	var out []domain.Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DueCards — карточки к повторению: сначала те, что дольше ждут, внутри —
// из младших коробок, то есть хуже выученные.
func (r *Repo) DueCards(ctx context.Context, owner, subjectKey, discipline string, day time.Time, limit int) ([]domain.Card, error) {
	return r.cards(ctx, `SELECT `+cardColumns+` FROM cards
		WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3 AND kind='card' AND due_on <= $4
		ORDER BY due_on, box, reviewed_at NULLS FIRST, id LIMIT $5`, owner, subjectKey, discipline, day, limit)
}

func (r *Repo) CardStats(ctx context.Context, owner, subjectKey, discipline string, day time.Time) (int, int, *time.Time, error) {
	var due, total int
	var next *time.Time
	err := r.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE due_on <= $4), count(*), min(due_on) FILTER (WHERE due_on > $4)
		FROM cards WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3 AND kind='card'`,
		owner, subjectKey, discipline, day).Scan(&due, &total, &next)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("счёт карточек: %w", err)
	}
	return due, total, next, nil
}

func (r *Repo) Terms(ctx context.Context, owner, subjectKey, discipline string) ([]domain.Card, error) {
	return r.cards(ctx, `SELECT `+cardColumns+` FROM cards
		WHERE owner_key=$1 AND subject_key=$2 AND discipline=$3 AND kind='term'
		ORDER BY lower(front)`, owner, subjectKey, discipline)
}

func (r *Repo) Card(ctx context.Context, owner string, id int64) (domain.Card, bool, error) {
	c, err := scanCard(r.pool.QueryRow(ctx, `SELECT `+cardColumns+` FROM cards WHERE id=$1 AND owner_key=$2`, id, owner))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Card{}, false, nil
	}
	if err != nil {
		return domain.Card{}, false, fmt.Errorf("карточка: %w", err)
	}
	return c, true, nil
}

func (r *Repo) SaveReview(ctx context.Context, c domain.Card, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE cards SET box=$3, due_on=$4, reviewed_at=$5 WHERE id=$1 AND owner_key=$2`,
		c.ID, c.OwnerKey, c.Box, c.DueOn, at)
	if err != nil {
		return fmt.Errorf("повторение карточки: %w", err)
	}
	return nil
}
