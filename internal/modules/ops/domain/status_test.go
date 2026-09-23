package domain

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// healthy — всё в порядке: от него отталкиваются проверки поломок.
func healthy() Status {
	return Status{
		At:       now,
		Host:     Host{Uptime: 50 * time.Hour, Load1: 0.1, MemTotalMB: 8000, MemAvailMB: 7000, DiskTotalGB: 79, DiskFreeGB: 70},
		Services: []Service{{Name: "serve", Active: true, State: "active"}, {Name: "notes", Active: true, State: "active"}},
		SiteOK:   true,
		Collector: Collector{Found: true, LastOK: now.Add(-30 * time.Minute), Last: now.Add(-30 * time.Minute),
			Lessons: 11000, Requests: 541},
		LLM: LLM{Model: "glm-4.5-flash", ProbeAt: now.Add(-time.Hour), ProbeOK: true, LastOKAt: now.Add(-time.Hour)},
	}
}

func keys(ps []Problem) string {
	var k []string
	for _, p := range ps {
		k = append(k, p.Key)
	}
	return strings.Join(k, ",")
}

func TestZdorovyySerwerBezTrevog(t *testing.T) {
	if ps := healthy().Problems(); len(ps) != 0 {
		t.Errorf("тревоги на здоровом сервере: %+v", ps)
	}
}

func TestTrevogiNaPolomki(t *testing.T) {
	cases := map[string]func(*Status){
		"service:notes": func(s *Status) { s.Services[1].Active, s.Services[1].State = false, "failed" },
		"site":          func(s *Status) { s.SiteOK, s.SiteErr = false, "connection refused" },
		// Ночью обход раз в три часа — четыре часа тишины уже поломка.
		"collector":   func(s *Status) { s.Collector.LastOK = now.Add(-5 * time.Hour) },
		"disk":        func(s *Status) { s.Host.DiskFreeGB = 5 },
		"memory":      func(s *Status) { s.Host.MemAvailMB = 200 },
		"notes:stuck": func(s *Status) { s.Notes.Stuck = 1 },
		"llm:probe":   func(s *Status) { s.LLM.ProbeOK, s.LLM.ProbeErr = false, "исчерпан дневной лимит" },
		// Настоящий вызов упал после последнего удачного — нейросеть не
		// работает прямо сейчас, даже если проба была утром.
		"llm:calls": func(s *Status) {
			s.LLM.LastErrAt, s.LLM.LastErr, s.LLM.LastErrCode = now.Add(-10*time.Minute), "баланс", "1113"
		},
	}
	for want, breakIt := range cases {
		s := healthy()
		breakIt(&s)
		if got := keys(s.Problems()); got != want {
			t.Errorf("%s: тревоги %q", want, got)
		}
	}
}

// Бот пишет о смене состояния, а не о состоянии: та же поломка каждые
// пять минут — это спам, после которого чат глушат вместе с настоящими
// тревогами.
func TestTrevogaTolkoPriSmeneSostoyaniya(t *testing.T) {
	active := map[string]string{"disk": "мало места", "site": "сайт не отвечает"}
	cur := []Problem{{Key: "disk", Text: "мало места"}, {Key: "collector", Text: "сбор стоит"}}
	raised, cleared := Diff(active, cur)
	if keys(raised) != "collector" || keys(cleared) != "site" {
		t.Errorf("новые %q, снятые %q", keys(raised), keys(cleared))
	}
	if cleared[0].Text != "сайт не отвечает" {
		t.Errorf("снятая тревога без текста: %+v", cleared[0])
	}
}

// Отчёт уходит в Telegram с разметкой HTML: текст ошибок чужой и может
// нести «<» — экранируется.
func TestOtchyotSoderzhitGlavnoeIEkraniruetsya(t *testing.T) {
	s := healthy()
	s.LLM.Tokens24h, s.LLM.Calls24h, s.LLM.Tokens7d = 48210, 3, 120500
	s.Notes.Ready24h, s.Notes.Failed24h, s.Notes.LastFailure = 2, 1, "ответ <500>"
	text := s.Format()
	for _, want := range []string{"glm-4.5-flash", "48 210", "120 500", "11 000", "ответ &lt;500&gt;", "serve", "70 из 79"} {
		if !strings.Contains(text, want) {
			t.Errorf("в отчёте нет %q:\n%s", want, text)
		}
	}
}
