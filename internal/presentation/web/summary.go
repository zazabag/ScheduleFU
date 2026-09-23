package web

import (
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
	"github.com/zazabag/schedulefu/internal/platform/httpx"
)

// lessonsSummary — все сохранённые конспекты предмета одной страницей, от
// первой пары к последней, с оглавлением и заданиями в конце. Перед
// сессией конспект нужен целиком, а не по одной паре; страница же
// печатается как есть. Модель не нужна: это склейка того, что уже
// сохранено.
func (s *Server) lessonsSummary(w http.ResponseWriter, r *http.Request) {
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
	data := map[string]any{"Title": discipline + " — сводка", "Tab": "lessons", "Discipline": discipline,
		"Back": template.URL(base)}
	owner := httpx.ExistingOwnerKey(r)
	if owner == "" {
		s.render(w, r, "summary", data)
		return
	}
	ctx := r.Context()
	notesList, err := s.d.Notes.Notes(ctx, owner, subj.Key(), discipline)
	if err != nil {
		http.Error(w, "не удалось собрать конспекты", http.StatusInternalServerError)
		return
	}
	// Хранилище отдаёт от новых к старым — для ленты; сводку читают по
	// порядку курса.
	sort.SliceStable(notesList, func(i, j int) bool {
		a, b := notesList[i].Lesson, notesList[j].Lesson
		if !a.Date.Equal(b.Date) {
			return a.Date.Before(b.Date)
		}
		return a.BeginsAt < b.BeginsAt
	})
	type entry struct {
		noteView
		Anchor, When string
	}
	var entries []entry
	for i, n := range notesList {
		v := s.noteView(n, nil, base)
		entries = append(entries, entry{noteView: v, Anchor: "p" + strconv.Itoa(i+1),
			When: clock.DateRu(n.Lesson.Date) + ", " + clock.WeekdayRu(n.Lesson.Date)})
	}
	hws, err := s.d.Notes.Homeworks(ctx, owner, subj.Key(), discipline, true)
	if err != nil {
		http.Error(w, "не удалось собрать задания", http.StatusInternalServerError)
		return
	}
	var hwViews []homeworkView
	for _, h := range hws {
		hwViews = append(hwViews, homeworkViewOf(h))
	}
	data["Entries"], data["Homeworks"] = entries, hwViews
	if len(notesList) > 0 {
		first, last := notesList[0].Lesson.Date, notesList[len(notesList)-1].Lesson.Date
		data["Range"] = clock.DateRu(first) + " — " + clock.DateRu(last)
	}
	s.render(w, r, "summary", data)
}
