package collector

import (
	"testing"

	"github.com/zazabag/schedulefu/internal/ruz"
)

func TestParseStream(t *testing.T) {
	cases := []struct {
		stream, group string
		want          []string
	}{
		{"ПИ24-1; ПИ24-2; ПИ24-3", "", []string{"ПИ24-1", "ПИ24-2", "ПИ24-3"}},
		{"  ПИ24-1 ;  ПИ24-2  ", "", []string{"ПИ24-1", "ПИ24-2"}},
		// Повтор в потоке не должен порождать дубль: по этому списку
		// адресуются уведомления, дубли дали бы их по два раза.
		{"ПИ24-1; ПИ24-1", "", []string{"ПИ24-1"}},
		// У пары поле group почти всегда пустое, но если stream пуст —
		// берём его.
		{"", "ПИ24-1", []string{"ПИ24-1"}},
		{"", "", []string{}},
	}
	for _, c := range cases {
		got := ParseStream(c.stream, c.group)
		if len(got) != len(c.want) {
			t.Errorf("ParseStream(%q,%q) = %v, ожидалось %v", c.stream, c.group, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseStream(%q,%q) = %v, ожидалось %v", c.stream, c.group, got, c.want)
				break
			}
		}
	}
}

// TestParseSourceTime закрепляет разбор нестандартной метки источника:
// "2026-09-09T10:51:08Z00:00" — это не RFC3339, у которого не бывает
// одновременно Z и смещения.
func TestParseSourceTime(t *testing.T) {
	ts, ok := parseSourceTime("2026-09-09T10:51:08Z00:00")
	if !ok {
		t.Fatal("метка источника не разобрана")
	}
	if ts.Year() != 2026 || ts.Month() != 9 || ts.Day() != 9 || ts.Hour() != 10 {
		t.Fatalf("разобрано неверно: %s", ts)
	}
	if _, ok := parseSourceTime(""); ok {
		t.Error("пустая метка не должна разбираться")
	}
	if _, ok := parseSourceTime("мусор"); ok {
		t.Error("мусор не должен разбираться")
	}
}

func TestToStoreLesson(t *testing.T) {
	src := ruz.Lesson{
		LessonOid:        2373615,
		Date:             "2026-09-08",
		BeginLesson:      "08:30",
		EndLesson:        "10:00",
		Auditorium:       "ЛП49/2/313",
		AuditoriumOid:    2851,
		AuditoriumAmount: 54,
		Building:         "Ленинградский проспект, 49/2",
		Discipline:       "Философия",
		KindOfWork:       "Семинар",
		Lecturer:         "Замараева Е.И.",
		LecturerOid:      46674,
		Stream:           "ПИ24-1; ПИ24-2; ПИ24-3",
		ModifiedDate:     "2026-09-09T10:51:08Z00:00",
	}
	got, ok := ToStoreLesson(src)
	if !ok {
		t.Fatal("пара отвергнута")
	}
	if got.LessonOid != 2373615 {
		t.Errorf("LessonOid = %d", got.LessonOid)
	}
	if got.BeginsAt != "08:30" || got.EndsAt != "10:00" {
		t.Errorf("время = %s-%s", got.BeginsAt, got.EndsAt)
	}
	if len(got.GroupNames) != 3 {
		t.Errorf("групп = %d, ожидалось 3", len(got.GroupNames))
	}
	if got.AuditoriumOid == nil || *got.AuditoriumOid != 2851 {
		t.Error("аудитория не проставлена")
	}
	if got.LecturerOid == nil || *got.LecturerOid != 46674 {
		t.Error("преподаватель не проставлен")
	}
	if got.SourceModifiedAt == nil {
		t.Error("метка правки источника не разобрана")
	}
}

// TestToStoreLessonOtvergaetNeprigodnye: пара без идентификатора или даты
// бесполезна — её нельзя ни разместить, ни сопоставить между слепками.
func TestToStoreLessonOtvergaetNeprigodnye(t *testing.T) {
	base := ruz.Lesson{
		LessonOid: 1, Date: "2026-09-08",
		BeginLesson: "08:30", EndLesson: "10:00",
	}
	if _, ok := ToStoreLesson(base); !ok {
		t.Fatal("нормальная пара отвергнута")
	}

	noOid := base
	noOid.LessonOid = 0
	if _, ok := ToStoreLesson(noOid); ok {
		t.Error("пара без lessonOid принята")
	}

	badDate := base
	badDate.Date = "не дата"
	if _, ok := ToStoreLesson(badDate); ok {
		t.Error("пара с нечитаемой датой принята")
	}

	noTime := base
	noTime.BeginLesson = ""
	if _, ok := ToStoreLesson(noTime); ok {
		t.Error("пара без времени принята")
	}

	deleted := base
	deleted.DeletionMark = 1
	if _, ok := ToStoreLesson(deleted); ok {
		t.Error("помеченная удалённой пара принята")
	}
}
