package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Замер загрузки страниц с устройства.
//
// Жалоба «в приложении всё открывается по несколько секунд» с сервера не
// видна: сам сервер отдаёт страницу за 40 мс. Где уходит время — сеть,
// шрифты с чужого сервера, отрисовка, перенаправление — знает только
// браузер. Он и присылает свои тайминги (Navigation Timing), а мы пишем их
// в лог. Не хранятся: для разбора жалобы хватает журнала.
//
// Приходит только то, что ниже: адрес страницы и миллисекунды. Ни
// идентификаторов, ни адресов человека — «никаких досье» (§ 5 канона).

type timingReport struct {
	Path       string `json:"path"`
	Nav        string `json:"nav"` // navigate | reload | back_forward
	Standalone bool   `json:"standalone"`
	// Миллисекунды от начала перехода.
	Redirect int `json:"redirect"`
	DNS      int `json:"dns"`
	Connect  int `json:"connect"`
	TTFB     int `json:"ttfb"`
	HTML     int `json:"html"`
	FCP      int `json:"fcp"`
	DOM      int `json:"dom"`
	Load     int `json:"load"`
	Slow     []struct {
		Name string `json:"n"`
		Ms   int    `json:"d"`
	} `json:"slow"`
}

func (s *Server) timing(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<10+1))
	if err != nil || len(raw) > 8<<10 {
		http.Error(w, "слишком длинно", http.StatusRequestEntityTooLarge)
		return
	}
	var t timingReport
	if err := json.Unmarshal(raw, &t); err != nil || !strings.HasPrefix(t.Path, "/") {
		http.Error(w, "не разобрано", http.StatusBadRequest)
		return
	}
	var slow []string
	for i, res := range t.Slow {
		if i == 8 {
			break
		}
		slow = append(slow, clip(res.Name, 80)+"="+itoa(res.Ms))
	}
	slog.Info("замер страницы", "путь", clip(t.Path, 100), "переход", clip(t.Nav, 16), "приложение", t.Standalone,
		"перенаправление", t.Redirect, "dns", t.DNS, "соединение", t.Connect, "ttfb", t.TTFB, "html", t.HTML,
		"первая_отрисовка", t.FCP, "dom", t.DOM, "загрузка", t.Load, "медленное", strings.Join(slow, " "))
	w.WriteHeader(http.StatusNoContent)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
