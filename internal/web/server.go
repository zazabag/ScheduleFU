package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// StaticFS отдаёт встроенные статические файлы: тем же стилем
// пользуется генератор версии для GitHub Pages.
func StaticFS() embed.FS { return staticFS }

// Server — веб-интерфейс.
type Server struct {
	store *store.Store
	loc   *time.Location
	pages map[string]*template.Template
}

// New собирает сервер и разбирает шаблоны.
//
// Каждая страница парсится отдельно вместе с базовым шаблоном: все они
// определяют блок "content", и в одном наборе последний затёр бы остальные.
func New(s *store.Store, loc *time.Location) (*Server, error) {
	if loc == nil {
		loc = time.UTC
	}
	pages := map[string]*template.Template{}
	for _, name := range []string{"rooms", "schedule", "groups", "lecturers"} {
		t, err := template.New("base").ParseFS(templateFS,
			"templates/base.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("web: шаблон %s: %w", name, err)
		}
		pages[name] = t
	}
	return &Server{store: s, loc: loc, pages: pages}, nil
}

// Routes отдаёт маршруты интерфейса.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("GET /{$}", s.handleRooms)
	mux.HandleFunc("GET /schedule", s.handleSchedule)
	mux.HandleFunc("GET /groups", s.handleGroups)
	mux.HandleFunc("GET /lecturers", s.handleLecturers)
	return mux
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "страница не найдена", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		// Заголовки уже отправлены — остаётся только не молчать в логах.
		fmt.Printf("web: отрисовка %s: %v\n", page, err)
	}
}

func (s *Server) now() time.Time { return time.Now().In(s.loc) }

// freshness — человеческая подпись о свежести данных.
func (s *Server) freshness(r *http.Request) string {
	at, ok, err := s.store.LastSuccessfulRun(r.Context())
	if err != nil || !ok {
		return ""
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return "только что"
	case d < time.Hour:
		return fmt.Sprintf("%d мин назад", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d ч назад", int(d.Hours()))
	default:
		return FormatDateTimeRu(at.In(s.loc))
	}
}

// ——— Свободные аудитории ———

type tabView struct {
	Label string
	Value string
	On    bool
}

type chipView struct {
	Label string
	Href  string
	On    bool
}

type roomRow struct {
	Room       string
	Meta       string
	Until      string
	UntilClass string
	Cells      []SlotCell
	Lessons    []store.LessonView
}

func (s *Server) handleRooms(w http.ResponseWriter, r *http.Request) {
	campus := r.URL.Query().Get("campus")
	if campus == "" {
		campus = "leningradsky"
	}
	floorParam := r.URL.Query().Get("floor")

	campuses, err := s.store.Campuses(r.Context())
	if err != nil {
		http.Error(w, "не удалось получить список корпусов", http.StatusInternalServerError)
		return
	}
	days, err := s.store.CampusDay(r.Context(), campus, "", s.now())
	if err != nil {
		http.Error(w, "не удалось получить занятость", http.StatusInternalServerError)
		return
	}

	clock := NowClock(s.loc)
	label := campus
	for _, c := range campuses {
		if c.Campus == campus {
			label = c.Building
		}
	}

	// Этажи собираем из того, что реально есть на площадке: нумерация
	// в корпусах разная, и общего списка этажей не существует.
	floorSet := map[int]bool{}
	var rooms []roomRow
	free := 0
	for _, d := range days {
		v := BuildRoomView(d, clock)
		if d.Floor != nil {
			floorSet[*d.Floor] = true
		}
		if !v.FreeNow {
			continue
		}
		free++
		if floorParam != "" {
			f, err := strconv.Atoi(floorParam)
			if err != nil || d.Floor == nil || *d.Floor != f {
				continue
			}
		}
		rooms = append(rooms, roomRow{
			Room:       d.Room,
			Meta:       roomMeta(d.Auditorium),
			Until:      untilLabel(v.FreeUntil),
			UntilClass: untilClass(v.FreeUntil),
			Cells:      v.Cells,
			Lessons:    d.Lessons,
		})
	}

	var floors []int
	for f := range floorSet {
		floors = append(floors, f)
	}
	sort.Ints(floors)

	chips := []chipView{{
		Label: "все",
		Href:  "/?campus=" + url.QueryEscape(campus),
		On:    floorParam == "",
	}}
	for _, f := range floors {
		v := strconv.Itoa(f)
		chips = append(chips, chipView{
			Label: v,
			Href:  "/?campus=" + url.QueryEscape(campus) + "&floor=" + v,
			On:    floorParam == v,
		})
	}

	tabs := make([]tabView, 0, len(campuses))
	for _, c := range campuses {
		tabs = append(tabs, tabView{
			Label: ShortCampus(c.Building), Value: c.Campus, On: c.Campus == campus,
		})
	}

	s.render(w, "rooms", map[string]any{
		"Title": "Свободные аудитории", "Tab": "rooms",
		"Clock": clock, "CampusLabel": label,
		"FreeCount": free, "TotalCount": len(days),
		"Campuses": tabs, "Floors": chips, "Rooms": rooms,
		"Freshness": s.freshness(r),
	})
}

func roomMeta(a store.Auditorium) string {
	parts := []string{ShortBuilding(a.Building)}
	if a.Floor != nil {
		parts = append(parts, strconv.Itoa(*a.Floor)+" эт")
	}
	if a.Capacity != nil && *a.Capacity > 0 {
		parts = append(parts, strconv.Itoa(*a.Capacity)+" мест")
	}
	return strings.Join(parts, " · ")
}

// shortBuilding сокращает адрес до узнаваемого куска: полная строка
// «Ленинградский проспект, 51, корп. 1» в строке аудитории не помещается,
// а различать корпуса необходимо — номера в них похожи (313 и 0314).
func ShortBuilding(b string) string {
	switch {
	case strings.Contains(b, "49"):
		return "49/2"
	case strings.Contains(b, "51"):
		return "51"
	case strings.Contains(b, "55"):
		return "55"
	case strings.Contains(b, "Вешняковский"):
		return "Вешняковский"
	case strings.Contains(b, "Масловка"):
		return "Масловка"
	case strings.Contains(b, "Щербаковская"):
		return "Щербаковская"
	case strings.Contains(b, "Кибальчича"):
		return "Кибальчича"
	case strings.Contains(b, "Дундича"):
		return "Дундича"
	}
	return b
}

// shortCampus сокращает подпись площадки до узнаваемого слова.
//
// Полные адреса источника («4-й Вешняковский проезд, 4») в переключателе
// не помещаются: их тринадцать, и лентой они занимали весь экран, оставляя
// сами аудитории за краем.
func ShortCampus(b string) string {
	switch {
	case strings.Contains(b, "Ленинградский"):
		return "Ленинградский"
	case strings.Contains(b, "Масловка") && strings.Contains(b, "стр"):
		return "Масловка, стр. 1"
	case strings.Contains(b, "Масловка"):
		return "Масловка"
	case strings.Contains(b, "Вешняковский"):
		return "Вешняковский"
	case strings.Contains(b, "Щербаковская"):
		return "Щербаковская"
	case strings.Contains(b, "Дундича"):
		return "Дундича"
	case strings.Contains(b, "Златоустинский"):
		return "Златоустинский"
	case strings.Contains(b, "Кибальчича") && strings.Contains(b, "строение 2"):
		return "Кибальчича, 2"
	case strings.Contains(b, "Кибальчича"):
		return "Кибальчича, 1"
	case strings.Contains(b, "Баумана") && strings.Contains(b, "Главный"):
		return "МГТУ, главный"
	case strings.Contains(b, "Баумана"):
		return "МГТУ, лабораторный"
	case strings.Contains(b, "Касаткина"):
		return "Касаткина"
	case strings.Contains(b, "Фили"):
		return "Фили"
	}
	return b
}

func untilLabel(until string) string {
	if until == "" {
		return "до конца дня"
	}
	return "до " + until
}

func untilClass(until string) string {
	if until == "" {
		return ""
	}
	return "warn"
}
