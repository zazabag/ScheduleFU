package schedule

import (
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/source"
)

var msk = time.FixedZone("MSK", 3*3600)

func TestParseStream(t *testing.T) {
	cases := []struct {
		stream, group string
		want          []string
	}{
		{"ПИ24-1; ПИ24-2; ПИ24-3", "", []string{"ПИ24-1", "ПИ24-2", "ПИ24-3"}},
		{"  ПИ24-1 ;  ПИ24-2  ", "", []string{"ПИ24-1", "ПИ24-2"}},
		// Дубль в потоке дал бы уведомление дважды.
		{"ПИ24-1; ПИ24-1", "", []string{"ПИ24-1"}},
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
			}
		}
	}
}

// TestParseSourceTime закрепляет нестандартную метку источника:
// «Z00:00» — одновременно Z и смещение, чего в RFC3339 не бывает.
func TestParseSourceTime(t *testing.T) {
	ts, ok := parseSourceTime("2026-09-09T10:51:08Z00:00", msk)
	if !ok || ts.Year() != 2026 || ts.Month() != 9 || ts.Day() != 9 || ts.Hour() != 10 {
		t.Fatalf("разобрано неверно: %v %s", ok, ts)
	}
	for _, bad := range []string{"", "мусор"} {
		if _, ok := parseSourceTime(bad, msk); ok {
			t.Errorf("%q не должно разбираться", bad)
		}
	}
}

func TestFromSource(t *testing.T) {
	src := source.Lesson{
		LessonOid: 2373615, Date: "2026-09-08", BeginLesson: "08:30", EndLesson: "10:00",
		Auditorium: "ЛП49/2/313", AuditoriumOid: 2851, Building: "Ленинградский проспект, 49/2",
		Discipline: "Философия", KindOfWork: "Семинар", Lecturer: "Замараева Е.И.", LecturerOid: 46674,
		Stream: "ПИ24-1; ПИ24-2; ПИ24-3", ModifiedDate: "2026-09-09T10:51:08Z00:00",
	}
	got, ok := fromSource(src, msk)
	if !ok {
		t.Fatal("пара отвергнута")
	}
	if got.LessonOid != 2373615 || got.BeginsAt != "08:30" || got.EndsAt != "10:00" {
		t.Errorf("основные поля: %+v", got)
	}
	if len(got.GroupNames) != 3 || got.AuditoriumOid == nil || *got.AuditoriumOid != 2851 ||
		got.LecturerOid == nil || *got.LecturerOid != 46674 || got.SourceModifiedAt == nil {
		t.Errorf("связи: %+v", got)
	}
}

// Пара без идентификатора, даты, времени или с пометкой удаления
// бесполезна: её нельзя ни разместить, ни сопоставить между слепками.
func TestFromSourceOtvergaetNeprigodnye(t *testing.T) {
	base := source.Lesson{LessonOid: 1, Date: "2026-09-08", BeginLesson: "08:30", EndLesson: "10:00"}
	if _, ok := fromSource(base, msk); !ok {
		t.Fatal("нормальная пара отвергнута")
	}
	bad := map[string]source.Lesson{}
	b := base
	b.LessonOid = 0
	bad["без oid"] = b
	b = base
	b.Date = "не дата"
	bad["без даты"] = b
	b = base
	b.BeginLesson = ""
	bad["без времени"] = b
	b = base
	b.DeletionMark = 1
	bad["удалённая"] = b
	for name, l := range bad {
		if _, ok := fromSource(l, msk); ok {
			t.Errorf("%s: принята", name)
		}
	}
}

// Спортзалы и чужие помещения — не место для занятий; тип известен только
// посеву, и без этой проверки обход раздувался с 541 до 767 аудиторий.
func TestStudySpaceKind(t *testing.T) {
	for kind, want := range map[string]bool{
		"Лекционная": true, "Коворкинг": true, "": true,
		"Спортивный зал": false, "Зал аэробики": false, "Тренажерный зал": false,
		"Помещение, не принадлежащее университету": false, "Лекционная online": false,
	} {
		if got := studySpaceKind(kind); got != want {
			t.Errorf("studySpaceKind(%q) = %v", kind, got)
		}
	}
}
