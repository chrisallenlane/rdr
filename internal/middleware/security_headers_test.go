package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSecurityHeaders_CSP verifies that the Content-Security-Policy header
// contains the critical security directives.
func TestSecurityHeaders_CSP(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("Content-Security-Policy")

	directives := []string{
		"frame-ancestors 'none'",
		"script-src 'self'",
		"default-src 'self'",
		"style-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
	}
	for _, d := range directives {
		if !strings.Contains(got, d) {
			t.Errorf(
				"Content-Security-Policy = %q, missing directive %q",
				got, d,
			)
		}
	}
}

// TestSecurityHeaders_XContentTypeOptions verifies that the
// X-Content-Type-Options header is set to "nosniff".
func TestSecurityHeaders_XContentTypeOptions(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	got := rec.Header().Get("X-Content-Type-Options")
	if got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

// TestSecurityHeaders_NextHandlerCalled verifies that the next handler in the
// middleware chain is called.
func TestSecurityHeaders_NextHandlerCalled(t *testing.T) {
	reached := false
	handler := SecurityHeaders(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !reached {
		t.Error("next handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
