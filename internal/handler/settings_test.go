package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/token"
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

// TestSettings_TokenLifecycle exercises the full create-via-UI →
// use-against-API → revoke-via-UI → reject-via-API flow that ties
// together internal/handler/settings.go and internal/api/middleware.go.
// It is the deferred integration test from ticket #64.
func TestSettings_TokenLifecycle(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	sessionID := createTestSession(t, s, userID)
	sessionCookie := &http.Cookie{Name: "rdr_session", Value: sessionID}

	// 1. Create a token via the UI.
	form := url.Values{"name": {"ci"}}
	createReq, _ := http.NewRequest(http.MethodPost, "/settings/tokens", strings.NewReader(form.Encode()))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(sessionCookie)

	createRec := httptest.NewRecorder()
	s.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, want %d (body=%q)", createRec.Code, http.StatusSeeOther, createRec.Body.String())
	}

	var rawToken string
	for _, c := range createRec.Result().Cookies() {
		if c.Name == newTokenCookieName {
			rawToken = c.Value
			break
		}
	}
	if rawToken == "" {
		t.Fatal("create did not set rdr_new_token cookie")
	}
	if !strings.HasPrefix(rawToken, "rdr_pat_") {
		t.Errorf("token has unexpected prefix: %q", rawToken)
	}

	// 2. Use the bearer token against the API.
	apiReq, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	apiReq.Header.Set("Authorization", "Bearer "+rawToken)
	apiRec := httptest.NewRecorder()
	s.ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusOK {
		t.Fatalf("api with valid token: status = %d, want 200; body = %q",
			apiRec.Code, apiRec.Body.String())
	}

	// 3. Look up the token row id (the UI uses /settings/tokens/{id}/delete).
	tokens, err := token.List(s.db, userID)
	if err != nil {
		t.Fatalf("listing tokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("token.List = %d rows, want 1", len(tokens))
	}
	tokenID := tokens[0].ID

	// 4. Revoke it via the UI.
	revokeURL := "/settings/tokens/" + strconv.FormatInt(tokenID, 10) + "/delete"
	revokeReq, _ := http.NewRequest(http.MethodPost, revokeURL, nil)
	revokeReq.AddCookie(sessionCookie)
	revokeRec := httptest.NewRecorder()
	s.ServeHTTP(revokeRec, revokeReq)
	if revokeRec.Code != http.StatusSeeOther {
		t.Fatalf("revoke status = %d, want %d (body=%q)", revokeRec.Code, http.StatusSeeOther, revokeRec.Body.String())
	}

	// 5. The same bearer is now rejected.
	apiReq2, _ := http.NewRequest(http.MethodGet, "/api/v1/me", nil)
	apiReq2.Header.Set("Authorization", "Bearer "+rawToken)
	apiRec2 := httptest.NewRecorder()
	s.ServeHTTP(apiRec2, apiReq2)
	if apiRec2.Code != http.StatusUnauthorized {
		t.Errorf("api after revoke: status = %d, want 401", apiRec2.Code)
	}
}

// TestRevokeToken_RejectsCrossUser confirms that one user cannot
// revoke another user's token via the settings UI.
func TestRevokeToken_RejectsCrossUser(t *testing.T) {
	s := newTestServer(t)
	aliceID := createTestUser(t, s, "alice", "testpass1")
	bobID := createTestUser(t, s, "bob", "testpass1")

	// Mint a token for bob.
	bobRaw, bobTokID, err := token.Generate(s.db, bobID, "bob's token", time.Time{})
	if err != nil {
		t.Fatalf("generating bob's token: %v", err)
	}

	// Alice tries to revoke it.
	aliceSession := createTestSession(t, s, aliceID)
	revokeReq, _ := http.NewRequest(http.MethodPost,
		"/settings/tokens/"+strconv.FormatInt(bobTokID, 10)+"/delete", nil)
	revokeReq.AddCookie(&http.Cookie{Name: "rdr_session", Value: aliceSession})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, revokeReq)

	if rec.Code != http.StatusNotFound {
		t.Errorf("cross-user revoke status = %d, want 404", rec.Code)
	}

	// Bob's token is still usable: validate against the api directly.
	uid, _, err := token.Validate(s.db, bobRaw)
	if err != nil {
		t.Errorf("after alice's failed revoke, bob's token does not validate: %v", err)
	}
	if uid != bobID {
		t.Errorf("validated user id = %d, want %d", uid, bobID)
	}
}

func TestParseTokenExpiry(t *testing.T) {
	now := time.Now()
	future := now.Add(48 * time.Hour).Format("2006-01-02")
	past := now.Add(-48 * time.Hour).Format("2006-01-02")

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantSet bool // true if non-zero result expected
	}{
		{name: "empty returns zero time", input: "", wantErr: false, wantSet: false},
		{name: "future date is end-of-day UTC", input: future, wantErr: false, wantSet: true},
		{name: "past date errors", input: past, wantErr: true},
		{name: "malformed errors", input: "not-a-date", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTokenExpiry(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTokenExpiry(%q): want error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTokenExpiry(%q) error: %v", tt.input, err)
			}
			if tt.wantSet && got.IsZero() {
				t.Errorf("parseTokenExpiry(%q): want non-zero time, got zero", tt.input)
			}
			if !tt.wantSet && !got.IsZero() {
				t.Errorf("parseTokenExpiry(%q): want zero time, got %v", tt.input, got)
			}
		})
	}
}
