package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThemeFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		cookie   string
		hasCook  bool
		expected string
	}{
		{"no cookie defaults to auto", "", false, "auto"},
		{"auto", "auto", true, "auto"},
		{"solarized-light", "solarized-light", true, "solarized-light"},
		{"solarized-dark", "solarized-dark", true, "solarized-dark"},
		{"modus-light", "modus-light", true, "modus-light"},
		{"modus-dark", "modus-dark", true, "modus-dark"},
		{"legacy light maps to solarized-light", "light", true, "solarized-light"},
		{"legacy dark maps to solarized-dark", "dark", true, "solarized-dark"},
		{"invalid value defaults to auto", "bogus", true, "auto"},
		{"empty cookie defaults to auto", "", true, "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/", nil)
			if tt.hasCook {
				req.AddCookie(&http.Cookie{Name: "rdr_theme", Value: tt.cookie})
			}
			got := themeFromRequest(req)
			if got != tt.expected {
				t.Errorf("themeFromRequest() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestHandleThemeChange(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		name         string
		formTheme    string
		wantCookie   string
		referer      string
		wantRedirect string
	}{
		{"sets auto", "auto", "auto", "", "/items"},
		{"sets solarized-light", "solarized-light", "solarized-light", "", "/items"},
		{"sets solarized-dark", "solarized-dark", "solarized-dark", "", "/items"},
		{"sets modus-light", "modus-light", "modus-light", "", "/items"},
		{"sets modus-dark", "modus-dark", "modus-dark", "", "/items"},
		{"invalid theme defaults to solarized-light", "bogus", "solarized-light", "", "/items"},
		{"redirects to referer path", "modus-light", "modus-light", "http://example.com/feeds", "/feeds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader("theme=" + tt.formTheme)
			req, _ := http.NewRequest("POST", "/theme", body)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tt.referer != "" {
				req.Header.Set("Referer", tt.referer)
			}

			rr := httptest.NewRecorder()
			s.handleThemeChange(rr, req)

			// Check redirect.
			if rr.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", rr.Code, http.StatusSeeOther)
			}
			if loc := rr.Header().Get("Location"); loc != tt.wantRedirect {
				t.Errorf("redirect = %q, want %q", loc, tt.wantRedirect)
			}

			// Check cookie.
			cookies := rr.Result().Cookies()
			var found bool
			for _, c := range cookies {
				if c.Name == "rdr_theme" {
					found = true
					if c.Value != tt.wantCookie {
						t.Errorf("cookie = %q, want %q", c.Value, tt.wantCookie)
					}
				}
			}
			if !found {
				t.Error("rdr_theme cookie not set")
			}
		})
	}

	t.Run("HTMX request returns 204 with HX-Trigger", func(t *testing.T) {
		body := strings.NewReader("theme=modus-dark")
		req, _ := http.NewRequest("POST", "/theme", body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rr := httptest.NewRecorder()
		s.handleThemeChange(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("status = %d, want %d", rr.Code, http.StatusNoContent)
		}
		trigger := rr.Header().Get("HX-Trigger")
		if !strings.Contains(trigger, "setTheme") {
			t.Errorf("HX-Trigger = %q, want to contain setTheme", trigger)
		}
		if !strings.Contains(trigger, "modus-dark") {
			t.Errorf("HX-Trigger = %q, want to contain modus-dark", trigger)
		}
		// Cookie should still be set.
		var found bool
		for _, c := range rr.Result().Cookies() {
			if c.Name == "rdr_theme" && c.Value == "modus-dark" {
				found = true
			}
		}
		if !found {
			t.Error("rdr_theme cookie not set for HTMX request")
		}
	})
}
