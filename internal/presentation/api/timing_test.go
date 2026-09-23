package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Замер с телефона — недоверенный ввод: принимаются только разобранные
// числа и путь страницы, длинное тело отвергается.
func TestZamerStranitsy(t *testing.T) {
	h := New(Deps{}).Routes()
	post := func(body string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/timing", strings.NewReader(body)))
		return rec.Code
	}
	if code := post(`{"path":"/rooms","nav":"navigate","standalone":true,"ttfb":420,"fcp":3100,"slow":[{"n":"fonts.googleapis.com/css2","d":2800}]}`); code != http.StatusNoContent {
		t.Errorf("замер: %d", code)
	}
	if code := post(`{"path":"http://evil"}`); code != http.StatusBadRequest {
		t.Errorf("чужой путь: %d", code)
	}
	if code := post(`{"path":"/` + strings.Repeat("x", 9000) + `"}`); code != http.StatusRequestEntityTooLarge {
		t.Errorf("длинное тело: %d", code)
	}
}
