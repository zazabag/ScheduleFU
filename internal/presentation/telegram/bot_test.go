package telegram

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

var msk = time.FixedZone("MSK", 3*3600)

// repo — ПИ24-1 и ПИ24-10; у ПИ24-1 сегодня две пары до 13:20, завтра одна.
type repo struct{ schedule.Repository }

func (repo) SearchGroups(context.Context, string, int) ([]sched.Group, error) {
	return []sched.Group{{Name: "ПИ24-10"}, {Name: "ПИ24-1"}}, nil
}

func (repo) ScheduleFor(_ context.Context, s sched.Subject, from, _ time.Time) ([]sched.Lesson, error) {
	if s.Group != "ПИ24-1" {
		return nil, nil
	}
	mk := func(d time.Time, b, e, disc, aud string) sched.Lesson {
		return sched.Lesson{Date: d, BeginsAt: b, EndsAt: e, Discipline: disc, Auditorium: aud, Building: "ЛП51"}
	}
	tomorrow := from.AddDate(0, 0, 1)
	return []sched.Lesson{
		mk(from, "10:10", "11:40", "Английский", "ЛП51_1/0412"),
		mk(from, "10:10", "11:40", "Английский", "ЛП51_1/0413"), // вторая подгруппа — одна строка
		mk(from, "11:50", "13:20", "Финансы <и> кредит", "ЛП51_1/0326"),
		mk(tomorrow, "08:30", "10:00", "История", "ЛП49/2/313"),
	}, nil
}

func botAt(h, m int, base string) *Bot {
	clk := clock.Fixed(time.Date(2026, 9, 24, h, m, 0, 0, msk))
	return New("TOKEN", base, Deps{Schedule: schedule.New(nil, repo{}, clk, nil, schedule.Options{}), Clock: clk,
		BuildingLabel: func(string) string { return "Ленинградский" }, Origin: "https://fa.planovo.pro",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestInlineSegodnyaTochnoeSovpadeniePervym(t *testing.T) {
	res := botAt(9, 0, "").Inline(context.Background(), "пи24-1")
	if len(res) != 2 || res[0].Title != "ПИ24-1 — сегодня, 2 пары" {
		t.Fatalf("результаты: %+v", res)
	}
	text := res[0].Content.Text
	for _, want := range []string{"<b>ПИ24-1 · сегодня · Ленинградский</b>", "10:10–11:40 Английский · 0412, 0413",
		"Финансы &lt;и&gt; кредит", "https://fa.planovo.pro/g/%D0%9F%D0%9824-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("в тексте нет %q:\n%s", want, text)
		}
	}
	if res[1].Title != "ПИ24-10" || !strings.Contains(res[1].Content.Text, "пар нет") {
		t.Errorf("группа без пар: %+v", res[1])
	}
}

func TestVecheromZavtrashniyDen(t *testing.T) {
	res := botAt(18, 0, "").Inline(context.Background(), "ПИ24-1")
	if res[0].Title != "ПИ24-1 — завтра, 1 пара" {
		t.Errorf("после пар — завтрашний день: %q", res[0].Title)
	}
	if got := botAt(9, 0, "").Inline(context.Background(), "п"); got != nil {
		t.Error("одна буква — ничего не ищем")
	}
}

// Полный круг через поддельный Telegram: inline-запрос пришёл, ответ ушёл
// с текстом расписания; в личке — подсказка с именем бота.
func TestBotOtvechaetCherezBotAPI(t *testing.T) {
	var mu sync.Mutex
	calls := map[string][]map[string]any{}
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndexByte(r.URL.Path, '/')+1:]
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		calls[method] = append(calls[method], body)
		mu.Unlock()
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"finashka_bot"}}`))
		case "getUpdates":
			if served {
				time.Sleep(50 * time.Millisecond)
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			}
			served = true
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":1,"inline_query":{"id":"q1","query":"ПИ24-1"}},
				{"update_id":2,"message":{"chat":{"id":7,"type":"private"},"text":"/start"}},
				{"update_id":3,"message":{"chat":{"id":8,"type":"group"},"text":"привет"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_ = botAt(9, 0, srv.URL).Run(ctx)

	mu.Lock()
	defer mu.Unlock()
	ans := calls["answerInlineQuery"]
	if len(ans) != 1 || ans[0]["inline_query_id"] != "q1" {
		t.Fatalf("ответ на inline: %+v", ans)
	}
	sent := calls["sendMessage"]
	if len(sent) != 1 || sent[0]["chat_id"].(float64) != 7 || !strings.Contains(sent[0]["text"].(string), "@finashka_bot ПИ24-1") {
		t.Errorf("в личку — одна подсказка с именем бота, в группу — ничего: %+v", sent)
	}
	if off := calls["getUpdates"][1]["offset"].(float64); off != 4 {
		t.Errorf("следующий опрос с offset 4, а %v — иначе обновления придут снова", off)
	}
}
