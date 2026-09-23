package web

import (
	"strings"
	"testing"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
)

func TestSamayaLyudnayaPeremenaNazyvaetsya(t *testing.T) {
	s := windowServer(t)
	// В windowRepo на площадке одна пара, 11:50—13:20: к перемене 13:20
	// она и расходится.
	code, body := get(t, s, "/rooms?site=leningradsky")
	if code != 200 {
		t.Fatalf("код %d", code)
	}
	if !strings.Contains(body, "Люднее всего в 13:20: расходится 1 пара") {
		t.Errorf("нет фразы про людную перемену")
	}
	if !strings.Contains(body, `class="break-fill peak"`) {
		t.Error("пиковая перемена не выделена")
	}
}

func TestBezParFrazyProPeremenyNet(t *testing.T) {
	rows, sentence := breakRows(sched.BuildBreaks(nil, "09:00"))
	if sentence != "" || len(rows) != len(sched.Slots)-1 {
		t.Errorf("без пар: %q, строк %d", sentence, len(rows))
	}
}
