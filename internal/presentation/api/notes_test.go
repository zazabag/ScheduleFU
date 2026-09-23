package api

import (
	"testing"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

// Пустая группа заводила запись «ничьей»: GroupSubject всегда ставит вид
// «группа», и «group:» с пустым именем проходил IsZero. UI так не делает,
// но ручка открыта всем, а прямой запрос с пустой группой и заданным
// предметом создавал бы бесхозную запись.
func TestZapisSPustoyGruppoyNeZavoditsya(t *testing.T) {
	if !startSubject(startRequest{Group: "", Discipline: "История"}).IsZero() {
		t.Error("пустая группа принята за владельца расписания")
	}
	if !startSubject(startRequest{Group: "   "}).IsZero() {
		t.Error("группа из пробелов принята за владельца расписания")
	}
}

func TestZapisSGruppoyIliPrepodavatelemZavoditsya(t *testing.T) {
	g := startSubject(startRequest{Group: "ПИ24-1"})
	if g.Kind != sched.SubjectGroup || g.Group != "ПИ24-1" {
		t.Errorf("группа: %+v", g)
	}
	// Преподаватель важнее группы, если пришли оба: у ручки один владелец.
	l := startSubject(startRequest{Group: "ПИ24-1", Lecturer: 46674})
	if l.Kind != sched.SubjectLecturer || l.LecturerOid != 46674 {
		t.Errorf("преподаватель: %+v", l)
	}
}
