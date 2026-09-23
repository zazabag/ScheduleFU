package domain

// Auditorium — аудитория справочника.
type Auditorium struct {
	Oid      int64
	Name     string // как в источнике: «ЛП49/2/313»
	Room     string // номер или название: «313», «Коворкинг № 1»
	Building string // адрес из источника
	Site     Site
	Kind     string // «Лекционная», «Коворкинг», …
	// Floor — nil, когда правило нумерации корпуса не подтверждено.
	// Осознанно указатель: ноль означал бы первый этаж.
	Floor *int
	// Capacity — nil, когда неизвестна: источник отдаёт вместимость только
	// вместе с парой, и у пустующих аудиторий её нет.
	Capacity     *int
	IsStudySpace bool
}

// Site — площадка: здание или группа зданий, которые человек воспринимает
// как одно место. Ленинградские 49, 51 и 55 — одна площадка; у источника
// это четыре разных адреса, склейку делает адаптер источника.
type Site struct {
	Slug  string // ключ в ссылках и хранилище
	Label string
	Order int // порядок в переключателе: задан руками, не по числу аудиторий
}

// Group — учебная группа.
type Group struct {
	ID            int64
	Name          string
	FacultyOid    string // источник отдаёт только номер, без названия
	AdmissionYear *int
}

// DisciplineHit — дисциплина в текущем окне расписания: кто её ведёт,
// каким группам, где и в каком виде. Отвечает на вопрос выбора дисциплины
// и на «кто у нас ведёт эконометрику».
type DisciplineHit struct {
	Name      string
	Lessons   int
	Lecturers []Lecturer
	Groups    []string
	Kinds     []string
	Buildings []string
}

// Lecturer — преподаватель. Oid — единственный допустимый ключ.
type Lecturer struct {
	Oid  int64
	Name string
}

// Course — курс группы в учебном году, начинающемся в сентябре.
// 0 — не определить.
func (g Group) Course(academicYear int) int {
	if g.AdmissionYear == nil {
		return 0
	}
	c := academicYear - *g.AdmissionYear + 1
	if c < 1 || c > 6 {
		return 0
	}
	return c
}
