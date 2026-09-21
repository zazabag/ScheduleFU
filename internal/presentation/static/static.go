// Package static — сборка версии для GitHub Pages.
//
// Pages раздаёт только файлы: данные выгружаются в JSON по дням, а расчёт
// «свободно сейчас», фильтры и поиск считает браузер. Логика сетки пар в
// app.js повторяет domain.BuildRoomView осознанно — на Pages Go нечем.
//
// Один файл на день, не на группу: файлов на каждую группу и преподавателя
// вышло бы четыре с половиной тысячи, и любое обновление означало бы
// столько же изменений в истории.
package static

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zazabag/schedulefu/internal/modules/schedule"
	sched "github.com/zazabag/schedulefu/internal/modules/schedule/domain"
	"github.com/zazabag/schedulefu/internal/platform/clock"
)

//go:embed site/*
var siteFS embed.FS

// Options — параметры сборки.
type Options struct {
	OutDir string
	// APIBase — адрес сервера, принимающего подписки. Пусто — кнопки
	// уведомлений нет: обещать то, чего не будет, хуже, чем не обещать.
	APIBase string
	// BuildingLabel — короткая подпись корпуса; живёт у источника.
	BuildingLabel func(string) string
	StyleCSS      []byte
}

// Короткие ключи: файл дня грузится на телефоне, каждое имя поля
// умножается на две тысячи пар.
type jsonLesson struct {
	Oid        int64    `json:"o"`
	AudOid     int64    `json:"a,omitempty"`
	Begins     string   `json:"b"`
	Ends       string   `json:"e"`
	Discipline int      `json:"d"`
	Kind       int      `json:"k"`
	Lecturer   int      `json:"l"`
	LecturerID int64    `json:"lo,omitempty"`
	Groups     []string `json:"g,omitempty"`
}

// Словари повторяющихся строк: в дне две тысячи пар, а дисциплин четыре
// сотни — замена на номера уменьшает файл вдвое до сжатия и вдвое
// сокращает работу телефона по разбору.
type dayDict struct {
	Disciplines []string `json:"d"`
	Lecturers   []string `json:"l"`
	Kinds       []string `json:"k"`
}

type jsonDay struct {
	Date        string       `json:"date"`
	GeneratedAt string       `json:"generated_at"`
	Dict        dayDict      `json:"dict"`
	Lessons     []jsonLesson `json:"lessons"`
}

type jsonMeta struct {
	GeneratedAt string           `json:"generated_at"`
	APIBase     string           `json:"api_base,omitempty"`
	Days        []string         `json:"days"`
	Sites       []jsonSite       `json:"sites"`
	Auditoriums []jsonAuditorium `json:"auditoriums"`
	Groups      []jsonGroup      `json:"groups"`
	Lecturers   []jsonLecturer   `json:"lecturers"`
	Slots       []sched.Slot     `json:"slots"`
}
type jsonSite struct {
	Value string `json:"v"`
	Label string `json:"l"`
	Rooms int    `json:"n"`
}
type jsonAuditorium struct {
	Oid      int64  `json:"o"`
	Room     string `json:"r"`
	Building string `json:"b"`
	Site     string `json:"s"`
	Floor    *int   `json:"f,omitempty"`
	Capacity *int   `json:"cap,omitempty"`
	Kind     string `json:"k,omitempty"`
}
type jsonGroup struct {
	Name   string `json:"n"`
	Course int    `json:"c,omitempty"`
}
type jsonLecturer struct {
	Oid  int64  `json:"o"`
	Name string `json:"n"`
}

type pool struct {
	values []string
	index  map[string]int
}

func (p *pool) id(v string) int {
	if v == "" {
		return -1
	}
	if p.index == nil {
		p.index = map[string]int{}
	}
	if n, ok := p.index[v]; ok {
		return n
	}
	p.values = append(p.values, v)
	p.index[v] = len(p.values) - 1
	return len(p.values) - 1
}

// Build собирает сайт в OutDir.
func Build(ctx context.Context, repo schedule.Repository, clk *clock.Clock, o Options) (int, error) {
	if err := os.MkdirAll(filepath.Join(o.OutDir, "data", "day"), 0o755); err != nil {
		return 0, err
	}
	now := clk.Now().Format(time.RFC3339)
	days, err := repo.Days(ctx)
	if err != nil {
		return 0, err
	}
	if len(days) == 0 {
		return 0, fmt.Errorf("в базе нет расписания — сначала сбор")
	}
	var keys []string
	for _, d := range days {
		key := d.Format("2006-01-02")
		keys = append(keys, key)
		lessons, err := repo.LessonsOn(ctx, d)
		if err != nil {
			return 0, err
		}
		var disc, lec, kinds pool
		day := jsonDay{Date: key, GeneratedAt: now, Lessons: make([]jsonLesson, 0, len(lessons))}
		for _, l := range lessons {
			jl := jsonLesson{Oid: l.LessonOid, Begins: l.BeginsAt, Ends: l.EndsAt,
				Discipline: disc.id(l.Discipline), Kind: kinds.id(l.KindOfWork), Lecturer: lec.id(l.LecturerName), Groups: l.GroupNames}
			if l.AuditoriumOid != nil {
				jl.AudOid = *l.AuditoriumOid
			}
			if l.LecturerOid != nil {
				jl.LecturerID = *l.LecturerOid
			}
			day.Lessons = append(day.Lessons, jl)
		}
		day.Dict = dayDict{Disciplines: disc.values, Lecturers: lec.values, Kinds: kinds.values}
		if err := writeJSON(filepath.Join(o.OutDir, "data", "day", key+".json"), day); err != nil {
			return 0, err
		}
	}

	meta := jsonMeta{GeneratedAt: now, APIBase: strings.TrimRight(o.APIBase, "/"), Days: keys, Slots: sched.Slots}
	sites, err := repo.Sites(ctx)
	if err != nil {
		return 0, err
	}
	for _, s := range sites {
		meta.Sites = append(meta.Sites, jsonSite{Value: s.Site.Slug, Label: s.Site.Label, Rooms: s.Rooms})
	}
	auds, err := repo.Auditoriums(ctx)
	if err != nil {
		return 0, err
	}
	for _, a := range auds {
		meta.Auditoriums = append(meta.Auditoriums, jsonAuditorium{Oid: a.Oid, Room: a.Room, Building: o.BuildingLabel(a.Building),
			Site: a.Site.Slug, Floor: a.Floor, Capacity: a.Capacity, Kind: a.Kind})
	}
	groups, err := repo.GroupNames(ctx)
	if err != nil {
		return 0, err
	}
	year := clk.Now().Year()
	if clk.Now().Month() < time.September {
		year--
	}
	for _, g := range groups {
		meta.Groups = append(meta.Groups, jsonGroup{Name: g, Course: courseFromName(g, year)})
	}
	lecs, err := repo.Lecturers(ctx)
	if err != nil {
		return 0, err
	}
	for _, l := range lecs {
		meta.Lecturers = append(meta.Lecturers, jsonLecturer{Oid: l.Oid, Name: l.Name})
	}
	if err := writeJSON(filepath.Join(o.OutDir, "data", "meta.json"), meta); err != nil {
		return 0, err
	}

	entries, _ := fs.ReadDir(siteFS, "site")
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, _ := siteFS.ReadFile("site/" + e.Name())
		if err := os.WriteFile(filepath.Join(o.OutDir, e.Name()), body, 0o644); err != nil {
			return 0, err
		}
	}
	if err := os.WriteFile(filepath.Join(o.OutDir, "style.css"), o.StyleCSS, 0o644); err != nil {
		return 0, err
	}
	if err := writeIcons(o.OutDir); err != nil {
		return 0, err
	}
	// .nojekyll обязателен: без него Pages прогоняет сайт через Jekyll и
	// выкидывает пути с подчёркиванием.
	return len(keys), os.WriteFile(filepath.Join(o.OutDir, ".nojekyll"), nil, 0o644)
}

// courseFromName: ПИ24-1 -> набор 2024.
func courseFromName(name string, academicYear int) int {
	digits, count := 0, 0
	for _, r := range name {
		if r >= '0' && r <= '9' {
			digits, count = digits*10+int(r-'0'), count+1
			if count == 2 {
				break
			}
			continue
		}
		if count > 0 {
			break
		}
	}
	if count != 2 {
		return 0
	}
	c := academicYear - (2000 + digits) + 1
	if c < 1 || c > 6 {
		return 0
	}
	return c
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}
