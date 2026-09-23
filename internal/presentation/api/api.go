// Package api — JSON /api/v1.
//
// Стадии операций — по канону § 7: всё здесь пока internal, форма меняется
// свободно. В preview первыми уйдут расписание владельца и календарь — то,
// что нужно Akeda для стыковки.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	"github.com/zazabag/schedulefu/internal/modules/notify"
	ndomain "github.com/zazabag/schedulefu/internal/modules/notify/domain"
	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

// Deps — что нужно API.
type Deps struct {
	Schedule *schedule.Service
	Notify   *notify.Service
	Notes    *notes.Service
	Clock    *clock.Clock
	PushKey  string // публичный VAPID; пусто — уведомления выключены
	StandEnv string
	Calendar http.HandlerFunc
	// NotesReady — настроена ли обработка записей.
	NotesReady bool
	// HideWhereLecturer снимает ручку «где преподаватель сейчас» целиком:
	// выключенная функция не должна отвечать даже пустым ответом.
	HideWhereLecturer bool
	// ResolveLesson собирает слепок пары по предмету и выбранному времени.
	// Приходит снаружи, потому что живёт в web, а транспорт транспорт не
	// импортирует; связывает их composition root.
	ResolveLesson func(ctx context.Context, subj sched.Subject, discipline, choice string) (ndom.LessonRef, error)
}

type Server struct{ d Deps }

func New(d Deps) *Server { return &Server{d: d} }

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("GET /api/v1/free", s.free)
	mux.HandleFunc("GET /api/v1/schedule", s.schedule)
	if !s.d.HideWhereLecturer {
		mux.HandleFunc("GET /api/v1/lecturer/{oid}/where", s.where)
	}
	mux.HandleFunc("GET /api/v1/changes", s.changes)
	mux.HandleFunc("GET /api/v1/push/key", s.pushKey)
	mux.HandleFunc("POST /api/v1/push/subscribe", s.subscribe)
	mux.HandleFunc("POST /api/v1/push/unsubscribe", s.unsubscribe)
	if s.d.Notes != nil {
		mux.HandleFunc("POST /api/v1/notes/recordings", s.notesStart)
		mux.HandleFunc("GET /api/v1/notes/recordings/{id}", s.notesState)
		mux.HandleFunc("PUT /api/v1/notes/recordings/{id}/chunks/{seq}", s.notesChunk)
		mux.HandleFunc("POST /api/v1/notes/recordings/{id}/finish", s.notesFinish)
	}
	if s.d.Calendar != nil {
		mux.HandleFunc("GET /api/v1/calendar.ics", s.d.Calendar)
	}
	return mux
}

type freshness struct {
	UpdatedAt *time.Time `json:"updated_at"`
	StaleFor  string     `json:"stale_for,omitempty"`
}

func (s *Server) freshness(r *http.Request) freshness {
	at, ok := s.d.Schedule.Freshness(r.Context())
	if !ok {
		return freshness{}
	}
	return freshness{UpdatedAt: &at, StaleFor: time.Since(at).Round(time.Minute).String()}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"status": "ok", "env": s.d.StandEnv}
	if at, ok := s.d.Schedule.Freshness(r.Context()); ok {
		resp["last_collected_at"] = at
		// Сборщик молчит дольше суток — данные протухли: расписание правят
		// задним числом, вчерашний слепок обманет.
		if time.Since(at) > 24*time.Hour {
			resp["status"] = "stale"
		}
	} else {
		resp["status"] = "empty"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) free(w http.ResponseWriter, r *http.Request) {
	at, err := s.moment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	site := r.URL.Query().Get("site")
	free, total, err := s.d.Schedule.FreeRooms(r.Context(), site, at)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить список")
		return
	}
	type room struct {
		Oid       int64            `json:"oid"`
		Room      string           `json:"room"`
		Building  string           `json:"building"`
		Site      string           `json:"site"`
		Kind      string           `json:"kind,omitempty"`
		Floor     *int             `json:"floor"`
		Capacity  *int             `json:"capacity"`
		FreeUntil string           `json:"free_until,omitempty"`
		Cells     []sched.SlotCell `json:"cells"`
	}
	out := make([]room, 0, len(free))
	for _, v := range free {
		a := v.Auditorium
		out = append(out, room{Oid: a.Oid, Room: a.Room, Building: a.Building, Site: a.Site.Slug, Kind: a.Kind,
			Floor: a.Floor, Capacity: a.Capacity, FreeUntil: v.FreeUntil, Cells: v.Cells})
	}
	writeJSON(w, http.StatusOK, map[string]any{"at": at, "site": site, "count": len(out), "total": total,
		"data": out, "freshness": s.freshness(r),
		// Предупреждение едет с данными, а не только в вёрстке.
		"disclaimer": "Данные из расписания вуза. Брони вне расписания (мероприятия, экзамены) в нём не отражаются."})
}

func (s *Server) schedule(w http.ResponseWriter, r *http.Request) {
	subj, err := sched.SubjectFromValues(r.URL.Query())
	if err != nil || subj.IsZero() {
		writeError(w, http.StatusBadRequest, "укажите group или lecturer")
		return
	}
	from, to, err := s.period(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lessons, err := s.d.Schedule.ScheduleFor(r.Context(), subj, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить расписание")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subject": subj.Key(), "from": from, "to": to,
		"count": len(lessons), "data": lessons, "freshness": s.freshness(r)})
}

// where — «где преподаватель сейчас». Отдельная ручка: её можно отключить
// одной строкой, не трогая остальное (канон, ограничения).
func (s *Server) where(w http.ResponseWriter, r *http.Request) {
	oid, err := strconv.ParseInt(r.PathValue("oid"), 10, 64)
	if err != nil || oid <= 0 {
		writeError(w, http.StatusBadRequest, "oid должен быть положительным числом")
		return
	}
	at, err := s.moment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, _, err := s.d.Schedule.WhereIsLecturer(r.Context(), oid, at)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выполнить поиск")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lecturer_oid": oid, "at": at, "in_class": current != nil, "data": current, "freshness": s.freshness(r)})
}

func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	since := s.d.Clock.Now().Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		p, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "since должен быть в формате RFC3339")
			return
		}
		since = p
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			writeError(w, http.StatusBadRequest, "limit — число от 1 до 500")
			return
		}
		limit = n
	}
	out, err := s.d.Schedule.Repo().ChangesSince(r.Context(), since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить изменения")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"since": since, "count": len(out), "data": out})
}

// ─── подписки ────────────────────────────────────────────────────────────────

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	if s.d.PushKey == "" || s.d.Notify == nil {
		// Не ошибка, а состояние: приложение спокойно прячет кнопку.
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "public_key": s.d.PushKey})
}

type subscribeRequest struct {
	SubjectKey string `json:"subject_key"`
	Endpoint   string `json:"endpoint"`
	Keys       struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *Server) subscribe(w http.ResponseWriter, r *http.Request) {
	if s.d.Notify == nil {
		writeError(w, http.StatusServiceUnavailable, "уведомления не настроены")
		return
	}
	var req subscribeRequest
	// Подписка — меньше килобайта; принимать больше от кого угодно незачем.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать подписку")
		return
	}
	if err := validEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Keys.P256dh == "" || req.Keys.Auth == "" || len(req.Keys.P256dh) > 256 || len(req.Keys.Auth) > 256 {
		writeError(w, http.StatusBadRequest, "ключи шифрования отсутствуют или неправдоподобны")
		return
	}
	err := s.d.Notify.Subscribe(r.Context(), ndomain.Subscription{
		SubjectKey: strings.TrimSpace(req.SubjectKey), Transport: "webpush", Target: req.Endpoint,
		Credentials: map[string]string{"p256dh": req.Keys.P256dh, "auth": req.Keys.Auth}})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscribed": req.SubjectKey})
}

func (s *Server) unsubscribe(w http.ResponseWriter, r *http.Request) {
	if s.d.Notify == nil {
		writeError(w, http.StatusServiceUnavailable, "уведомления не настроены")
		return
	}
	var req subscribeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "не удалось разобрать запрос")
		return
	}
	if err := validEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.d.Notify.Unsubscribe(r.Context(), "webpush", req.Endpoint, strings.TrimSpace(req.SubjectKey)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unsubscribed": true})
}

// validEndpoint: по этому адресу мы потом пойдём сами. Только https и без
// localhost — иначе сервис становится средством слать запросы куда попало
// от своего имени.
func validEndpoint(endpoint string) error {
	if endpoint == "" || len(endpoint) > 1024 {
		return fmt.Errorf("адрес доставки отсутствует или неправдоподобно длинный")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || strings.Contains(u.Host, "localhost") || strings.HasPrefix(u.Host, "127.") {
		return fmt.Errorf("адрес доставки должен быть внешним https")
	}
	return nil
}

// ─── разбор параметров ───────────────────────────────────────────────────────

func (s *Server) moment(r *http.Request) (time.Time, error) {
	v := r.URL.Query().Get("at")
	if v == "" {
		return s.d.Clock.Now(), nil
	}
	t, err := time.ParseInLocation(time.RFC3339, v, s.d.Clock.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("at должен быть в формате RFC3339")
	}
	return t.In(s.d.Clock.Location()), nil
}

// period — неделя вперёд по умолчанию; потолок месяц: отдавать наружу
// произвольно длинные куски расписания вуза мы не хотим.
func (s *Server) period(r *http.Request) (time.Time, time.Time, error) {
	from := s.d.Clock.Today()
	if v := r.URL.Query().Get("from"); v != "" {
		p, err := time.ParseInLocation("2006-01-02", v, s.d.Clock.Location())
		if err != nil {
			return from, from, fmt.Errorf("from должен быть ГГГГ-ММ-ДД")
		}
		from = p
	}
	to := from.AddDate(0, 0, 6)
	if v := r.URL.Query().Get("to"); v != "" {
		p, err := time.ParseInLocation("2006-01-02", v, s.d.Clock.Location())
		if err != nil {
			return from, from, fmt.Errorf("to должен быть ГГГГ-ММ-ДД")
		}
		to = p
	}
	if to.Before(from) || to.Sub(from) > 31*24*time.Hour {
		return from, from, fmt.Errorf("период от 0 до 31 дня")
	}
	return from, to, nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
