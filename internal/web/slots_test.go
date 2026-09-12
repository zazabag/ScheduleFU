package web

import (
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

func lesson(from, to, discipline string) store.LessonView {
	return store.LessonView{BeginsAt: from, EndsAt: to, Discipline: discipline}
}

func TestBuildRoomViewRaskladyvaetPary(t *testing.T) {
	day := store.RoomDay{Lessons: []store.LessonView{
		lesson("08:30", "10:00", "Философия"),
		lesson("14:00", "15:30", "История"),
	}}
	v := BuildRoomView(day, "11:00")

	if len(v.Cells) != len(Slots) {
		t.Fatalf("клеток %d, ожидалось %d", len(v.Cells), len(Slots))
	}
	if v.Cells[0].State != SlotBusy {
		t.Errorf("08:30 должна быть занята, получено %q", v.Cells[0].State)
	}
	// 10:10—11:40 идёт прямо сейчас и свободна, значит ещё впереди.
	if v.Cells[1].State != SlotFree {
		t.Errorf("10:10 должна быть свободна, получено %q", v.Cells[1].State)
	}
	if v.Cells[3].State != SlotBusy {
		t.Errorf("14:00 должна быть занята, получено %q", v.Cells[3].State)
	}
	if !v.FreeNow {
		t.Error("в 11:00 пары нет — аудитория свободна")
	}
	if v.FreeUntil != "14:00" {
		t.Errorf("FreeUntil = %q, ожидалось 14:00", v.FreeUntil)
	}
}

// TestDlinnayaParaZakryvaetNeskolkoSlotov — главный случай, ради которого
// занятость считается пересечением: в данных вуза есть пары вне сетки,
// вплоть до 08:00—22:00. Сравнение по времени начала показало бы такую
// аудиторию свободной почти весь день.
func TestDlinnayaParaZakryvaetNeskolkoSlotov(t *testing.T) {
	day := store.RoomDay{Lessons: []store.LessonView{
		lesson("08:30", "16:45", "Тренинг"),
	}}
	v := BuildRoomView(day, "09:00")

	for i := 0; i < 5; i++ { // слоты до 17:10
		if v.Cells[i].State != SlotBusy {
			t.Errorf("слот %s должен быть занят длинной парой, получено %q",
				Slots[i].Begins, v.Cells[i].State)
		}
	}
	if v.Cells[5].State == SlotBusy {
		t.Error("слот 17:20 начинается после конца пары и занят быть не должен")
	}
	if v.FreeNow {
		t.Error("в 09:00 идёт пара — аудитория занята")
	}
}

func TestProshedsheeVremyaOtlichaetsyaOtSvobodnogo(t *testing.T) {
	v := BuildRoomView(store.RoomDay{}, "14:30")

	if v.Cells[0].State != SlotPast {
		t.Errorf("08:30 к 14:30 уже прошла, получено %q", v.Cells[0].State)
	}
	// Слот 14:00—15:30 ещё идёт: он не прошёл.
	if v.Cells[3].State != SlotFree {
		t.Errorf("идущий слот должен считаться свободным, получено %q", v.Cells[3].State)
	}
	if v.Cells[7].State != SlotFree {
		t.Errorf("вечерний слот должен быть свободен, получено %q", v.Cells[7].State)
	}
	if !v.FreeNow {
		t.Error("пар нет — аудитория свободна")
	}
	if v.FreeUntil != "" {
		t.Errorf("пар нет, FreeUntil должен быть пуст, получено %q", v.FreeUntil)
	}
}

func TestGranicyPary(t *testing.T) {
	day := store.RoomDay{Lessons: []store.LessonView{lesson("08:30", "10:00", "Философия")}}

	// Ровно в момент окончания аудитория уже свободна.
	if v := BuildRoomView(day, "10:00"); !v.FreeNow {
		t.Error("в 10:00 пара закончилась — аудитория свободна")
	}
	// Ровно в момент начала — уже занята.
	if v := BuildRoomView(day, "08:30"); v.FreeNow {
		t.Error("в 08:30 пара началась — аудитория занята")
	}
}

func TestPodpisiSlotovBezVeduschegoNulya(t *testing.T) {
	v := BuildRoomView(store.RoomDay{}, "08:00")
	if v.Cells[0].Label != "8:30" {
		t.Errorf("подпись = %q, ожидалось 8:30", v.Cells[0].Label)
	}
	if v.Cells[7].Label != "20:30" {
		t.Errorf("подпись = %q, ожидалось 20:30", v.Cells[7].Label)
	}
}

func TestRusskieDaty(t *testing.T) {
	d := time.Date(2026, 9, 14, 15, 4, 0, 0, time.UTC)
	if got := FormatDateRu(d); got != "14 сентября" {
		t.Errorf("FormatDateRu = %q, ожидалось «14 сентября»", got)
	}
	if got := FormatDateTimeRu(d); got != "14 сентября в 15:04" {
		t.Errorf("FormatDateTimeRu = %q", got)
	}
	if got := WeekdayRu(d); got != "понедельник" {
		t.Errorf("WeekdayRu = %q", got)
	}
	if got := WeekdayShortRu(d); got != "пн" {
		t.Errorf("WeekdayShortRu = %q", got)
	}
	// Проверяем именно то, из-за чего появилась таблица: раскладка
	// time.Format печатала бы «января» для любого месяца.
	jan := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	if got := FormatDateRu(jan); got != "3 января" {
		t.Errorf("FormatDateRu(январь) = %q", got)
	}
	dec := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	if got := FormatDateRu(dec); got != "31 декабря" {
		t.Errorf("FormatDateRu(декабрь) = %q", got)
	}
}
