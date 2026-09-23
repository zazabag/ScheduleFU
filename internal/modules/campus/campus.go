// Package campus — план кампуса: корпуса, их этажи, где какая аудитория.
//
// Модуль не владеет таблицами: план — данные, собранные разово
// (tools/plans/osm.py) и вшитые в бинарник. Формат — Apple IMDF (GeoJSON:
// venue, building, footprint, level, позже unit), контуры корпусов — из
// OpenStreetMap. Ни от какого другого модуля campus не зависит: на вход —
// адрес и номер аудитории, как их отдаёт источник, на выход — корпус и
// этаж.
//
// Главное правило — показывать только то, что известно наверняка. Корпус
// следует из адреса, этаж — из номера по правилу корпуса. Где аудитория на
// этаже, мы пока не знаем: внутренних планов в открытом доступе нет, а
// угаданные места путали больше, чем помогали. Помещения (unit) появятся,
// когда их обведут по фото планов эвакуации.
package campus

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed plans/*.geojson
var plansFS embed.FS

// Building — корпус на плане в локальных метрах (x — на восток, y — на юг).
type Building struct {
	ID     string // «51-1»
	Name   string // «51 к.1»
	Levels int
	Path   string // контур для <path d>, дворы — вырезами (evenodd)
	CX, CY int    // куда ставить подпись
}

// Plan — план площадки.
type Plan struct {
	Name      string
	Source    string // обязательная ссылка на OSM
	Buildings []Building
	ViewBox   [4]int
}

// Location — ответ «где аудитория».
type Location struct {
	BuildingID   string
	BuildingName string
	Level        int // 0 — этаж по номеру не определить
	Levels       int
	Number       string
}

// Service — план кампуса.
type Service struct {
	plan Plan
}

type feature struct {
	ID          string          `json:"id"`
	FeatureType string          `json:"feature_type"`
	Geometry    json.RawMessage `json:"geometry"`
	Properties  struct {
		Name        map[string]string `json:"name"`
		Source      string            `json:"source"`
		BuildingIDs []string          `json:"building_ids"`
		Ordinal     *int              `json:"ordinal"`
	} `json:"properties"`
}

// Load читает вшитый план.
func Load() (*Service, error) {
	body, err := plansFS.ReadFile("plans/leningradsky.geojson")
	if err != nil {
		return nil, err
	}
	var fc struct {
		Features []feature `json:"features"`
	}
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("campus: %w", err)
	}
	return &Service{plan: build(fc.Features)}, nil
}

// build проецирует контуры в локальные метры. Равнопромежуточная проекция
// у центра кампуса: на трёхстах метрах искажение меньше сантиметра, а
// Меркатор ради этого тянуть незачем.
func build(fs []feature) Plan {
	var p Plan
	names := map[string]string{}
	levels := map[string]int{}
	type ring [][2]float64
	shapes := map[string][][]ring{}
	for _, f := range fs {
		switch f.FeatureType {
		case "venue":
			p.Name, p.Source = f.Properties.Name["ru"], f.Properties.Source
		case "building":
			names[f.ID] = f.Properties.Name["ru"]
		case "level":
			for _, b := range f.Properties.BuildingIDs {
				if f.Properties.Ordinal != nil && *f.Properties.Ordinal+1 > levels[b] {
					levels[b] = *f.Properties.Ordinal + 1
				}
			}
		case "footprint":
			var g struct {
				Coordinates [][][][2]float64 `json:"coordinates"`
			}
			if json.Unmarshal(f.Geometry, &g) != nil {
				continue
			}
			for _, b := range f.Properties.BuildingIDs {
				for _, poly := range g.Coordinates {
					var rs []ring
					for _, r := range poly {
						rs = append(rs, r)
					}
					shapes[b] = append(shapes[b], rs)
				}
			}
		}
	}
	var lon0, lat0 float64
	n := 0
	for _, polys := range shapes {
		for _, poly := range polys {
			for _, pt := range poly[0] {
				lon0, lat0, n = lon0+pt[0], lat0+pt[1], n+1
			}
		}
	}
	if n == 0 {
		return p
	}
	lon0, lat0 = lon0/float64(n), lat0/float64(n)
	kx := 111320 * math.Cos(lat0*math.Pi/180)
	const ky = 110540.0
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for id, polys := range shapes {
		b := Building{ID: strings.TrimPrefix(id, "building-"), Name: names[id], Levels: levels[id]}
		var d strings.Builder
		var sx, sy float64
		var cnt int
		for _, poly := range polys {
			for ri, r := range poly {
				for i, pt := range r {
					x, y := (pt[0]-lon0)*kx, -(pt[1]-lat0)*ky
					minX, minY, maxX, maxY = math.Min(minX, x), math.Min(minY, y), math.Max(maxX, x), math.Max(maxY, y)
					if i == 0 {
						d.WriteString("M")
					} else {
						d.WriteString("L")
					}
					d.WriteString(strconv.FormatFloat(x, 'f', 1, 64) + " " + strconv.FormatFloat(y, 'f', 1, 64) + " ")
					if ri == 0 {
						sx, sy, cnt = sx+x, sy+y, cnt+1
					}
				}
				d.WriteString("Z ")
			}
		}
		b.Path = strings.TrimSpace(d.String())
		b.CX, b.CY = int(sx/float64(cnt)), int(sy/float64(cnt))
		p.Buildings = append(p.Buildings, b)
	}
	sort.Slice(p.Buildings, func(i, j int) bool { return p.Buildings[i].ID < p.Buildings[j].ID })
	const pad = 12
	p.ViewBox = [4]int{int(minX) - pad, int(minY) - pad, int(maxX-minX) + 2*pad, int(maxY-minY) + 2*pad}
	return p
}

// Plan — план площадки.
func (s *Service) Plan() Plan { return s.plan }

// passages — как корпуса связаны изнутри. Только то, что видно на схемах
// вуза: переход 49/2 ↔ 51 к.1 через Нобелевский зал, стыки корпуса 49 с
// 49/2. Выдумывать переходы нельзя — по ним пошли бы люди.
var passages = map[string][]string{
	"49":   {"С 51 к.1 — переход на 3 этаже, мимо Нобелевского зала", "49 и 49/2 соединены на 3 и 4 этажах"},
	"51-1": {"С 49/2 — переход на 3 этаже, мимо Нобелевского зала"},
}

// Passages — переходы из корпуса в соседние.
func (s *Service) Passages(id string) []string { return passages[id] }

// Building — корпус по идентификатору.
func (s *Service) Building(id string) (Building, bool) {
	for _, b := range s.plan.Buildings {
		if b.ID == id {
			return b, true
		}
	}
	return Building{}, false
}

// corps — адрес источника → корпус плана и правило этажа. Корпуса 49 и
// 49/2 у вуза сидят под одним адресом и нумеруются одним рядом.
var corps = map[string]struct {
	id    string
	floor func(digits string) int
}{
	"Ленинградский проспект, 49/2":           {"49", firstOfThree},
	"Ленинградский проспект, 55":             {"55", firstOfThree},
	"Ленинградский проспект, 51, корп. 1":    {"51-1", firstTwoOfFour},
	"Ленинградский проспект, 51, строение 4": {"51-4", nil}, // нумерация лицея не подтверждена
}

// Правила этажа — те же, что в адаптере источника (ruz): там они решают,
// что показать в списке аудиторий, здесь — что показать на плане.
func firstOfThree(d string) int {
	if len(d) == 3 {
		return int(d[0] - '0')
	}
	return 0
}

func firstTwoOfFour(d string) int {
	if len(d) == 4 {
		return int(d[0]-'0')*10 + int(d[1]-'0')
	}
	return 0
}

var numberRe = regexp.MustCompile(`\d{2,4}[а-я]?`)

// Number — номер аудитории из имени источника: «ЛП49/2/313» → «313»,
// «ауд.406а_ОВП» → «406а», «0705(кк)» → «0705». Пусто — номера нет.
func Number(room string) string {
	if i := strings.LastIndexByte(room, '/'); i >= 0 {
		room = room[i+1:]
	}
	return numberRe.FindString(room)
}

// Locate — в каком корпусе и на каком этаже аудитория. Адрес — как у
// источника. Корпус известен всегда, когда адрес на Ленинградском; этаж —
// когда номер подчиняется правилу корпуса.
func (s *Service) Locate(address, room string) (Location, bool) {
	c, ok := corps[strings.TrimSpace(address)]
	if !ok {
		return Location{}, false
	}
	b, ok := s.Building(c.id)
	if !ok {
		return Location{}, false
	}
	loc := Location{BuildingID: b.ID, BuildingName: b.Name, Levels: b.Levels, Number: Number(room)}
	if c.floor != nil && loc.Number != "" {
		digits := loc.Number
		if i := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
			digits = digits[:i]
		}
		if lv := c.floor(digits); lv > 0 && lv <= b.Levels {
			loc.Level = lv
		}
	}
	return loc, true
}
