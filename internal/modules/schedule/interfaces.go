// Package schedule — ядро: слепки расписания, поиск изменений, справочники,
// запросы «что свободно» и «где кто».
//
// Модуль владеет таблицами lessons, lesson_changes, auditoriums, groups,
// lecturers, collector_runs. Пишет в них только он.
package schedule

import (
	"context"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// Repository — хранилище модуля. Реализация — infrastructure/postgres.
type Repository interface {
	// ApplySnapshot заменяет окно [from, to] слепком и возвращает итог.
	// Период задаётся явно: день, из которого источник убрал все пары, иначе
	// остался бы в базе навсегда — мы бы не увидели, что там что-то было.
	ApplySnapshot(ctx context.Context, from, to time.Time, lessons []domain.Lesson) (domain.ApplyResult, error)
	ChangesSince(ctx context.Context, since time.Time, limit int) ([]domain.Change, error)
	CleanupChanges(ctx context.Context, olderThan time.Duration) (int64, error)

	UpsertAuditoriums(ctx context.Context, items []domain.Auditorium) error
	UpsertGroups(ctx context.Context, items []domain.Group) error
	// MissingGroups — имена из списка, которых нет в справочнике групп.
	MissingGroups(ctx context.Context, names []string) ([]string, error)
	UpsertLecturers(ctx context.Context, items []domain.Lecturer) error
	AuditoriumOids(ctx context.Context, onlyStudySpaces bool) ([]int64, error)
	LecturerName(ctx context.Context, oid int64) (string, error)
	SearchGroups(ctx context.Context, query string, limit int) ([]domain.Group, error)
	SearchLecturers(ctx context.Context, query string, limit int) ([]domain.Lecturer, error)

	Sites(ctx context.Context) ([]SiteRow, error)
	SiteDay(ctx context.Context, site string, date time.Time) ([]RoomDay, error)
	ScheduleFor(ctx context.Context, s domain.Subject, from, to time.Time) ([]domain.Lesson, error)
	OccupancyForAuditorium(ctx context.Context, oid int64, date time.Time) ([]domain.Lesson, error)
	// Days и LessonsOn нужны выгрузке для Pages: там данные раскладываются по
	// дням, один файл на день.
	Days(ctx context.Context) ([]time.Time, error)
	LessonsOn(ctx context.Context, date time.Time) ([]domain.Lesson, error)
	Auditoriums(ctx context.Context) ([]domain.Auditorium, error)
	GroupNames(ctx context.Context) ([]string, error)
	Lecturers(ctx context.Context) ([]domain.Lecturer, error)

	// Ленивая привязка языковых подгрупп: когда группу дотягивали в последний
	// раз, и запись связей «пара → группа» из ответа вуза.
	GroupFetchedOn(ctx context.Context, group string) (time.Time, bool, error)
	ApplyGroupLinks(ctx context.Context, group string, lessonOids []int64, on time.Time) error
	GroupID(ctx context.Context, name string) (int64, bool, error)

	StartRun(ctx context.Context, from, to time.Time) (int64, error)
	FinishRun(ctx context.Context, id int64, requests, errs, lessons, changes int, failure error) error
	LastSuccessfulRun(ctx context.Context) (time.Time, bool, error)
}

// SiteRow — площадка с числом учебных аудиторий для переключателя.
type SiteRow struct {
	Site  domain.Site
	Rooms int
}

// RoomDay — аудитория вместе с её парами на дату.
type RoomDay struct {
	Auditorium domain.Auditorium
	Lessons    []domain.Lesson
}

// ChangeReader — порт для тех, кому нужны изменения (notify).
// Объявлен здесь, потому что реализует его этот модуль; вызывающие
// объявляют свою копию у себя — Go сопоставит по форме.
type ChangeReader interface {
	ChangesSince(ctx context.Context, since time.Time, limit int) ([]domain.Change, error)
}
