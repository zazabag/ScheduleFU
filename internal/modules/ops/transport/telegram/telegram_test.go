package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOtpravkaSRazmetkoy(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("путь %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()
	b := New("TOKEN", srv.URL)
	if err := b.Send(context.Background(), -100, "<b>ок</b>"); err != nil {
		t.Fatal(err)
	}
	if got["chat_id"].(float64) != -100 || got["parse_mode"] != "HTML" || got["text"] != "<b>ок</b>" {
		t.Errorf("запрос: %+v", got)
	}
}

// Токен — единственный секрет бота. Ошибка сети несёт адрес запроса, а
// значит и токен, и попадает в лог — там его быть не должно.
func TestTokenNeUtekaetVOshibku(t *testing.T) {
	b := New("SECRET123", "http://127.0.0.1:1") // порт закрыт
	err := b.Send(context.Background(), 1, "x")
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Errorf("ошибка: %v", err)
	}
	if strings.Contains(b.String(), "SECRET123") {
		t.Error("токен в строковом виде клиента")
	}
}

func TestOprosOtdayotKomandy(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ok":true,"result":[
				{"update_id":7,"message":{"text":"/status@FAsupportingbot","chat":{"id":-100}}},
				{"update_id":8,"my_chat_member":{}}]}`))
			return
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		if body["offset"].(float64) != 9 {
			t.Errorf("смещение после первой пачки: %v", body["offset"])
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()
	b := New("T", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := <-b.Updates(ctx)
	if cmd.ChatID != -100 || cmd.Text != "/status@FAsupportingbot" {
		t.Errorf("команда: %+v", cmd)
	}
	for calls.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
	}
}
