// Package ruz — клиент открытого API системы расписания ruz.fa.ru.
//
// Это единственное место в проекте, которое знает о странностях источника.
// Всё, что отдаётся наружу, уже нормализовано: выше по стеку не должно быть
// ни разбора номеров аудиторий, ни знания о том, что этаж приходит нулём.
package ruz

// Kind — измерение, в котором запрашивается расписание.
//
// У всех трёх один и тот же вид URL, но ключи разные, и это важно:
// KindLecturer работает ТОЛЬКО с числовым lecturerOid. Если передать
// GUID преподавателя, API не вернёт ошибку — он вернёт сотни чужих пар с
// пустым полем Lecturer. Поэтому идентификатор здесь — целое число, а не
// строка: невалидный ключ не должен быть представим.
type Kind string

const (
	KindGroup      Kind = "group"
	KindLecturer   Kind = "lecturer"
	KindAuditorium Kind = "auditorium"
)

// SearchKind — тип сущности в поиске.
//
// SearchPerson существует в API, но возвращает GUID, непригодный для
// запроса расписания. Он намеренно не объявлен, чтобы им нельзя было
// воспользоваться по ошибке.
type SearchKind string

const (
	SearchGroup      SearchKind = "group"
	SearchLecturer   SearchKind = "lecturer"
	SearchAuditorium SearchKind = "auditorium"
)

// SearchResult — элемент выдачи /api/search.
type SearchResult struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Label string `json:"label"`
	// Description у аудитории — "название | корпус | тип",
	// у группы — числовой oid факультета без названия,
	// у преподавателя — кафедра.
	Description string `json:"description"`
	GUID        string `json:"guid"`
}

// Lesson — одна пара. Поля названы как в API, чтобы при отладке можно было
// сверяться с сырым ответом; из полутора сотен полей взяты только значимые.
type Lesson struct {
	// LessonOid стабилен между выгрузками и служит ключом при поиске
	// изменений в расписании.
	LessonOid     int64  `json:"lessonOid"`
	Date          string `json:"date"`        // 2026-09-08
	BeginLesson   string `json:"beginLesson"` // 08:30
	EndLesson     string `json:"endLesson"`   // 10:00
	DayOfWeek     int    `json:"dayOfWeek"`
	Discipline    string `json:"discipline"`
	KindOfWork    string `json:"kindOfWork"`
	Auditorium    string `json:"auditorium"`
	AuditoriumOid int64  `json:"auditoriumOid"`
	// AuditoriumAmount — вместимость. Приходит только вместе с парой,
	// поэтому у пустующих аудиторий вместимость неизвестна.
	AuditoriumAmount int    `json:"auditoriumAmount"`
	Building         string `json:"building"`
	BuildingOid      int64  `json:"buildingOid"`
	// AuditoriumFloor всегда 0 — источник его не заполняет.
	// Этаж определяется разбором номера, см. ParseAuditorium.
	AuditoriumFloor int `json:"auditoriumfloor"`

	Lecturer    string `json:"lecturer"`
	LecturerOid int64  `json:"lecturerOid"`
	// Stream — поток: "ПИ24-1; ПИ24-2; ПИ24-3". Поле Group у пары обычно
	// пустое, состав групп берётся отсюда.
	Stream    string `json:"stream"`
	StreamOid int64  `json:"streamOid"`
	Group     string `json:"group"`
	SubGroup  string `json:"subGroup"`

	// ModifiedDate показывает, когда деканат последний раз правил пару.
	ModifiedDate string `json:"modifieddate"`
	CreatedDate  string `json:"createddate"`
	Note         string `json:"note"`
	URL1         string `json:"url1"`
	DeletionMark int    `json:"deletion_mark"`
}
