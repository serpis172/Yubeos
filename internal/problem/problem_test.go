package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProducesRFC9457(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/v1/x", nil)
	w := httptest.NewRecorder()
	Write(w, r, http.StatusUnprocessableEntity, "validation", "Invalid input", "bad", FieldError{Pointer: "/name", Message: "too long"})

	if w.Code != 422 || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status/content-type: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	var p Problem
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Type != TypeBase+"validation" || p.Status != 422 || p.Instance != "/api/v1/x" || len(p.Errors) != 1 || p.Errors[0].Pointer != "/name" {
		t.Fatalf("unexpected body: %+v", p)
	}
}
