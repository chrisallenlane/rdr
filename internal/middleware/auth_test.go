package middleware

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

// okHandler is a minimal next handler that records it was reached.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

// TestUserFromContext_NoUser verifies that UserFromContext returns nil when no
// user has been stored in the context.
func TestUserFromContext_NoUser(t *testing.T) {
	ctx := context.Background()
	if got := UserFromContext(ctx); got != nil {
		t.Errorf("UserFromContext(empty context) = %v, want nil", got)
	}
}

func TestIsSecureRequest(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*http.Request)
		expected bool
	}{
		{
			name:     "plain HTTP returns false",
			setup:    func(*http.Request) {},
			expected: false,
		},
		{
			name: "direct TLS returns true",
			setup: func(r *http.Request) {
				r.TLS = &tls.ConnectionState{}
			},
			expected: true,
		},
		{
			name: "X-Forwarded-Proto: https returns true",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "https")
			},
			expected: true,
		},
		{
			name: "X-Forwarded-Proto: http returns false",
			setup: func(r *http.Request) {
				r.Header.Set("X-Forwarded-Proto", "http")
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			tt.setup(req)
			if got := IsSecureRequest(req); got != tt.expected {
				t.Errorf("IsSecureRequest() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestContextWithUser_RoundTrip verifies that a user stored with
// ContextWithUser can be retrieved with UserFromContext.
func TestContextWithUser_RoundTrip(t *testing.T) {
	want := &model.User{ID: 42, Username: "alice"}
	ctx := ContextWithUser(context.Background(), want)

	got := UserFromContext(ctx)
	if got == nil {
		t.Fatal("UserFromContext returned nil, want non-nil")
	}
	if got.ID != want.ID {
		t.Errorf("User.ID = %d, want %d", got.ID, want.ID)
	}
	if got.Username != want.Username {
		t.Errorf("User.Username = %q, want %q", got.Username, want.Username)
	}
}

// TestRequireAuth_NoCookie verifies that a request without the rdr_session
// cookie is redirected to /login.
func TestRequireAuth_NoCookie(t *testing.T) {
	db := testutil.OpenTestDB(t)

	reached := false
	handler := RequireAuth(db)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("next handler was called, want redirect")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}
}

// TestRequireAuth_EmptyCookie verifies that an empty rdr_session cookie value
// is treated as unauthenticated.
func TestRequireAuth_EmptyCookie(t *testing.T) {
	db := testutil.OpenTestDB(t)

	reached := false
	handler := RequireAuth(db)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: ""})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("next handler was called, want redirect")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}
}

// TestRequireAuth_InvalidSession verifies that a cookie whose session ID does
// not exist in the database is redirected to /login and the cookie is cleared.
func TestRequireAuth_InvalidSession(t *testing.T) {
	db := testutil.OpenTestDB(t)

	reached := false
	handler := RequireAuth(db)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: "nonexistent-session-id"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("next handler was called, want redirect")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}

	// The middleware should clear the cookie with empty value and root path.
	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_session" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("expected rdr_session Set-Cookie header")
	}
	if found.MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want < 0", found.MaxAge)
	}
	if found.Value != "" {
		t.Errorf("cookie Value = %q, want empty", found.Value)
	}
	if found.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", found.Path, "/")
	}
}

// TestRequireAuth_ExpiredSession verifies that a cookie with an expired
// session is redirected to /login.
func TestRequireAuth_ExpiredSession(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	testutil.InsertSession(
		t, db, userID, "expired-session",
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	)

	reached := false
	handler := RequireAuth(db)(okHandler(&reached))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: "expired-session"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if reached {
		t.Error("next handler was called for expired session, want redirect")
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want %q", loc, "/login")
	}

	// The middleware should clear the cookie with empty value and root path.
	var found2 *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_session" {
			found2 = c
			break
		}
	}
	if found2 == nil {
		t.Fatal("expected rdr_session Set-Cookie header")
	}
	if found2.MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want < 0", found2.MaxAge)
	}
	if found2.Value != "" {
		t.Errorf("cookie Value = %q, want empty", found2.Value)
	}
	if found2.Path != "/" {
		t.Errorf("cookie Path = %q, want %q", found2.Path, "/")
	}
}

// TestRequireAuth_WrongSessionID verifies that a valid, non-expired session
// belonging to another user does not authenticate the request. This ensures
// the WHERE clause checks s.id (not just s.expires_at).
func TestRequireAuth_WrongSessionID(t *testing.T) {
	db := testutil.OpenTestDB(t)

	// Create two users, each with their own session.
	aliceID := testutil.InsertUser(t, db, "alice")
	testutil.InsertSession(t, db, aliceID, "alice-session",
		time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC))

	bobID := testutil.InsertUser(t, db, "bobbyboy")
	testutil.InsertSession(t, db, bobID, "bob-session",
		time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC))

	// Send bob's session ID and verify we get bob (not alice).
	// If the WHERE clause doesn't filter by s.id, the query returns the
	// first non-expired session (alice), which would be wrong.
	var contextUser *model.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(db)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: "bob-session"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if contextUser == nil {
		t.Fatal("UserFromContext returned nil")
	}
	if contextUser.ID != bobID {
		t.Errorf("User.ID = %d, want bob's ID %d", contextUser.ID, bobID)
	}
	if contextUser.Username != "bobbyboy" {
		t.Errorf("User.Username = %q, want %q", contextUser.Username, "bobbyboy")
	}
}

// TestRequireAuth_ValidSession verifies that a request with a valid session
// passes through to the next handler and the user is stored in the context.
func TestRequireAuth_ValidSession(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	testutil.InsertSession(
		t, db, userID, "valid-session",
		time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC),
	)

	var contextUser *model.User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contextUser = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := RequireAuth(db)(next)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: "valid-session"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if contextUser == nil {
		t.Fatal("UserFromContext returned nil, want a valid user")
	}
	if contextUser.ID != userID {
		t.Errorf("User.ID = %d, want %d", contextUser.ID, userID)
	}
	if contextUser.Username != "alice" {
		t.Errorf("User.Username = %q, want %q", contextUser.Username, "alice")
	}
}
