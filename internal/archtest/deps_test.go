// Package archtest держит правила зависимостей из ARCHITECTURE.md § 6.
//
// Дешёвый аналог depguard: go list по всем пакетам и пять правил. Когда
// модули получат свои go.mod, границу возьмёт компилятор, и тест уйдёт.
package archtest

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

const root = "github.com/zazabag/schedulefu/"

type pkg struct {
	ImportPath string
	Imports    []string
}

func load(t *testing.T) []pkg {
	t.Helper()
	out, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func layer(path string) string {
	rel := strings.TrimPrefix(path, root)
	switch {
	case strings.HasPrefix(rel, "cmd/"):
		return "cmd"
	case strings.HasPrefix(rel, "internal/presentation/"):
		return "presentation"
	case strings.HasPrefix(rel, "internal/modules/"):
		return "modules"
	case strings.HasPrefix(rel, "internal/platform/"):
		return "platform"
	}
	return ""
}

// module — имя модуля: internal/modules/<name>/...
func module(path string) string {
	rel := strings.TrimPrefix(path, root+"internal/modules/")
	if i := strings.IndexByte(rel, '/'); i >= 0 {
		return rel[:i]
	}
	return rel
}

func TestPravilaZavisimostey(t *testing.T) {
	// Кто кому может смотреть в глаза. Порядок слоёв: cmd → presentation →
	// modules → platform; обратных рёбер нет.
	allowed := map[string]map[string]bool{
		"cmd":          {"presentation": true, "modules": true, "platform": true},
		"presentation": {"modules": true, "platform": true},
		"modules":      {"modules": true, "platform": true},
		"platform":     {"platform": true},
	}
	// Модуль → модуль: только через порты. Разрешённые рёбра между
	// модулями перечислены явно — это и есть карта портов.
	modulePorts := map[string]map[string]bool{
		"schedule": {"source": true},   // зовёт порт Source
		"notify":   {"schedule": true}, // ChangeReader и домен
		"export":   {"schedule": true}, // ScheduleReader и домен
	}
	for _, p := range load(t) {
		from := layer(p.ImportPath)
		if from == "" {
			continue // старый код вне канона проверяется после сноса
		}
		for _, imp := range p.Imports {
			if !strings.HasPrefix(imp, root) {
				continue
			}
			to := layer(imp)
			if to == "" {
				t.Errorf("%s импортирует %s: пакет вне слоёв канона", p.ImportPath, imp)
				continue
			}
			if !allowed[from][to] {
				t.Errorf("%s (%s) импортирует %s (%s): против направления слоёв", p.ImportPath, from, imp, to)
			}
			if from == "modules" && to == "modules" {
				a, b := module(p.ImportPath), module(imp)
				if a != b && !modulePorts[a][b] {
					t.Errorf("модуль %s импортирует модуль %s без объявленного порта", a, b)
				}
			}
		}
	}
}
