package web

import (
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/zazabag/schedulefu/internal/modules/campus"
)

// ─── план кампуса ────────────────────────────────────────────────────────────

// plan — Ленинградский кампус: настоящие контуры корпусов (OSM), выделенный
// корпус, полоса его этажей и аудитории выбранного этажа из расписания.
// Показывает только известное наверняка — корпус и этаж; места аудиторий
// на этаже появятся, когда этажи обведут по планам эвакуации.
//
// Параметры: room и b — аудитория и адрес, как их отдаёт источник (так на
// план ведут ссылки из расписания); c — корпус; l — этаж.
func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	if s.d.Campus == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	p := s.d.Campus.Plan()
	data := map[string]any{"Title": "План кампуса", "Tab": "rooms", "Plan": p}

	corp, level := q.Get("c"), 0
	level, _ = strconv.Atoi(q.Get("l"))
	var loc campus.Location
	if room := q.Get("room"); room != "" {
		l, ok := s.d.Campus.Locate(q.Get("b"), room)
		if !ok {
			data["NotFound"] = room
		} else {
			loc, corp = l, l.BuildingID
			if l.Level > 0 {
				level = l.Level
			}
			data["Loc"] = l
		}
	}
	b, hasCorp := s.d.Campus.Building(corp)

	type corpView struct {
		campus.Building
		On   bool
		Href template.URL
	}
	var corps []corpView
	for _, x := range p.Buildings {
		corps = append(corps, corpView{Building: x, On: hasCorp && x.ID == b.ID,
			Href: template.URL("/map?c=" + url.QueryEscape(x.ID))})
	}
	data["Corps"] = corps
	vb := p.ViewBox
	data["ViewBox"] = strconv.Itoa(vb[0]) + " " + strconv.Itoa(vb[1]) + " " + strconv.Itoa(vb[2]) + " " + strconv.Itoa(vb[3])

	if hasCorp {
		if level < 0 || level > b.Levels {
			level = 0
		}
		// Полоса этажей сверху вниз, как в здании.
		type levelView struct {
			N    int
			On   bool
			Href template.URL
		}
		var lv []levelView
		for n := b.Levels; n >= 1; n-- {
			v := url.Values{"c": {b.ID}, "l": {strconv.Itoa(n)}}
			lv = append(lv, levelView{N: n, On: n == level, Href: template.URL("/map?" + v.Encode())})
		}
		data["Corp"], data["Levels"], data["Level"] = b, lv, level
		data["Passages"] = s.d.Campus.Passages(b.ID)
		if level > 0 {
			data["Rooms"] = s.roomsOnLevel(r, b.ID, level, loc.Number)
		}
	}
	s.render(w, r, "map", data)
}

// levelRoom — аудитория этажа в списке под планом.
type levelRoom struct {
	Number, Meta string
	Here         bool
}

// roomsOnLevel — учебные аудитории корпуса на этаже, из справочника
// расписания. Место на этаже неизвестно, но список «что здесь есть»
// отвечает на вопрос «туда ли я пришёл».
func (s *Server) roomsOnLevel(r *http.Request, corp string, level int, here string) []levelRoom {
	if s.d.Schedule == nil {
		return nil
	}
	all, err := s.d.Schedule.Repo().Auditoriums(r.Context())
	if err != nil {
		return nil
	}
	var out []levelRoom
	for _, a := range all {
		l, ok := s.d.Campus.Locate(a.Building, a.Room)
		if !ok || l.BuildingID != corp || l.Level != level {
			continue
		}
		out = append(out, levelRoom{Number: roomShort(a.Room), Meta: s.roomMeta(a), Here: l.Number != "" && l.Number == here})
	}
	sort.Slice(out, func(i, j int) bool { return naturalLess(out[i].Number, out[j].Number) })
	return out
}

// mapHref — ссылка на план для аудитории на Ленинградском; иначе пусто.
func (s *Server) mapHref(building, auditorium string) template.URL {
	if s.d.Campus == nil || auditorium == "" {
		return ""
	}
	if _, ok := s.d.Campus.Locate(building, auditorium); !ok {
		return ""
	}
	v := url.Values{"room": {auditorium}, "b": {building}}
	return template.URL("/map?" + v.Encode())
}
