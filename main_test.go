package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Checks the M1 acceptance criterion from PRD §12:
// GET /healthz -> 200 "ok", any other method -> 405.
//
// httptest.NewRecorder lets us call the handler in memory, with no real
// network port. The same approach scales up to the end-to-end login test
// required in PRD §11.
// look at: https://pkg.go.dev/net/http/httptest
func TestHealthz(t *testing.T) {
	h := newHandler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("GET /healthz = %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want 405", rec.Code)
	}
}
