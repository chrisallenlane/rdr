package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrisallenlane/rdr/internal/testutil"
)

func TestHealthz(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(Config{DB: db})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", got)
	}

	var body HealthStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Status != Ok {
		t.Errorf("status field: got %q, want %q", body.Status, Ok)
	}
}

func TestServeSpecYAML(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(Config{DB: db})

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "yaml") {
		t.Errorf("Content-Type missing yaml, got %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "openapi: 3.0") {
		t.Error("response body does not look like an OpenAPI YAML document")
	}
}

func TestServeSpecJSON(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(Config{DB: db})

	req := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Errorf("Content-Type missing json, got %q", rec.Header().Get("Content-Type"))
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decoding json response: %v", err)
	}
	if _, ok := doc["openapi"].(string); !ok {
		t.Error("decoded JSON spec is missing top-level openapi field")
	}
}

func TestWriteProblem_RFC7807Shape(t *testing.T) {
	rec := httptest.NewRecorder()
	writeProblem(rec, http.StatusBadRequest, "", "", "field x is invalid")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type: got %q, want application/problem+json", got)
	}

	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if p.Type != "about:blank" {
		t.Errorf("type: got %q, want about:blank (default)", p.Type)
	}
	if p.Title != "Bad Request" {
		t.Errorf("title: got %q, want %q", p.Title, "Bad Request")
	}
	if p.Status != http.StatusBadRequest {
		t.Errorf("status field: got %d, want %d", p.Status, http.StatusBadRequest)
	}
	if p.Detail == nil || *p.Detail != "field x is invalid" {
		t.Errorf("detail: got %v, want pointer to %q", p.Detail, "field x is invalid")
	}
}
