package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHandleSettingsForm(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Seed settings so we can verify the form shows the user's values, not
	// the defaults.
	if _, err := s.db.Exec(
		`INSERT INTO user_settings (user_id, show_descriptions, date_display) VALUES (?, 0, 1)`,
		userID,
	); err != nil {
		t.Fatalf("seeding settings: %v", err)
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/settings")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "show=false") {
		t.Errorf("body missing 'show=false'; got %q", body)
	}
	if !strings.Contains(body, "date=true") {
		t.Errorf("body missing 'date=true'; got %q", body)
	}
}

func TestHandleSettingsForm_DefaultsWhenNoRow(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	req := authedRequest(t, s, userID, http.MethodGet, "/settings")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// Default settings: ShowDescriptions=true, DateDisplayAbsolute=false.
	body := rec.Body.String()
	if !strings.Contains(body, "show=true") {
		t.Errorf("expected default show=true; got %q", body)
	}
	if !strings.Contains(body, "date=false") {
		t.Errorf("expected default date=false; got %q", body)
	}
}

func TestHandleUpdateSettings(t *testing.T) {
	postSettings := func(t *testing.T, s *Server, userID int64, form url.Values, htmx bool) *httptest.ResponseRecorder {
		t.Helper()
		req := authedRequest(t, s, userID, http.MethodPost, "/settings")
		req.Body = http.NoBody // replaced below
		req.Method = http.MethodPost
		newReq, _ := http.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
		newReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// Copy over the cookie and user context.
		newReq = newReq.WithContext(req.Context())
		for _, c := range req.Cookies() {
			newReq.AddCookie(c)
		}
		if htmx {
			newReq.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, newReq)
		return rec
	}

	readSettings := func(t *testing.T, s *Server, userID int64) (show, date int) {
		t.Helper()
		if err := s.db.QueryRow(
			`SELECT show_descriptions, date_display FROM user_settings WHERE user_id = ?`,
			userID,
		).Scan(&show, &date); err != nil {
			t.Fatalf("reading settings: %v", err)
		}
		return
	}

	t.Run("valid form inserts row and redirects", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := postSettings(t, s, userID, url.Values{
			"show_descriptions": {"0"},
			"date_display":      {"1"},
		}, false)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/settings" {
			t.Errorf("Location = %q, want /settings", loc)
		}
		show, date := readSettings(t, s, userID)
		if show != 0 || date != 1 {
			t.Errorf("settings = (show=%d, date=%d), want (0, 1)", show, date)
		}
	})

	t.Run("repeat POST updates row (ON CONFLICT)", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		postSettings(t, s, userID, url.Values{
			"show_descriptions": {"1"},
			"date_display":      {"0"},
		}, false)

		// Repeat with different values — must update, not fail on unique constraint.
		rec := postSettings(t, s, userID, url.Values{
			"show_descriptions": {"0"},
			"date_display":      {"1"},
		}, false)

		if rec.Code != http.StatusSeeOther {
			t.Fatalf("second POST status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		show, date := readSettings(t, s, userID)
		if show != 0 || date != 1 {
			t.Errorf("after update, settings = (show=%d, date=%d), want (0, 1)", show, date)
		}
	})

	t.Run("invalid show_descriptions defaults to 1", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		postSettings(t, s, userID, url.Values{
			"show_descriptions": {"banana"},
			"date_display":      {"0"},
		}, false)

		show, date := readSettings(t, s, userID)
		if show != 1 || date != 0 {
			t.Errorf("settings = (show=%d, date=%d), want (1, 0)", show, date)
		}
	})

	t.Run("out-of-range date_display defaults to 0", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		postSettings(t, s, userID, url.Values{
			"show_descriptions": {"1"},
			"date_display":      {"2"},
		}, false)

		show, date := readSettings(t, s, userID)
		if show != 1 || date != 0 {
			t.Errorf("settings = (show=%d, date=%d), want (1, 0)", show, date)
		}
	})

	t.Run("HTMX returns 204 with HX-Trigger flash, no redirect", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := postSettings(t, s, userID, url.Values{
			"show_descriptions": {"0"},
			"date_display":      {"0"},
		}, true)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("HTMX response should not redirect; got Location = %q", loc)
		}
		trigger := rec.Header().Get("HX-Trigger")
		if !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger missing showFlash; got %q", trigger)
		}
	})
}
