package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/zazabag/schedulefu/internal/modules/campus"
)

func planServer(t *testing.T) *Server {
	t.Helper()
	s := windowServer(t)
	c, err := campus.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.d.Campus = c
	return s
}

func get(t *testing.T, s *Server, target string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

func TestPlanVydelyaetKorpusIEtazhAuditorii(t *testing.T) {
	s := planServer(t)
	q := url.Values{"room": {"ЛП51_1/0412"}, "b": {"Ленинградский проспект, 51, корп. 1"}}
	code, body := get(t, s, "/map?"+q.Encode())
	if code != http.StatusOK {
		t.Fatalf("код %d", code)
	}
	if !strings.Contains(body, `plan-roomchip here"`) {
		t.Error("аудитория не выделена в списке этажа")
	}
	if !strings.Contains(body, ">0409<") || strings.Contains(body, ">0611<") {
		t.Error("в списке этажа — только аудитории этого этажа")
	}
	for _, want := range []string{"Аудитория 0412", "корпус 51 к.1", "<b>4 этаж</b>", `class="plan-corp on"`, `class="plan-level on"`, "OpenStreetMap"} {
		if !strings.Contains(body, want) {
			t.Errorf("нет %q", want)
		}
	}
}

// Угаданных мест больше нет: план не рисует аудиторий на этаже, пока их
// не обвели по планам эвакуации.
func TestPlanNeRisuetMestAuditoriy(t *testing.T) {
	s := planServer(t)
	_, body := get(t, s, "/map?c=51-1&l=6")
	if strings.Contains(body, "<polygon") || strings.Contains(body, "примерн") {
		t.Error("на плане остались угаданные места")
	}
}

func TestChuzhoyKampusNaPlaneNazyvaetsya(t *testing.T) {
	s := planServer(t)
	q := url.Values{"room": {"31"}, "b": {"ул. Кибальчича, 1"}}
	_, body := get(t, s, "/map?"+q.Encode())
	if !strings.Contains(body, "не на Ленинградском") {
		t.Error("аудитория другого кампуса должна называться, а не молча пропадать")
	}
}
