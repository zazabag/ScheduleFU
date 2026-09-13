package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/zazabag/schedulefu/internal/store"
	"github.com/zazabag/schedulefu/internal/web"
)

// PushConfig — то, что нужно ручкам подписки.
type PushConfig struct {
	// PublicKey отдаётся браузеру: без него подписаться нельзя.
	PublicKey string
}

// subscribeRequest повторяет формат, в котором браузер отдаёт подписку.
type subscribeRequest struct {
	SubjectKey string `json:"subject_key"`
	Endpoint   string `json:"endpoint"`
	Keys       struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// handlePushKey отдаёт публичный ключ VAPID.
func (s *Server) handlePushKey(w http.ResponseWriter, r *http.Request) {
	if s.push.PublicKey == "" {
		// Уведомления не настроены — это не ошибка сервера, а состояние:
		// приложение должно спокойно спрятать кнопку подписки.
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true, "public_key": s.push.PublicKey,
	})
}

// handleSubscribe принимает подписку устройства.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	// Тело ограничено: подписка — это меньше килобайта, а принимать
	// произвольный объём от кого угодно незачем.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать подписку")
		return
	}

	subject, err := web.ParseSubjectKey(strings.TrimSpace(req.SubjectKey))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "не переданы ключи шифрования")
		return
	}
	if len(req.Keys.P256dh) > 256 || len(req.Keys.Auth) > 256 {
		writeError(w, http.StatusBadRequest, "ключи шифрования неправдоподобно длинные")
		return
	}

	if err := s.store.SaveSubscription(r.Context(), store.PushSubscription{
		SubjectKey: subject.Key(),
		Endpoint:   req.Endpoint,
		P256dh:     req.Keys.P256dh,
		Auth:       req.Keys.Auth,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось сохранить подписку")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": subject.Key()})
}

// handleUnsubscribe снимает подписку.
func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать запрос")
		return
	}
	if err := validEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Пустой ключ означает «отписать это устройство от всего».
	key := ""
	if strings.TrimSpace(req.SubjectKey) != "" {
		subject, err := web.ParseSubjectKey(req.SubjectKey)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		key = subject.Key()
	}

	if err := s.store.DeleteSubscription(r.Context(), req.Endpoint, key); err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось снять подписку")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unsubscribed": true})
}

// validEndpoint проверяет адрес доставки.
//
// Адрес приходит от браузера, но запрос к нам может отправить кто угодно.
// Мы потом сами пойдём по этому адресу, поэтому принимаем только https и
// отсеиваем явный мусор: иначе сервис превращается в средство слать запросы
// куда попало от своего имени.
func validEndpoint(endpoint string) error {
	if endpoint == "" {
		return errText("не передан адрес доставки")
	}
	if len(endpoint) > 1024 {
		return errText("адрес доставки неправдоподобно длинный")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return errText("адрес доставки не разобран")
	}
	if u.Scheme != "https" {
		return errText("адрес доставки должен быть https")
	}
	if u.Host == "" || strings.Contains(u.Host, "localhost") || strings.HasPrefix(u.Host, "127.") {
		return errText("недопустимый адрес доставки")
	}
	return nil
}

type errText string

func (e errText) Error() string { return string(e) }
