package domain

import "testing"

func room(lessons ...Lesson) RoomView { return RoomView{Lessons: lessons} }

func TestPeremenaSchitaetParyKotoryeRashodyatsya(t *testing.T) {
	views := []RoomView{
		room(lesson("10:10", "11:40", "a"), lesson("11:50", "13:20", "b")),
		room(lesson("10:10", "11:40", "c"), lesson("14:00", "15:30", "d")),
		// вне сетки: 11:35 ближе всего к концу второй пары — та же перемена
		room(lesson("10:05", "11:35", "e")),
		room(lesson("11:50", "13:20", "f")),
	}
	br := BuildBreaks(views, "09:00")
	if len(br) != len(Slots)-1 {
		t.Fatalf("перемен %d, ожидалось %d", len(br), len(Slots)-1)
	}
	if br[1].From != "11:40" || br[1].Ending != 3 || br[1].Starting != 2 {
		t.Errorf("перемена 11:40: %+v", br[1])
	}
	if br[2].From != "13:20" || br[2].Ending != 2 || br[2].Starting != 1 {
		t.Errorf("перемена 13:20: %+v", br[2])
	}
	if !br[1].Peak || br[2].Peak {
		t.Error("самая людная — 11:40")
	}
}

func TestPeremenyProshliIIdut(t *testing.T) {
	br := BuildBreaks(nil, "11:45")
	if br[0].State != "past" || br[1].State != "now" || br[2].State != "next" || br[3].State != "later" {
		t.Errorf("состояния: %s %s %s %s", br[0].State, br[1].State, br[2].State, br[3].State)
	}
	for _, b := range br {
		if b.Peak {
			t.Error("без пар пиковой перемены нет")
		}
	}
}
