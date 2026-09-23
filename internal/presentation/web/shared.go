package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"

	"github.com/zazabag/schedulefu/internal/modules/notes"
	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// ─── конспект по ссылке ──────────────────────────────────────────────────────

// sharedNote — конспект, которым поделились: открывается без ключа
// устройства, по одной ссылке, у кого угодно. Сохранить себе — одна кнопка.
func (s *Server) sharedNote(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	token := r.PathValue("token")
	n, hws, ok, err := s.d.Notes.Shared(r.Context(), token)
	if err != nil {
		http.Error(w, "не удалось открыть конспект", http.StatusInternalServerError)
		return
	}
	// Ссылки на конспекты не для поисковиков: их пересылают своим, а не
	// публикуют.
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	data := map[string]any{"Title": "Конспект", "Tab": "lessons"}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		data["Closed"] = true
		s.render(w, r, "shared", data)
		return
	}
	v := s.noteView(n, hws, "")
	data["Title"] = n.Lesson.Discipline
	data["Note"] = v
	data["Discipline"] = n.Lesson.Discipline
	data["When"] = clock.DateRu(n.Lesson.Date) + " · " + clock.WeekdayRu(n.Lesson.Date)
	data["Lecturer"] = n.Lesson.LecturerName
	data["Token"] = token
	// Уже сохранённый у себя (или свой же) конспект кнопку «Сохранить» не
	// показывает: ведёт туда, где он лежит.
	if owner := httpx.ExistingOwnerKey(r); owner != "" {
		if have, ok, err := s.d.Notes.SavedCopy(r.Context(), owner, token); err == nil && ok {
			data["SavedHref"] = template.URL(noteHref(have))
		}
	}
	s.render(w, r, "shared", data)
}

// saveSharedNote кладёт копию конспекта себе в «Пары» — к тому расписанию,
// что закреплено на этом устройстве, а без него — к расписанию автора.
func (s *Server) saveSharedNote(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	owner := httpx.OwnerKey(w, r, httpx.IsSecure(r))
	if owner == "" {
		http.Error(w, "не удалось завести ключ устройства", http.StatusInternalServerError)
		return
	}
	cp, err := s.d.Notes.SaveShared(r.Context(), owner, SubjectFromCookie(r).Key(), r.PathValue("token"))
	if errors.Is(err, notes.ErrShareClosed) {
		http.Redirect(w, r, "/n/"+url.PathEscape(r.PathValue("token")), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "не удалось сохранить конспект", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, noteHref(cp), http.StatusSeeOther)
}

// noteHref — адрес конспекта в разделе «Пары»: экран его пары.
func noteHref(n ndom.Note) string {
	base := "/lessons?d=" + url.QueryEscape(n.Lesson.Discipline)
	if subj, err := sched.ParseSubjectKey(n.Lesson.SubjectKey); err == nil && !subj.IsZero() {
		base = "/lessons?" + subj.Query() + "&d=" + url.QueryEscape(n.Lesson.Discipline)
	}
	return string(dayHref(base, dayKeyOf(n.Lesson))) + "#note-" + strconv.FormatInt(n.ID, 10)
}
