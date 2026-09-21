// Package domain — язык модуля schedule: пара, аудитория, площадка,
// владелец расписания, изменение, сетка пар.
//
// Пакет не импортирует ничего из проекта. Странности источника сюда не
// доходят: их гасит модуль source, а здесь данные уже чистые.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// Lesson — одна пара в окне расписания.
type Lesson struct {
	// LessonOid стабилен между выгрузками источника: по нему пара
	// сопоставляется со вчерашней, и по нему же ищутся изменения.
	LessonOid int64
	Date      time.Time
	BeginsAt  string // ЧЧ:ММ; строковый порядок совпадает с хронологическим
	EndsAt    string

	AuditoriumOid *int64
	Auditorium    string
	Building      string

	Discipline string
	KindOfWork string

	LecturerOid  *int64
	LecturerName string

	Stream     string
	GroupNames []string
	Subgroup   string
	Note       string

	SourceModifiedAt *time.Time
}

// Fingerprint — отпечаток значимых полей пары.
//
// Сравнение слепков идёт по отпечатку, а не поле за полем: так нельзя
// забыть новое поле. Входит только то, изменение чего человек заметит и о
// чём его стоит уведомить. Служебные поля источника — кто правил, когда
// создана запись — намеренно снаружи: иначе техническая правка деканата
// давала бы ложное уведомление.
func (l Lesson) Fingerprint() string {
	var b strings.Builder
	sep := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
			b.WriteByte(0x1f)
		}
	}
	aud, lec := "", ""
	if l.AuditoriumOid != nil {
		aud = strconv.FormatInt(*l.AuditoriumOid, 10)
	}
	if l.LecturerOid != nil {
		lec = strconv.FormatInt(*l.LecturerOid, 10)
	}
	sep(l.Date.Format("2006-01-02"), l.BeginsAt, l.EndsAt,
		aud, l.Auditorium, l.Discipline, l.KindOfWork,
		lec, l.LecturerName, strings.Join(l.GroupNames, ","), l.Subgroup, l.Note)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:16])
}

// DateKey — дата пары как «ГГГГ-ММ-ДД».
func (l Lesson) DateKey() string { return l.Date.Format("2006-01-02") }

// Covers сообщает, идёт ли пара в момент hhmm.
// Ровно в момент начала — уже идёт, ровно в момент конца — уже нет.
func (l Lesson) Covers(hhmm string) bool { return l.BeginsAt <= hhmm && hhmm < l.EndsAt }

// Overlaps сообщает, пересекается ли пара с интервалом [from, to).
func (l Lesson) Overlaps(from, to string) bool { return l.BeginsAt < to && from < l.EndsAt }
