package handler

import (
	"context"
	"net/http"
	"testing"
	"testing/fstest"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

const testBaseHTML = `<!DOCTYPE html>
<html data-theme="{{.Theme}}">
<body>
{{block "title" .}}rdr{{end}}
{{block "content" .}}{{end}}
</body>
</html>`

var testTemplateFS = fstest.MapFS{
	"templates/layout/base.html":       {Data: []byte(testBaseHTML)},
	"templates/pages/login.html":       {Data: []byte(`{{define "title"}}Login{{end}}{{define "content"}}login{{end}}`)},
	"templates/pages/register.html":    {Data: []byte(`{{define "title"}}Register{{end}}{{define "content"}}register{{end}}`)},
	"templates/pages/error.html":       {Data: []byte(`{{define "title"}}Error{{end}}{{define "content"}}{{.Content}}{{end}}`)},
	"templates/pages/items.html":       {Data: []byte(`{{define "title"}}Items{{end}}{{define "content"}}items{{end}}`)},
	"templates/pages/item.html":        {Data: []byte(`{{define "title"}}Item{{end}}{{define "content"}}item{{end}}`)},
	"templates/pages/feeds.html":       {Data: []byte(`{{define "title"}}Feeds{{end}}{{define "content"}}feeds{{end}}`)},
	"templates/pages/search.html":      {Data: []byte(`{{define "title"}}Search{{end}}{{define "content"}}search{{end}}`)},
	"templates/pages/lists.html":       {Data: []byte(`{{define "title"}}Lists{{end}}{{define "content"}}lists{{end}}`)},
	"templates/pages/list_detail.html": {Data: []byte(`{{define "title"}}List{{end}}{{define "content"}}list_detail{{end}}`)},

	// Fragment templates (standalone, no base layout).
	"templates/fragments/star_button.html":        {Data: []byte(`<form>{{if .Starred}}starred{{else}}unstarred{{end}}</form>`)},
	"templates/fragments/list_detail_feeds.html": {Data: []byte(`<section id="feeds-in-list">{{range .InList}}{{.ID}}{{end}}</section>`)},
	"templates/fragments/lists_table.html":       {Data: []byte(`<div id="lists-table">{{range .}}{{.Name}}{{end}}</div>`)},
	"templates/fragments/feeds_table.html":       {Data: []byte(`<div id="feeds-table">{{range .}}{{.URL}}{{end}}</div>`)},
	"templates/fragments/items_section.html":     {Data: []byte(`<section id="items-section">{{.Heading}}</section>`)},
	"templates/fragments/search_results.html":   {Data: []byte(`<div id="search-results">{{.Query}}</div>`)},
}

// newTestServer creates a *Server backed by an in-memory SQLite database and
// minimal stub templates suitable for handler tests. The database is
// automatically cleaned up when the test finishes. The feed resolver is
// set to a passthrough that returns the URL as-is (no network fetch).
func newTestServer(t *testing.T) *Server {
	t.Helper()
	db := testutil.OpenTestDB(t)
	s, err := NewServer(db, fstest.MapFS{}, testTemplateFS, t.TempDir())
	if err != nil {
		t.Fatalf("newTestServer: %v", err)
	}
	s.feedResolver = func(_ context.Context, u string) (string, error) { return u, nil }
	return s
}

// createTestUser inserts a test user with a bcrypt-hashed password and returns
// the user ID. The password "testpass1" is used by default.
func createTestUser(t *testing.T, s *Server, username, password string) int64 {
	t.Helper()
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	result, err := s.db.Exec(
		"INSERT INTO users (username, password) VALUES (?, ?)",
		username, hash,
	)
	if err != nil {
		t.Fatalf("inserting test user: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

// createTestSession inserts a session row and returns the session ID string.
func createTestSession(t *testing.T, s *Server, userID int64) string {
	t.Helper()
	sessionID, err := generateRandomHex(32)
	if err != nil {
		t.Fatalf("generateRandomHex: %v", err)
	}
	expiresAt := "2099-12-31 00:00:00"
	if _, err := s.db.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		sessionID, userID, expiresAt,
	); err != nil {
		t.Fatalf("inserting test session: %v", err)
	}
	return sessionID
}

// authedRequest creates an HTTP request with a valid session cookie for the
// given user. It attaches the user to the request context so handlers that
// call middleware.UserFromContext work correctly.
func authedRequest(t *testing.T, s *Server, userID int64, method, target string) *http.Request {
	t.Helper()
	sessionID := createTestSession(t, s, userID)

	var username string
	if err := s.db.QueryRow("SELECT username FROM users WHERE id = ?", userID).Scan(&username); err != nil {
		t.Fatalf("querying username: %v", err)
	}

	req, _ := http.NewRequest(method, target, nil)
	req.AddCookie(&http.Cookie{Name: "rdr_session", Value: sessionID})
	ctx := middleware.ContextWithUser(req.Context(), &model.User{ID: userID, Username: username})
	return req.WithContext(ctx)
}
