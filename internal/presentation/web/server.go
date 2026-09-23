// Package web — HTML-интерфейс. Серверный рендеринг, без JavaScript кроме
// подписки на уведомления. Доменных правил здесь нет: если в обработчике
// появляется if про расписание, он переезжает в модуль.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/campus"
	"github.com/zazabag/schedulefu/internal/modules/notes"
	"github.com/zazabag/schedulefu/internal/modules/notify"
	"github.com/zazabag/schedulefu/internal/modules/schedule"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Deps — что нужно страницам.
type Deps struct {
	Schedule *schedule.Service
	Notify   *notify.Service
	Notes    *notes.Service
	Clock    *clock.Clock
	// Campus — план корпусов; nil — раздела плана нет.
	Campus *campus.Service
	// NotesReady — настроена ли обработка записей. Раздел «Пары» без неё
	// показывает конспекты, но не предлагает записать новую пару: обещать
	// конспект, которого не будет, хуже, чем не предлагать вовсе.
	NotesReady bool
	// SiteLabel и BuildingLabel — короткие подписи; живут у источника.
	BuildingLabel func(building string) string
	Calendar      func(w http.ResponseWriter, r *http.Request) // ручка export
	// Dev включает параметр ?now=ЧЧ:ММ на экране дня: иначе состояние «пара
	// идёт» можно увидеть только дождавшись пары. В бою параметр игнорируется.
	Dev bool
	// HideWhereLecturer выключает «где преподаватель сейчас»: карточка
	// преподавателя уводит на его расписание недели. Нулевое значение —
	// функция включена, как и было до выключателя.
	HideWhereLecturer bool
}

// Server — страницы.
type Server struct {
	d     Deps
	pages map[string]*template.Template
}

// New разбирает шаблоны: каждая страница вместе с базовым, потому что все
// определяют блок content и в одном наборе последний затёр бы остальные.
func New(d Deps) (*Server, error) {
	funcs := template.FuncMap{"asset": AssetURL, "skinCSS": func(id string) string { return AssetURL("skins/" + id + ".css") }, "cover": lessonCover}
	pages := map[string]*template.Template{}
	for _, name := range []string{"rooms", "disciplines", "window", "together", "shared", "map", "schedule", "lessons", "summary", "lecturers", "settings"} {
		// hero.html — общий верхний блок дня: его рисуют и экран расписания,
		// и экран «где преподаватель».
		t, err := template.New("base").Funcs(funcs).ParseFS(templateFS, "templates/base.html", "templates/hero.html", "templates/onboard.html", "templates/manul.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("web: шаблон %s: %w", name, err)
		}
		pages[name] = t
	}
	return &Server{d: d, pages: pages}, nil
}

// Routes — маршруты интерфейса.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", staticHandler())
	// Первый раздел — расписание. Старые ссылки на аудитории вели на корень
	// с параметром site: они переезжают на /rooms, а не ломаются.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" && r.URL.Query().Get("site") != "" {
			http.Redirect(w, r, "/rooms?"+r.URL.RawQuery, http.StatusMovedPermanently)
			return
		}
		s.schedule(w, r)
	})
	mux.HandleFunc("GET /schedule", s.schedule)
	mux.HandleFunc("GET /rooms", s.rooms)
	mux.HandleFunc("GET /together", s.together)
	mux.HandleFunc("GET /map", s.plan)
	mux.HandleFunc("GET /n/{token}", s.sharedNote)
	mux.HandleFunc("POST /n/{token}", s.saveSharedNote)
	mux.HandleFunc("GET /lessons", s.lessons)
	mux.HandleFunc("GET /lessons/summary", s.lessonsSummary)
	mux.HandleFunc("POST /lessons", s.lessonsAction)
	mux.HandleFunc("POST /lessons/upload", s.lessonsUpload)
	mux.HandleFunc("GET /groups", s.groups)
	mux.HandleFunc("GET /lecturers", s.lecturers)
	mux.HandleFunc("GET /disciplines", s.disciplines)
	mux.HandleFunc("GET /settings", s.settings)
	mux.HandleFunc("POST /settings", s.settings)
	if s.d.Calendar != nil {
		mux.HandleFunc("GET /calendar.ics", s.d.Calendar)
	}
	return mux
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	data["Look"] = lookFrom(r)
	data["Onboard"] = s.onboard(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Страницы отвечают «что свободно прямо сейчас»: ответ из кэша через
	// минуту уже врёт.
	w.Header().Set("Cache-Control", "no-store, must-revalidate")
	if err := s.pages[page].ExecuteTemplate(w, "base", data); err != nil {
		fmt.Printf("web: отрисовка %s: %v\n", page, err)
	}
}

// freshness — когда данные последний раз сверялись с вузом.
func (s *Server) freshness(r *http.Request) string {
	at, ok := s.d.Schedule.Freshness(r.Context())
	if !ok {
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
	}
	return clock.DateTimeRu(at.In(s.d.Clock.Location()))
}

// ─── статика с отпечатком содержимого ────────────────────────────────────────

var assetOnce sync.Once
var assetVersions = map[string]string{}

// AssetURL — адрес файла с отпечатком: кэш держится год, при изменении
// файла меняется адрес, и старый кэш сам перестаёт использоваться.
func AssetURL(name string) string {
	assetOnce.Do(func() {
		_ = fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			body, _ := staticFS.ReadFile(p)
			sum := sha256.Sum256(body)
			assetVersions[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:])[:10]
			return nil
		})
	})
	if v, ok := assetVersions[name]; ok {
		return "/static/" + name + "?v=" + v
	}
	return "/static/" + name
}

func staticHandler() http.Handler {
	files := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".webmanifest") {
			// Стандартной библиотеке расширение неизвестно, а без верного типа
			// браузер не предлагает установить приложение.
			w.Header().Set("Content-Type", "application/manifest+json")
		}
		switch {
		case path.Base(r.URL.Path) == "sw.js":
			// Service worker отдаётся свежим всегда, иначе браузер месяцами
			// работает по старому сценарию.
			w.Header().Set("Cache-Control", "no-cache")
		case r.URL.Query().Get("v") != "", fontFile.MatchString(r.URL.Path):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		files.ServeHTTP(w, r)
	})
}

// onboardState — что сервер знает о трёх шагах: закреплено ли расписание.
// Уведомления и установка — знание браузера, их уточняет скрипт.
type onboardState struct {
	Pinned bool
	Label  string
	Key    string
}

func (s *Server) onboard(r *http.Request) onboardState {
	subj := SubjectFromCookie(r)
	if subj.IsZero() {
		return onboardState{}
	}
	label := subj.Group
	if subj.LecturerOid != 0 {
		label = s.d.Schedule.LecturerName(r.Context(), subj.LecturerOid, nil)
	}
	return onboardState{Pinned: true, Label: label, Key: subj.Key()}
}

// fontFile — шрифт с отпечатком содержимого в имени (tools/fetch_fonts.py):
// «inter-3f9a0c21bd.woff2». Имя меняется вместе с файлом, поэтому кэш на
// год безопасен, как у статики с ?v=.
var fontFile = regexp.MustCompile(`^/static/fonts/[a-z-]+-[0-9a-f]{10}\.woff2$`)
