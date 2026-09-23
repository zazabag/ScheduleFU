package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	"github.com/zazabag/schedulefu/internal/modules/notes/domain"
)

func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func chatWithLog(url string, calls *[]domain.LLMCall) *Chat {
	return New(Options{BaseURL: url, APIKey: "k", Model: "glm-4.5-flash",
		Record: func(_ context.Context, c domain.LLMCall) { *calls = append(*calls, c) }})
}

const okAnswer = `{"choices":[{"message":{"role":"assistant","content":"{\"title\":\"т\",\"summary\":\"текст\",\"theses\":[],\"homework\":[]}"}}],
"usage":{"prompt_tokens":1200,"completion_tokens":300,"total_tokens":1500}}`

// Остатка лимита провайдер не сообщает — считаем расход сами, по каждому
// вызову: иначе «нейронка кончилась» узнаётся от студента с пустой парой.
func TestRashodTokenovZapisyvaetsya(t *testing.T) {
	var calls []domain.LLMCall
	c := chatWithLog(server(t, 200, okAnswer).URL, &calls)
	if _, err := c.Summarize(context.Background(), notes.SummaryInput{Discipline: "История", Transcript: "речь"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("вызовов: %d", len(calls))
	}
	got := calls[0]
	if !got.OK || got.Kind != "summary" || got.PromptTokens != 1200 || got.CompletionTokens != 300 || got.Model != "glm-4.5-flash" {
		t.Errorf("вызов: %+v", got)
	}
}

// Отказ провайдера — с его кодом: 1113 (баланс), 1302 (частота) и 1304
// (дневной лимит) требуют разного, и бот должен сказать какого.
func TestOtkazProvayderaSKodom(t *testing.T) {
	var calls []domain.LLMCall
	c := chatWithLog(server(t, 429, `{"error":{"code":"1304","message":"今日调用次数已达上限"}}`).URL, &calls)
	_, err := c.Summarize(context.Background(), notes.SummaryInput{Discipline: "История", Transcript: "речь"})
	if err == nil {
		t.Fatal("отказ не всплыл")
	}
	if len(calls) != 1 || calls[0].OK || calls[0].Code != "1304" {
		t.Fatalf("вызов: %+v", calls)
	}
	if !strings.Contains(err.Error(), "дневной лимит") {
		t.Errorf("ошибка без объяснения: %v", err)
	}
}

// Пробный запрос — крошечный и тоже учитывается, отдельным видом.
func TestProbnyyZapros(t *testing.T) {
	var calls []domain.LLMCall
	c := chatWithLog(server(t, 200, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`).URL, &calls)
	if _, err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Kind != "probe" || !calls[0].OK {
		t.Errorf("проба: %+v", calls)
	}
	bad := chatWithLog(server(t, 401, `{"error":{"code":"1002","message":"Authorization Token非法"}}`).URL, &calls)
	if _, err := bad.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Errorf("проба с плохим ключом: %v", err)
	}
}
