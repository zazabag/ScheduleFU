package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// Ручки записи пары.
//
// Их четыре, и все нужны из-за одного обстоятельства: пара идёт полтора
// часа, а вкладка на телефоне живёт как получится. Поэтому запись не
// отправляется целиком в конце, а едет кусками по ходу занятия: оборвалось
// — потеряна минута, а не лекция. Продолжение после обрыва — это чтение
// состояния и досылка с нужного номера.

type startRequest struct {
	Group      string `json:"group"`
	Lecturer   int64  `json:"lecturer"`
	Discipline string `json:"discipline"`
	// Lesson — «2026-09-22T10:10»: дата и начало выбранной пары. Остальные
	// поля пары сервер берёт из расписания сам, из формы им веры нет.
	Lesson string `json:"lesson"`
}

type recordingResponse struct {
	ID       int64  `json:"id"`
	Status   string `json:"status"`
	Label    string `json:"label"`
	Chunks   int    `json:"chunks"`
	Bytes    int64  `json:"bytes"`
	Failure  string `json:"failure,omitempty"`
	NoteID   int64  `json:"note_id,omitempty"`
	Duration string `json:"duration,omitempty"`
}

func (s *Server) notesStart(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil || !s.d.NotesReady {
		writeError(w, http.StatusServiceUnavailable, "обработка записей не настроена")
		return
	}
	var req startRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	subj := sched.GroupSubject(req.Group)
	if req.Lecturer > 0 {
		subj = sched.LecturerSubject(req.Lecturer)
	}
	if subj.IsZero() {
		writeError(w, http.StatusBadRequest, "не указано, чьё это расписание")
		return
	}
	owner := httpx.OwnerKey(w, r, httpx.IsSecure(r))
	if owner == "" {
		writeError(w, http.StatusInternalServerError, "не удалось завести ключ устройства")
		return
	}
	lesson, err := s.d.ResolveLesson(r.Context(), subj, req.Discipline, req.Lesson)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := s.d.Notes.Start(r.Context(), owner, lesson, ndom.OriginRecord)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recordingResponse{ID: rec.ID, Status: string(rec.Status), Label: rec.Status.Label()})
}

// notesChunk принимает очередной кусок записи.
//
// Тело — сырые байты, а не форма: кусок и так уже сжат кодеком, и
// оборачивать его в multipart значит гонять лишнее по мобильной сети.
func (s *Server) notesChunk(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		writeError(w, http.StatusServiceUnavailable, "раздел выключен")
		return
	}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		writeError(w, http.StatusForbidden, "нет ключа устройства")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "неверный номер записи")
		return
	}
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil || seq < 0 {
		writeError(w, http.StatusBadRequest, "неверный номер куска")
		return
	}
	rec, err := s.d.Notes.Append(r.Context(), owner, id, seq, r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recordingResponse{ID: rec.ID, Status: string(rec.Status),
		Label: rec.Status.Label(), Chunks: rec.Chunks, Bytes: rec.Bytes})
}

func (s *Server) notesFinish(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		writeError(w, http.StatusServiceUnavailable, "раздел выключен")
		return
	}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		writeError(w, http.StatusForbidden, "нет ключа устройства")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "неверный номер записи")
		return
	}
	rec, err := s.d.Notes.Finish(r.Context(), owner, id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recordingResponse{ID: rec.ID, Status: string(rec.Status),
		Label: rec.Status.Label(), Chunks: rec.Chunks, Bytes: rec.Bytes})
}

// notesState — состояние записи: по нему экран понимает, дошла ли обработка
// до конспекта, и с какого куска продолжать после обрыва.
func (s *Server) notesState(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		writeError(w, http.StatusServiceUnavailable, "раздел выключен")
		return
	}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		writeError(w, http.StatusForbidden, "нет ключа устройства")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "неверный номер записи")
		return
	}
	rec, ok, err := s.d.Notes.Recording(r.Context(), owner, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "не удалось прочитать запись")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "запись не найдена")
		return
	}
	out := recordingResponse{ID: rec.ID, Status: string(rec.Status), Label: rec.Status.Label(),
		Chunks: rec.Chunks, Bytes: rec.Bytes, Failure: rec.Failure, Duration: rec.Duration()}
	if note, ok, err := s.d.Notes.NoteByRecording(r.Context(), owner, rec.ID); err == nil && ok {
		out.NoteID = note.ID
	}
	writeJSON(w, http.StatusOK, out)
}

// readJSON читает тело запроса с потолком: ручку вызывает наш же скрипт,
// но открыта она всем.
func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(v)
}
