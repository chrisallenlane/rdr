package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
)

func TestHandleLoginForm(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandleRegisterForm(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandleRegister(t *testing.T) {
	tests := []struct {
		name            string
		username        string
		password        string
		passwordConfirm string
		wantStatus      int
		wantLocation    string
		wantCookie      bool
	}{
		{
			name:            "valid registration",
			username:        "newuser",
			password:        "securepass",
			passwordConfirm: "securepass",
			wantStatus:      http.StatusSeeOther,
			wantLocation:    "/items",
			wantCookie:      true,
		},
		{
			name:            "username too short",
			username:        "ab",
			password:        "securepass",
			passwordConfirm: "securepass",
			wantStatus:      http.StatusOK,
		},
		{
			name:            "username exactly at minimum length",
			username:        "abc",
			password:        "securepass",
			passwordConfirm: "securepass",
			wantStatus:      http.StatusSeeOther,
			wantLocation:    "/items",
			wantCookie:      true,
		},
		{
			name:            "password too short",
			username:        "validuser",
			password:        "short",
			passwordConfirm: "short",
			wantStatus:      http.StatusOK,
		},
		{
			name:            "password exactly at minimum length",
			username:        "validusr2",
			password:        "12345678",
			passwordConfirm: "12345678",
			wantStatus:      http.StatusSeeOther,
			wantLocation:    "/items",
			wantCookie:      true,
		},
		{
			name:            "mismatched passwords",
			username:        "validuser",
			password:        "securepass1",
			passwordConfirm: "securepass2",
			wantStatus:      http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t)

			form := url.Values{
				"username":         {tt.username},
				"password":         {tt.password},
				"password_confirm": {tt.passwordConfirm},
			}
			req := httptest.NewRequest(
				http.MethodPost,
				"/register",
				strings.NewReader(form.Encode()),
			)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantLocation != "" {
				loc := rec.Header().Get("Location")
				if loc != tt.wantLocation {
					t.Errorf("Location = %q, want %q", loc, tt.wantLocation)
				}
			}

			if tt.wantCookie {
				found := false
				for _, c := range rec.Result().Cookies() {
					if c.Name == "rdr_session" && c.Value != "" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected rdr_session cookie to be set")
				}

				var count int
				if err := s.db.QueryRow(
					"SELECT COUNT(*) FROM users WHERE username = ?",
					tt.username,
				).Scan(&count); err != nil {
					t.Fatalf("querying user: %v", err)
				}
				if count != 1 {
					t.Errorf("user row count = %d, want 1", count)
				}
			}
		})
	}

	t.Run("duplicate username", func(t *testing.T) {
		s := newTestServer(t)
		createTestUser(t, s, "existing", "password1")

		form := url.Values{
			"username":         {"existing"},
			"password":         {"password1"},
			"password_confirm": {"password1"},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/register",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestHandleLogin(t *testing.T) {
	t.Run("valid credentials", func(t *testing.T) {
		s := newTestServer(t)
		createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{
			"username": {"testuser"},
			"password": {"testpass1"},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/login",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/items" {
			t.Errorf("Location = %q, want /items", loc)
		}

		found := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == "rdr_session" && c.Value != "" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected rdr_session cookie to be set")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		s := newTestServer(t)
		createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{
			"username": {"testuser"},
			"password": {"wrongpass"},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/login",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("non-existent username", func(t *testing.T) {
		s := newTestServer(t)

		form := url.Values{
			"username": {"nobody"},
			"password": {"testpass1"},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/login",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestHandleLogout(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	sessionID := createTestSession(t, s, userID)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: sessionID})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}

	// Cookie should be cleared (MaxAge < 0).
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_session" {
			found = true
			if c.MaxAge >= 0 {
				t.Errorf("cookie MaxAge = %d, want < 0", c.MaxAge)
			}
			break
		}
	}
	if !found {
		t.Error("expected rdr_session cookie in response")
	}

	// Session row should be deleted.
	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM sessions WHERE id = ?",
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("querying session: %v", err)
	}
	if count != 0 {
		t.Errorf("session row count = %d, want 0", count)
	}
}

func TestCreateSession(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	rec := httptest.NewRecorder()
	if err := s.createSession(rec, userID); err != nil {
		t.Fatalf("createSession: %v", err)
	}

	// Cookie should be set.
	var sessionID string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_session" {
			sessionID = c.Value
			if c.MaxAge <= 0 {
				t.Errorf("cookie MaxAge = %d, want > 0", c.MaxAge)
			}
			if !c.HttpOnly {
				t.Error("expected HttpOnly cookie")
			}
			break
		}
	}
	if sessionID == "" {
		t.Fatal("rdr_session cookie not set")
	}

	// Session row should exist with future expiration.
	var expiresAt string
	if err := s.db.QueryRow(
		"SELECT expires_at FROM sessions WHERE id = ? AND user_id = ?",
		sessionID, userID,
	).Scan(&expiresAt); err != nil {
		t.Fatalf("querying session: %v", err)
	}
	if expiresAt == "" {
		t.Error("expected non-empty expires_at")
	}

	// Verify the expiration is in the future (not the past).
	// SQLite may return the timestamp in multiple formats.
	var parsed time.Time
	for _, layout := range []string{model.SQLiteDatetimeFmt, time.RFC3339} {
		if p, err := time.Parse(layout, expiresAt); err == nil {
			parsed = p
			break
		}
	}
	if parsed.IsZero() {
		t.Fatalf("could not parse expires_at %q", expiresAt)
	}
	if !parsed.After(time.Now().UTC()) {
		t.Errorf("expires_at = %v, want future time", parsed)
	}
}
