package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// freshTokenForUser creates a user and an associated token, returning the
// db + user id + raw token string.
func freshTokenForUser(t *testing.T, username string) (*sql.DB, int64, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, username)
	raw, _, err := token.Generate(db, uid, "test", time.Time{})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return db, uid, raw
}

func TestGetMe_ValidToken(t *testing.T) {
	db, uid, raw := freshTokenForUser(t, "alice")
	h := New(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%q", rec.Code, rec.Body.String())
	}

	var u User
	if err := json.Unmarshal(rec.Body.Bytes(), &u); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("username: got %q, want alice", u.Username)
	}
	if u.Id != uid {
		t.Errorf("id: got %d, want %d", u.Id, uid)
	}
}

func TestBearerAuth_RejectsMissingHeader(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type: got %q, want application/problem+json", got)
	}
}

func TestBearerAuth_RejectsMalformedHeader(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(db)

	cases := []string{
		"token123",           // no scheme
		"Basic dGVzdDp0ZXN0", // wrong scheme
		"Bearer ",            // empty token
		"Bearer\tfoo",        // missing space
	}
	for _, hdr := range cases {
		t.Run(hdr, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			req.Header.Set("Authorization", hdr)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status: got %d, want 401 for header %q", rec.Code, hdr)
			}
		})
	}
}

func TestBearerAuth_RejectsUnknownToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+token.Prefix+strings.Repeat("a", 64))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestBearerAuth_RejectsExpiredToken(t *testing.T) {
	db := testutil.OpenTestDB(t)
	uid := testutil.InsertUser(t, db, "alice")
	raw, _, err := token.Generate(db, uid, "expired", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	h := New(db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
}

func TestBearerAuth_PublicPathsBypass(t *testing.T) {
	db := testutil.OpenTestDB(t)
	h := New(db)

	for _, path := range []string{
		"/api/v1/healthz",
		"/api/openapi.yaml",
		"/api/openapi.json",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status: got %d, want 200 (public path %q)", rec.Code, path)
			}
		})
	}
}
