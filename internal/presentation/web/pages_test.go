package web

import (
	"strings"
	"testing"
	"time"
)

func rows(times ...string) []lessonRow {
	var out []lessonRow
	for i := 0; i < len(times); i += 2 {
		out = append(out, lessonRow{BeginsAt: times[i], EndsAt: times[i+1], Discipline: "Пара " + times[i], Room: "204"})
	}
	return out
}

func TestStatusySegodnyaProshlaIdyotSleduyushchaya(t *testing.T) {
	rs := rows("08:30", "10:00", "10:10", "11:40", "11:50", "13:20", "14:00", "15:30")
	markStatuses(rs, "2026-09-24", "2026-09-24", "10:30")
	got := []string{rs[0].Status, rs[1].Status, rs[2].Status, rs[3].Status}
	want := []string{"past", "now", "next", "later"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("пара %d: статус %q, ожидался %q", i+1, got[i], want[i])
		}
	}
}

func TestStatusyDrugogoDnyaBezSeychas(t *testing.T) {
	rs := rows("08:30", "10:00", "10:10", "11:40")
	markStatuses(rs, "2026-09-25", "2026-09-24", "10:30")
	for _, r := range rs {
		if r.Status != "later" {
			t.Errorf("завтра всё впереди, а получили %q", r.Status)
		}
	}
	markStatuses(rs, "2026-09-23", "2026-09-24", "10:30")
	for _, r := range rs {
		if r.Status != "past" {
			t.Errorf("вчера всё прошло, а получили %q", r.Status)
		}
	}
}

func TestGeroyVPereryveZnaetSleduyushchuyu(t *testing.T) {
	s := &Server{}
	rs := rows("08:30", "10:00", "10:10", "11:40", "14:00", "15:30")
	markStatuses(rs, "2026-09-24", "2026-09-24", "12:00")
	h := s.buildHero(rs, true, "12:00")
	if h.State != "between" || h.Lesson == nil || h.Lesson.BeginsAt != "14:00" {
		t.Fatalf("перерыв должен показывать пару 14:00, получили %+v", h)
	}
	if !strings.Contains(h.Sentence, "Перерыв до 14:00") {
		t.Errorf("фраза не про перерыв: %q", h.Sentence)
	}
	if h.Stops != "3 остановки · ты на третьей" {
		t.Errorf("остановки: %q", h.Stops)
	}
}

func TestGeroyVoVremyaParySchitaetProgress(t *testing.T) {
	s := &Server{}
	rs := rows("10:10", "11:40")
	markStatuses(rs, "2026-09-24", "2026-09-24", "10:55")
	h := s.buildHero(rs, true, "10:55")
	if h.State != "now" || h.Progress != 50 || h.RemainMin != 45 {
		t.Errorf("на середине пары ожидали now/50/45, получили %s/%d/%d", h.State, h.Progress, h.RemainMin)
	}
	if h.Mood != "единственная пара — и свободен" {
		t.Errorf("настроение: %q", h.Mood)
	}
}

func TestDiapazonNedeliVOdnomMesyace(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 9, 21, 0, 0, 0, 0, loc)
	if got := weekRange(from, from.AddDate(0, 0, 6)); got != "21—27 сентября" {
		t.Errorf("получили %q", got)
	}
	from = time.Date(2026, 9, 28, 0, 0, 0, 0, loc)
	if got := weekRange(from, from.AddDate(0, 0, 6)); got != "28 сентября — 4 октября" {
		t.Errorf("на стыке месяцев получили %q", got)
	}
}

func TestPodpisiOstanovok(t *testing.T) {
	cases := map[[2]int]string{{1, 1}: "1 остановка · ты на первой", {4, 2}: "4 остановки · ты на второй", {5, 0}: "5 остановок"}
	for in, want := range cases {
		if got := stopsLabel(in[0], in[1]); got != want {
			t.Errorf("%v: %q, ожидалось %q", in, got, want)
		}
	}
}

func TestKorotkiyNomerAuditoriiBezSlovaAud(t *testing.T) {
	if got := roomShort("В4/ауд.3202(кк)"); got != "3202(кк)" {
		t.Errorf("получили %q", got)
	}
	if got := roomShort("ЛП49/2/313"); got != "313" {
		t.Errorf("получили %q", got)
	}
}

func TestNeizvestnoeOformlenieDayotUmolchanie(t *testing.T) {
	if got := SkinByID("hacker"); got.ID != defaultSkin {
		t.Errorf("неизвестное оформление должно давать %q, получили %q", defaultSkin, got.ID)
	}
	if got := ParseTheme("neon"); got != ThemeSystem {
		t.Errorf("неизвестная тема должна быть системной, получили %q", got)
	}
}

func TestVyborGruppyOtsevaetPotoki(t *testing.T) {
	for name, want := range map[string]bool{"ПИ24-1": true, "Ю24-5в": true, "006073_2 Иностранный язык (КАЯиПК)-10 СОЦ25-6_7": false, "ДПИ22-1; ДПИ22-2": false} {
		if got := groupNameRe.MatchString(name); got != want {
			t.Errorf("%q: %v, ожидалось %v", name, got, want)
		}
	}
}

func TestPersonazhStoitUTekushcheyOstanovki(t *testing.T) {
	s := &Server{}
	rs := rows("08:30", "10:00", "10:10", "11:40", "14:00", "15:30")
	markStatuses(rs, "2026-09-24", "2026-09-24", "10:30")
	h := s.buildHero(rs, true, "10:30")
	var here []int
	for _, p := range h.Route.Pins {
		if p.Here {
			here = append(here, p.Lesson.Index)
		}
	}
	if len(here) != 1 || here[0] != 2 {
		t.Errorf("персонаж должен стоять у второй пары, получили %v", here)
	}
}
