// Package telegram — служебный чат в Telegram: реализация ops.Messenger на
// Bot API без библиотек.
//
// Входящие — длинным опросом (getUpdates), а не вебхуком: сервер один,
// порт наружу для бота открывать незачем, и опрос не зависит от того, жив
// ли сайт, — присмотр как раз должен работать, когда сайт лежит.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/ops"
)

// Bot — клиент Bot API.
type Bot struct {
	token string
	base  string
	http  *http.Client
	// poll — сколько сервер Telegram держит опрос открытым.
	poll time.Duration
}

// New собирает клиента. base пустой — api.telegram.org; другой — для тестов.
func New(token, base string) *Bot {
	if base == "" {
		base = "https://api.telegram.org"
	}
	return &Bot{token: token, base: strings.TrimRight(base, "/"), poll: 50 * time.Second,
		// Таймаут больше опроса: иначе каждый пустой опрос кончался бы ошибкой.
		http: &http.Client{Timeout: 70 * time.Second}}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func (b *Bot) call(ctx context.Context, method string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+"/bot"+b.token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		// Ошибка сети несёт адрес с токеном — наружу его не выпускаем.
		return fmt.Errorf("telegram %s: %s", method, strings.ReplaceAll(err.Error(), b.token, "***"))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: чтение ответа: %w", method, err)
	}
	var r apiResponse
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

// Send отправляет сообщение с разметкой HTML. Длинное режется: у Telegram
// предел 4096 символов, отчёт в него помещается, а текст чужой ошибки —
// не обязательно.
func (b *Bot) Send(ctx context.Context, chatID int64, text string) error {
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000]) + "…"
	}
	return b.call(ctx, "sendMessage", map[string]any{
		"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true,
	}, nil)
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
	} `json:"message"`
}

// Updates опрашивает Telegram и отдаёт команды, пока жив контекст. Сбой
// опроса не роняет присмотр: пауза и снова.
func (b *Bot) Updates(ctx context.Context) <-chan ops.Command {
	out := make(chan ops.Command)
	go func() {
		defer close(out)
		var offset int64
		for ctx.Err() == nil {
			var ups []update
			err := b.call(ctx, "getUpdates", map[string]any{
				"offset": offset, "timeout": int(b.poll.Seconds()), "allowed_updates": []string{"message"},
			}, &ups)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
			for _, u := range ups {
				offset = u.UpdateID + 1
				if u.Message == nil || u.Message.Text == "" {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- ops.Command{ChatID: u.Message.Chat.ID, Text: u.Message.Text}:
				}
			}
		}
	}()
	return out
}

// Компилятор держит соответствие порту.
var _ ops.Messenger = (*Bot)(nil)

// String не выдаёт токен, если клиента вдруг напечатают в лог.
func (b *Bot) String() string {
	return "telegram-бот " + strconv.Itoa(len(b.token)) + " символов токена"
}
