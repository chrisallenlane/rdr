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

	// Duplicate-username check runs separately because it needs an
	// existing user in the DB and verifies the rendered error message.
	t.Run("duplicate username shows friendly error", func(t *testing.T) {
		s := newTestServer(t)
		createTestUser(t, s, "taken", "testpass1")

		form := url.Values{
			"username":         {"taken"},
			"password":         {"securepass"},
			"password_confirm": {"securepass"},
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
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if !strings.Contains(rec.Body.String(), "Username is already taken") {
			t.Errorf("body should mention 'Username is already taken'; got %q", rec.Body.String())
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == "rdr_session" {
				t.Error("no session cookie should be set on duplicate-username registration")
			}
		}
	})

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

	t.Run("session cookie gets Secure flag when X-Forwarded-Proto is https", func(t *testing.T) {
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
		req.Header.Set("X-Forwarded-Proto", "https")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		var found bool
		for _, c := range rec.Result().Cookies() {
			if c.Name == "rdr_session" {
				found = true
				if !c.Secure {
					t.Error("expected Secure flag on session cookie when X-Forwarded-Proto is https")
				}
			}
		}
		if !found {
			t.Fatal("rdr_session cookie not set")
		}
	})

	t.Run("non-existent username runs bcrypt to avoid timing oracle", func(t *testing.T) {
		// Guards against regression of the username-enumeration timing
		// channel fix. Bcrypt at DefaultCost (10) costs ~70-100ms; a pure
		// DB-miss path is sub-millisecond. A 20ms floor cleanly distinguishes
		// the two without being flaky on slow CI.
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
		start := time.Now()
		s.ServeHTTP(rec, req)
		elapsed := time.Since(start)

		if elapsed < 20*time.Millisecond {
			t.Errorf("non-existent login took %v; expected >= 20ms (decoy bcrypt must run to prevent username enumeration)", elapsed)
		}
	})

	t.Run("non-ErrNoRows DB error still runs decoy bcrypt", func(t *testing.T) {
		// Guards against a subtle regression: the decoy bcrypt must run for
		// any DB error, not only sql.ErrNoRows. Otherwise the response time
		// for a transient DB failure (sub-millisecond) becomes
		// distinguishable from "user does not exist" (~bcrypt cost),
		// reintroducing a timing oracle. We force a non-ErrNoRows error by
		// closing the underlying *sql.DB before the request — Scan then
		// returns "sql: database is closed" rather than sql.ErrNoRows.
		s := newTestServer(t)

		// Close the DB to force a non-ErrNoRows error from QueryRow.Scan.
		if err := s.db.Close(); err != nil {
			t.Fatalf("closing test db: %v", err)
		}

		form := url.Values{
			"username": {"anybody"},
			"password": {"testpass1"},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/login",
			strings.NewReader(form.Encode()),
		)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		start := time.Now()
		s.ServeHTTP(rec, req)
		elapsed := time.Since(start)

		// Response should be the friendly error page (200), not a 500.
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		// The decoy bcrypt must have run. Same 20ms floor as the
		// non-existent-username test.
		if elapsed < 20*time.Millisecond {
			t.Errorf("login with closed DB took %v; expected >= 20ms (decoy bcrypt must run on non-ErrNoRows DB errors too)", elapsed)
		}
	})
}

// TestHandleRegister_LongPassword pins the current behavior when a password
// longer than 72 bytes is submitted to /register.
//
// Contrary to a common assumption, golang.org/x/crypto/bcrypt does NOT
// silently truncate passwords at 72 bytes — it returns ErrPasswordTooLong
// from GenerateFromPassword. handleRegister surfaces that error via
// s.internalError, which renders a generic 500 page. The user has no
// indication that the password length was the cause.
//
// This is a UX bug, not a security bug: registration fails (no truncated
// hash is ever stored), so there is no shared-prefix login attack. The
// fix is to add an explicit length validation alongside the existing
// minimum-length check so the user gets a friendly error instead of a 500.
//
// Severity is LOW under the homelab threat model: a single intentional
// operator who pastes a >72-byte password sees a confusing 500 once and
// shortens it. There is no risk to anyone else.
func TestHandleRegister_LongPassword(t *testing.T) {
	s := newTestServer(t)

	// 100 bytes. Above bcrypt's 72-byte limit.
	password := strings.Repeat("a", 100)
	form := url.Values{
		"username":         {"longpw"},
		"password":         {password},
		"password_confirm": {password},
	}
	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	// Current behavior: 500. After a fix that validates max-length up
	// front, this should become 200 with a friendly error message (the
	// same shape as the existing "Password must be at least 8 characters"
	// error). When that fix lands, this test should be updated to assert
	// the new contract.
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d (pins current behavior: bcrypt's ErrPasswordTooLong is surfaced as a generic 500 with no user-visible explanation)",
			rec.Code, http.StatusInternalServerError)
	}

	// No user row should have been inserted. This is the saving grace:
	// the failure is loud, not silent. There is no truncated hash in the
	// DB that would later be matched by a shorter password.
	var count int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM users WHERE username = ?", "longpw",
	).Scan(&count); err != nil {
		t.Fatalf("querying user: %v", err)
	}
	if count != 0 {
		t.Errorf("user row count = %d, want 0 (registration failed; no row should be inserted)", count)
	}

	// And no session cookie either.
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_session" {
			t.Error("no session cookie should be set when registration fails")
		}
	}
}

// TestHandleRegister_NoUsernameLengthCap pins the current uncapped
// username behavior. A 10 KB username is accepted and stored verbatim.
//
// Severity is LOW: single-user homelab, no public registration on the
// hostile internet. The downside is a one-shot paste-bomb that produces
// a fat row; the system continues to function. A 64-char cap would be
// reasonable hygiene but is not urgent.
func TestHandleRegister_NoUsernameLengthCap(t *testing.T) {
	s := newTestServer(t)

	longUser := strings.Repeat("u", 10_000)
	form := url.Values{
		"username":         {longUser},
		"password":         {"securepass"},
		"password_confirm": {"securepass"},
	}
	req := httptest.NewRequest(http.MethodPost, "/register",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("register status = %d, want %d (no length cap currently exists)",
			rec.Code, http.StatusSeeOther)
	}

	var got string
	if err := s.db.QueryRow(
		"SELECT username FROM users WHERE LENGTH(username) = ?",
		len(longUser),
	).Scan(&got); err != nil {
		t.Fatalf("querying long-username user: %v", err)
	}
	if got != longUser {
		t.Errorf("stored username length = %d, want %d (no truncation expected at this layer)",
			len(got), len(longUser))
	}
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
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	if err := s.createSession(rec, req, userID); err != nil {
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
			if c.Secure {
				t.Error("Secure should be false on plain HTTP request")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Errorf("SameSite = %v, want Lax", c.SameSite)
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
