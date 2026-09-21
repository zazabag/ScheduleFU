// Package source — источники расписания.
//
// Здесь порт и типы на границе; реализации — в подпакетах (ruz). Порт
// объявлен у поставщика, а не у вызывающего, в отступление от общего
// правила: реализаций несколько (та же платформа РУЗ стоит в ВШЭ, СПбПУ,
// САФУ), а вызывающий один. Так второй вуз — это новая реализация или новая
// таблица правил, а не правка ядра.
package source

import (
	"context"
	"time"
)

// Kind — измерение, по которому запрашивается расписание.
type Kind string

const (
	KindGroup      Kind = "group"
	KindLecturer   Kind = "lecturer"
	KindAuditorium Kind = "auditorium"
)

// SearchKind — тип сущности в поиске. Поиска людей (person) здесь нет
// намеренно: он возвращает GUID, непригодный для запроса расписания.
type SearchKind string

const (
	SearchGroup      SearchKind = "group"
	SearchLecturer   SearchKind = "lecturer"
	SearchAuditorium SearchKind = "auditorium"
)

// SearchResult — элемент выдачи поиска.
type SearchResult struct {
	Type        string
	ID          string // числовой для group/lecturer/auditorium
	Label       string
	Description string // у аудитории «название | корпус | тип», у группы — номер факультета
}

// Lesson — пара так, как её отдаёт источник, уже без лишних полей.
// Разбор дат и времени — дело вызывающего: источник отдаёт строки.
type Lesson struct {
	LessonOid        int64
	Date             string // 2026-09-08
	BeginLesson      string // 08:30
	EndLesson        string
	Auditorium       string
	AuditoriumOid    int64
	AuditoriumAmount int
	Building         string
	Discipline       string
	KindOfWork       string
	Lecturer         string
	LecturerOid      int64
	Stream           string
	Group            string
	SubGroup         string
	Note             string
	ModifiedDate     string
	DeletionMark     int
}

// Auditorium — разобранная аудитория: всё, что источник знает о ней в
// одном имени и адресе. Правила разбора (этажи, площадки, мусор) — у
// реализации: они разные в каждом вузе и даже в каждом корпусе.
type Auditorium struct {
	Name         string
	Room         string
	Building     string
	Site         Site
	Floor        *int // nil — правило корпуса не подтверждено; не угадывается
	IsReal       bool // не «Без аудитории», не «д.а.», не «тест»
	IsStudySpace bool // не спортзал и не чужое помещение
}

// Site — площадка. Ленинградские 49, 51 и 55 — одна площадка: физически
// одно место с переходами, а у источника четыре адреса.
type Site struct {
	Slug  string
	Label string
	Order int
}

// Source — то, что модуль schedule хочет от источника расписания.
type Source interface {
	Search(ctx context.Context, kind SearchKind, term string) ([]SearchResult, error)
	Schedule(ctx context.Context, kind Kind, id int64, from, to time.Time) ([]Lesson, error)
	// ParseAuditorium разбирает имя и адрес. Kind у результата не
	// заполняется: тип аудитории приходит только из поиска.
	ParseAuditorium(name, building string) Auditorium
}
