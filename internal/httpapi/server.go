// Package httpapi — HTTP-интерфейс поверх хранилища.
//
// Слой намеренно тонкий: вся логика живёт в store и ruz, здесь только
// разбор параметров, формат ответа и заголовки кэширования.
package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

// Server — обработчики API.
type Server struct {
	store *store.Store
	push  PushConfig
	// Location — часовой пояс вуза: «сейчас» считается по нему, а не по
	// поясу сервера.
	Location *time.Location
}

// New создаёт сервер.
func New(s *store.Store, loc *time.Location) *Server {
	if loc == nil {
		loc = time.UTC
	}
	return &Server{store: s, Location: loc}
}

// WithPush включает ручки подписки на уведомления.
func (s *Server) WithPush(cfg PushConfig) *Server {
	s.push = cfg
	return s
}

// Routes собирает маршруты.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/free", s.handleFree)
	mux.HandleFunc("GET /api/v1/schedule/group/{name}", s.handleGroupSchedule)
	mux.HandleFunc("GET /api/v1/schedule/lecturer/{oid}", s.handleLecturerSchedule)
	mux.HandleFunc("GET /api/v1/lecturer/{oid}/where", s.handleWhereIsLecturer)
	mux.HandleFunc("GET /api/v1/auditorium/{oid}/occupancy", s.handleOccupancy)
	mux.HandleFunc("GET /api/v1/changes", s.handleChanges)
	mux.HandleFunc("GET /api/v1/push/key", s.handlePushKey)
	mux.HandleFunc("POST /api/v1/push/subscribe", s.handleSubscribe)
	mux.HandleFunc("POST /api/v1/push/unsubscribe", s.handleUnsubscribe)
	return mux
}

// freshness описывает, насколько свежи данные.
//
// Показывать это обязательно: расписание мы зеркалим, а не ведём, и
// пользователь должен видеть, когда мы последний раз сверялись с вузом.
type freshness struct {
	UpdatedAt *time.Time `json:"updated_at"`
	StaleFor  string     `json:"stale_for,omitempty"`
}

func (s *Server) freshness(r *http.Request) freshness {
	at, ok, err := s.store.LastSuccessfulRun(r.Context())
	if err != nil || !ok {
		return freshness{}
	}
	return freshness{
		UpdatedAt: &at,
		StaleFor:  time.Since(at).Round(time.Minute).String(),
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	at, ok, err := s.store.LastSuccessfulRun(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "база недоступна")
		return
	}
	resp := map[string]any{"status": "ok"}
	if ok {
		resp["last_collected_at"] = at
		// Если сборщик молчит дольше суток, данные считаем протухшими:
		// расписание правят задним числом, и вчерашний слепок обманет.
		if time.Since(at) > 24*time.Hour {
			resp["status"] = "stale"
		}
	} else {
		resp["status"] = "empty"
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleFree — свободные аудитории.
func (s *Server) handleFree(w http.ResponseWriter, r *http.Request) {
	at, err := s.parseMoment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	campus := r.URL.Query().Get("campus")

	free, err := s.store.FreeAuditoriums(r.Context(), at, campus)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить список")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"at":        at,
		"campus":    campus,
		"count":     len(free),
		"data":      free,
		"freshness": s.freshness(r),
		// Предупреждение едет вместе с данными, а не только в вёрстке:
		// бронирования вне расписания (мероприятия, экзамены, ремонт) в
		// источник не попадают, и аудитория может оказаться занятой.
		"disclaimer": "Данные из расписания вуза. Брони вне расписания (мероприятия, экзамены) в нём не отражаются.",
	})
}

func (s *Server) handleGroupSchedule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "не указана группа")
		return
	}
	from, to, err := s.parsePeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lessons, err := s.store.ScheduleForGroup(r.Context(), name, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить расписание")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"group": name, "from": from, "to": to,
		"count": len(lessons), "data": lessons,
		"freshness": s.freshness(r),
	})
}

func (s *Server) handleLecturerSchedule(w http.ResponseWriter, r *http.Request) {
	oid, err := pathInt(r, "oid")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.parsePeriod(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lessons, err := s.store.ScheduleForLecturer(r.Context(), oid, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить расписание")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lecturer_oid": oid, "from": from, "to": to,
		"count": len(lessons), "data": lessons,
		"freshness": s.freshness(r),
	})
}

// handleWhereIsLecturer отвечает, где преподаватель сейчас.
//
// Данные публичные — то же самое видно на сайте вуза, — но ответ про
// местонахождение человека здесь и сейчас чувствительнее обычного
// расписания, поэтому ручка вынесена отдельно: её можно отключить одной
// строкой, не трогая остальное.
func (s *Server) handleWhereIsLecturer(w http.ResponseWriter, r *http.Request) {
	oid, err := pathInt(r, "oid")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	at, err := s.parseMoment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lesson, err := s.store.WhereIsLecturer(r.Context(), oid, at)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось выполнить поиск")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lecturer_oid": oid, "at": at,
		"in_class":  lesson != nil,
		"data":      lesson,
		"freshness": s.freshness(r),
	})
}

func (s *Server) handleOccupancy(w http.ResponseWriter, r *http.Request) {
	oid, err := pathInt(r, "oid")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	date, err := s.parseDate(r, "date")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lessons, err := s.store.OccupancyForAuditorium(r.Context(), oid, date)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить занятость")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"auditorium_oid": oid, "date": date.Format("2006-01-02"),
		"count": len(lessons), "data": lessons,
		"freshness": s.freshness(r),
	})
}

// handleChanges — журнал изменений расписания.
func (s *Server) handleChanges(w http.ResponseWriter, r *http.Request) {
	since := time.Now().In(s.Location).Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "параметр since должен быть в формате RFC3339")
			return
		}
		since = parsed
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 500 {
			writeError(w, http.StatusBadRequest, "параметр limit должен быть числом от 1 до 500")
			return
		}
		limit = n
	}
	changes, err := s.store.ChangesSince(r.Context(), since, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось получить изменения")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"since": since, "count": len(changes), "data": changes,
	})
}

// parseMoment разбирает момент времени: параметр at или «сейчас».
func (s *Server) parseMoment(r *http.Request) (time.Time, error) {
	v := r.URL.Query().Get("at")
	if v == "" {
		return time.Now().In(s.Location), nil
	}
	t, err := time.ParseInLocation(time.RFC3339, v, s.Location)
	if err != nil {
		return time.Time{}, fmt.Errorf("параметр at должен быть в формате RFC3339")
	}
	return t.In(s.Location), nil
}

func (s *Server) parseDate(r *http.Request, key string) (time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		now := time.Now().In(s.Location)
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.Location), nil
	}
	t, err := time.ParseInLocation("2006-01-02", v, s.Location)
	if err != nil {
		return time.Time{}, fmt.Errorf("параметр %s должен быть в формате ГГГГ-ММ-ДД", key)
	}
	return t, nil
}

// parsePeriod разбирает период. По умолчанию — неделя вперёд.
//
// Период ограничен месяцем: отдавать наружу произвольно длинные куски
// расписания вуза мы не хотим, см. docs/03-legal-risks.md.
func (s *Server) parsePeriod(r *http.Request) (time.Time, time.Time, error) {
	from, err := s.parseDate(r, "from")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to := from.AddDate(0, 0, 6)
	if v := r.URL.Query().Get("to"); v != "" {
		to, err = time.ParseInLocation("2006-01-02", v, s.Location)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("параметр to должен быть в формате ГГГГ-ММ-ДД")
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("конец периода раньше начала")
	}
	if to.Sub(from) > 31*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("период не может быть длиннее 31 дня")
	}
	return from, to, nil
}

func pathInt(r *http.Request, key string) (int64, error) {
	v := r.PathValue(key)
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("параметр %s должен быть положительным числом", key)
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}
