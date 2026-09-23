package web

import (
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// maxGroupsShown — сколько групп показывать у дисциплины сразу. У потоковых
// лекций их бывает под сотню, и список съел бы экран.
const maxGroupsShown = 12

// disciplines — поиск по дисциплине: кто ведёт, каким группам, где.
// Помогает с выбором дисциплины и отвечает на «кто у нас ведёт
// эконометрику». Данные — текущее окно расписания, то есть эта и следующая
// неделя: архив вуза мы не держим.
func (s *Server) disciplines(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := map[string]any{"Title": "Дисциплины", "Tab": "lecturers", "Query": q, "Freshness": s.freshness(r)}
	if q != "" {
		hits, err := s.d.Schedule.SearchDisciplines(r.Context(), q, 20)
		if err != nil {
			http.Error(w, "не удалось выполнить поиск", http.StatusInternalServerError)
			return
		}
		type link struct {
			Label string
			Href  template.URL
		}
		type hitView struct {
			Name, Meta, Places string
			Lecturers, Groups  []link
			MoreGroups         int
		}
		var views []hitView
		for _, h := range hits {
			v := hitView{Name: h.Name, Meta: pluralN(h.Lessons, "пара", "пары", "пар") + " в окне · " + strings.Join(h.Kinds, ", ")}
			seen := map[string]bool{}
			var places []string
			for _, b := range h.Buildings {
				if p := s.d.BuildingLabel(b); p != "" && !seen[p] {
					seen[p] = true
					places = append(places, p)
				}
			}
			v.Places = strings.Join(places, ", ")
			for _, l := range h.Lecturers {
				v.Lecturers = append(v.Lecturers, link{Label: l.Name, Href: template.URL("/schedule?lecturer=" + strconv.FormatInt(l.Oid, 10))})
			}
			for i, g := range h.Groups {
				if i == maxGroupsShown {
					v.MoreGroups = len(h.Groups) - maxGroupsShown
					break
				}
				v.Groups = append(v.Groups, link{Label: g, Href: template.URL("/schedule?group=" + url.QueryEscape(g))})
			}
			views = append(views, v)
		}
		data["Hits"], data["Searched"] = views, true
		data["TooShort"] = len([]rune(q)) < 2
	}
	s.render(w, r, "disciplines", data)
}
