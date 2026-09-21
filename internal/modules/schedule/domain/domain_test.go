package domain

import (
	"net/url"
	"testing"
	"time"
)

func lesson(from, to, disc string) Lesson {
	return Lesson{BeginsAt: from, EndsAt: to, Discipline: disc}
}

func TestBuildRoomViewRaskladyvaetPary(t *testing.T) {
	v := BuildRoomView(Auditorium{}, []Lesson{lesson("08:30", "10:00", "Философия"), lesson("14:00", "15:30", "История")}, "11:00")
	if len(v.Cells) != len(Slots) {
		t.Fatalf("клеток %d, ожидалось %d", len(v.Cells), len(Slots))
	}
	if v.Cells[0].State != SlotBusy || v.Cells[1].State != SlotFree || v.Cells[3].State != SlotBusy {
		t.Errorf("состояния: %v %v %v", v.Cells[0].State, v.Cells[1].State, v.Cells[3].State)
	}
	if !v.FreeNow || v.FreeUntil != "14:00" {
		t.Errorf("FreeNow=%v FreeUntil=%q", v.FreeNow, v.FreeUntil)
	}
}

// Ради этого занятость считается пересечением: в данных вуза есть пары
// 08:30—16:45 и даже 08:00—22:00, и сравнение по началу показало бы такую
// аудиторию свободной почти весь день.
func TestDlinnayaParaZakryvaetNeskolkoSlotov(t *testing.T) {
	v := BuildRoomView(Auditorium{}, []Lesson{lesson("08:30", "16:45", "Тренинг")}, "09:00")
	for i := 0; i < 5; i++ {
		if v.Cells[i].State != SlotBusy {
			t.Errorf("слот %s должен быть занят", Slots[i].Begins)
		}
	}
	if v.Cells[5].State == SlotBusy || v.FreeNow {
		t.Error("17:20 после конца пары; в 09:00 аудитория занята")
	}
}

func TestProshedsheeOtlichaetsyaOtSvobodnogo(t *testing.T) {
	v := BuildRoomView(Auditorium{}, nil, "14:30")
	if v.Cells[0].State != SlotPast || v.Cells[3].State != SlotFree || v.Cells[7].State != SlotFree {
		t.Errorf("прошло/идёт/вечер: %v %v %v", v.Cells[0].State, v.Cells[3].State, v.Cells[7].State)
	}
	if !v.FreeNow || v.FreeUntil != "" {
		t.Errorf("без пар: FreeNow=%v FreeUntil=%q", v.FreeNow, v.FreeUntil)
	}
}

func TestGranicyPary(t *testing.T) {
	ls := []Lesson{lesson("08:30", "10:00", "Философия")}
	if !BuildRoomView(Auditorium{}, ls, "10:00").FreeNow {
		t.Error("в 10:00 пара закончилась")
	}
	if BuildRoomView(Auditorium{}, ls, "08:30").FreeNow {
		t.Error("в 08:30 пара началась")
	}
}

func TestPodpisiBezVeduschegoNulya(t *testing.T) {
	v := BuildRoomView(Auditorium{}, nil, "08:00")
	if v.Cells[0].Label != "8:30" || v.Cells[7].Label != "20:30" {
		t.Errorf("подписи: %q %q", v.Cells[0].Label, v.Cells[7].Label)
	}
}

func TestSubjectKeyTudaIObratno(t *testing.T) {
	for _, want := range []Subject{GroupSubject("ПИ24-1"), LecturerSubject(46674)} {
		got, err := ParseSubjectKey(want.Key())
		if err != nil || got != want {
			t.Errorf("ключ %q -> %+v (%v), ожидалось %+v", want.Key(), got, err, want)
		}
	}
}

// Ключ преподавателя — только число: по GUID источник отдаёт чужие пары.
func TestParseSubjectKeyOtvergaetGUID(t *testing.T) {
	for _, bad := range []string{"lecturer:d6607672-25a6-4ef6-8d02-69106f92cf1e", "lecturer:0", "lecturer:-5", "lecturer:", "person:46674", "ПИ24-1", ""} {
		if _, err := ParseSubjectKey(bad); err == nil {
			t.Errorf("ключ %q принят", bad)
		}
	}
}

func TestSubjectFromValues(t *testing.T) {
	if g, err := SubjectFromValues(url.Values{"group": {"Ю24-5"}}); err != nil || g.Group != "Ю24-5" {
		t.Errorf("группа: %+v %v", g, err)
	}
	if l, err := SubjectFromValues(url.Values{"lecturer": {"46674"}}); err != nil || l.LecturerOid != 46674 {
		t.Errorf("преподаватель: %+v %v", l, err)
	}
	if _, err := SubjectFromValues(url.Values{"lecturer": {"guid"}}); err == nil {
		t.Error("нечисловой преподаватель принят")
	}
	if e, err := SubjectFromValues(url.Values{}); err != nil || !e.IsZero() {
		t.Errorf("пусто: %+v %v", e, err)
	}
}

// Служебные поля источника не входят в отпечаток: техническая правка
// деканата не должна выглядеть изменением.
func TestFingerprintIgnoriruetSluzhebnye(t *testing.T) {
	a := Lesson{LessonOid: 1, Date: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), BeginsAt: "08:30", EndsAt: "10:00", Discipline: "Философия"}
	b := a
	ts := time.Now()
	b.SourceModifiedAt = &ts
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("метка правки источника изменила отпечаток")
	}
	c := a
	c.Discipline = "История"
	if a.Fingerprint() == c.Fingerprint() {
		t.Error("смена дисциплины не изменила отпечаток")
	}
}

func TestChangeSubjects(t *testing.T) {
	oid := int64(46674)
	c := Change{GroupNames: []string{"ПИ24-1", "", "ПИ24-2"}, LecturerOid: &oid}
	if got := c.Subjects(); len(got) != 3 || got[2].Kind != SubjectLecturer {
		t.Errorf("адресаты: %+v", got)
	}
}
