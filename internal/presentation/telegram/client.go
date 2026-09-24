// Package telegram — расписание в Telegram: inline-режим бота и /start.
//
// Это ещё один способ показать расписание, как сайт и /api/v1, поэтому
// живёт в presentation. Главное — inline: «@бот ПИ24-1» в любом чате
// группы вставляет расписание прямо в переписку. Личный бот вуза в
// групповые чаты не зовут, а этот и звать не надо.
//
// Входящие — длинным опросом, как у присмотра: порт наружу открывать
// незачем. Клиент Bot API свой, на стандартной библиотеке: у присмотра —
// тоже свой, общим он станет, когда понадобится третьему.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// client — минимальный клиент Bot API: три метода.
type client struct {
	token string
	base  string
	http  *http.Client
	poll  time.Duration
}

func newClient(token, base string) *client {
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &client{token: token, base: strings.TrimRight(base, "/"), poll: 50 * time.Second,
		// Таймаут больше опроса: иначе каждый пустой опрос кончался бы ошибкой.
		http: &http.Client{Timeout: 70 * time.Second}}
}

func (c *client) call(ctx context.Context, method string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		// Ошибка сети несёт адрес с токеном — наружу его не выпускаем.
		return fmt.Errorf("telegram %s: %s", method, strings.ReplaceAll(err.Error(), c.token, "***"))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: чтение ответа: %w", method, err)
	}
	var r struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("telegram %s: ответ %d не разобран", method, resp.StatusCode)
	}
	if !r.OK {
		return fmt.Errorf("telegram %s: %s", method, r.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// update — входящее: inline-запрос или сообщение.
type update struct {
	ID          int64 `json:"update_id"`
	InlineQuery *struct {
		ID    string `json:"id"`
		Query string `json:"query"`
	} `json:"inline_query"`
	Message *struct {
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		Text string `json:"text"`
	} `json:"message"`
}

func (c *client) updates(ctx context.Context, offset int64) ([]update, error) {
	var out []update
	err := c.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": int(c.poll.Seconds()),
		"allowed_updates": []string{"inline_query", "message"},
	}, &out)
	return out, err
}

// article — ответ inline-режима: карточка в списке, по касанию в чат уходит
// текст.
type article struct {
	Type        string         `json:"type"`
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Content     messageContent `json:"input_message_content"`
}

type messageContent struct {
	Text      string `json:"message_text"`
	ParseMode string `json:"parse_mode"`
	// Предпросмотр ссылки на неделю занял бы полэкрана чата.
	NoPreview bool `json:"disable_web_page_preview"`
}

func (c *client) answerInline(ctx context.Context, queryID string, results []article) error {
	if results == nil {
		results = []article{}
	}
	// Кэш на пять минут: расписание меняется реже, а повторный набор той
	// же группы в том же чате не должен каждый раз ходить к нам.
	return c.call(ctx, "answerInlineQuery", map[string]any{
		"inline_query_id": queryID, "results": results, "cache_time": 300, "is_personal": false,
	}, nil)
}

func (c *client) me(ctx context.Context) (string, error) {
	var u struct {
		Username string `json:"username"`
	}
	err := c.call(ctx, "getMe", map[string]any{}, &u)
	return u.Username, err
}

func (c *client) send(ctx context.Context, chatID int64, text string) error {
	return c.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true,
	}, nil)
}
