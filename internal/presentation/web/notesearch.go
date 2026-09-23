package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	ndom "github.com/zazabag/schedulefu/internal/modules/notes/domain"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// snippetPart — кусок отрывка: совпадение подсвечивается. Разметку
// собирает шаблон, текст экранируется как всегда.
type snippetPart struct {
	Text string
	Hit  bool
}

// splitSnippet режет отрывок по служебным границам подсветки.
func splitSnippet(s string) []snippetPart {
	var out []snippetPart
	for s != "" {
		i := strings.Index(s, ndom.HitStart)
		if i < 0 {
			out = append(out, snippetPart{Text: s})
			break
		}
		if i > 0 {
			out = append(out, snippetPart{Text: s[:i]})
		}
		s = s[i+len(ndom.HitStart):]
		j := strings.Index(s, ndom.HitEnd)
		if j < 0 {
			out = append(out, snippetPart{Text: s, Hit: true})
			break
		}
		out = append(out, snippetPart{Text: s[:j], Hit: true})
		s = s[j+len(ndom.HitEnd):]
	}
	return out
}

// notesSearch — поиск по своим конспектам: «что преподаватель говорил про
// дюрацию». Русская морфология — на стороне базы; только сохранённые и
// только с этого устройства.
func (s *Server) notesSearch(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Title": "Поиск по конспектам", "Tab": "lessons", "Query": q}
	if q == "" {
		s.render(w, r, "notesearch", data)
		return
	}
	data["Searched"] = true
	data["TooShort"] = len([]rune(q)) < 2
	hits, err := s.d.Notes.SearchNotes(r.Context(), httpx.ExistingOwnerKey(r), q, 20)
	if err != nil {
		http.Error(w, "не удалось выполнить поиск", http.StatusInternalServerError)
		return
	}
	type hitView struct {
		Discipline, When, Title string
		Parts                   []snippetPart
		Href                    template.URL
	}
	var views []hitView
	for _, h := range hits {
		n := h.Note
		base := "/lessons?d=" + url.QueryEscape(n.Lesson.Discipline)
		if subj, err := sched.ParseSubjectKey(n.Lesson.SubjectKey); err == nil && !subj.IsZero() {
			base = "/lessons?" + subj.Query() + "&d=" + url.QueryEscape(n.Lesson.Discipline)
		}
		views = append(views, hitView{Discipline: n.Lesson.Discipline, When: clock.DateRu(n.Lesson.Date), Title: n.Title,
			Parts: splitSnippet(h.Snippet),
			Href:  template.URL(string(dayHref(base, dayKeyOf(n.Lesson))) + "#note-" + strconv.FormatInt(n.ID, 10))})
	}
	data["Hits"] = views
	s.render(w, r, "notesearch", data)
}
