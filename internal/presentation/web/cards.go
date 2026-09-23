package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// lessonsCards — повторение и словарь предмета. Одна карточка за раз:
// вопрос, ответ по касанию, «помню» или «не помню» — и следующая.
// Работает без скрипта: ответ прячется в <details>, оценка — обычная форма.
func (s *Server) lessonsCards(w http.ResponseWriter, r *http.Request) {
	if s.d.Notes == nil {
		http.NotFound(w, r)
		return
	}
	subj := SubjectFromCookie(r)
	if q, err := sched.SubjectFromValues(r.URL.Query()); err == nil && !q.IsZero() {
		subj = q
	}
	discipline := strings.TrimSpace(r.URL.Query().Get("d"))
	if subj.IsZero() || discipline == "" {
		http.Redirect(w, r, "/lessons", http.StatusSeeOther)
		return
	}
	base := "/lessons?" + subj.Query() + "&d=" + url.QueryEscape(discipline)
	here := "/lessons/cards?" + subj.Query() + "&d=" + url.QueryEscape(discipline)
	data := map[string]any{"Title": discipline + " — карточки", "Tab": "lessons", "Discipline": discipline,
		"Back": template.URL(base), "Here": here}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		s.render(w, r, "cards", data)
		return
	}
	ctx := r.Context()
	deck, err := s.d.Notes.Deck(ctx, owner, subj.Key(), discipline)
	if err != nil {
		http.Error(w, "не удалось открыть карточки", http.StatusInternalServerError)
		return
	}
	terms, err := s.d.Notes.Terms(ctx, owner, subj.Key(), discipline)
	if err != nil {
		http.Error(w, "не удалось открыть словарь", http.StatusInternalServerError)
		return
	}
	data["Total"], data["Left"], data["Terms"] = deck.Total, deck.Left, terms
	if len(deck.Due) > 0 {
		c := deck.Due[0]
		data["Card"] = map[string]any{"ID": strconv.FormatInt(c.ID, 10), "Front": c.Front, "Back": c.Back,
			"From": clock.DateRu(c.Lesson.Date), "Box": c.Box}
	}
	if deck.Next != nil {
		data["Next"] = clock.DateRu(*deck.Next)
	}
	s.render(w, r, "cards", data)
}
