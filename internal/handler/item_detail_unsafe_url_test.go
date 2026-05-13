package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/chrisallenlane/rdr/internal/background"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

// itemTemplateWithURLHref is a stub template that mirrors the production
// templates/pages/item.html line 5 — it renders {{.Content.Item.URL}}
// directly into an href attribute. The default test template stub
// (in testhelpers_test.go) renders only the literal string "item" and
// would mask any XSS via href.
const itemTemplateWithURLHref = `{{define "title"}}Item{{end}}` +
	`{{define "content"}}` +
	`<h1>{{if .Content.Item.URL}}` +
	`<a href="{{.Content.Item.URL}}" target="_blank" rel="nofollow noopener">` +
	`{{.Content.Item.Title}}</a>` +
	`{{else}}{{.Content.Item.Title}}{{end}}</h1>` +
	`<div>{{.Content.Content}}</div>` +
	`{{end}}`

// newTestServerWithRealItemHref creates a test server whose item.html
// template renders Item.URL into an href attribute, matching production.
func newTestServerWithRealItemHref(t *testing.T) *Server {
	t.Helper()
	tplFS := fstest.MapFS{}
	for k, v := range testTemplateFS {
		tplFS[k] = v
	}
	tplFS["templates/pages/item.html"] = &fstest.MapFile{
		Data: []byte(itemTemplateWithURLHref),
	}
	db := testutil.OpenTestDB(t)
	s, err := NewServer(context.Background(), &background.Group{}, db, fstest.MapFS{}, tplFS, t.TempDir())
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.feedResolver = func(_ context.Context, u string) (string, error) {
		return u, nil
	}
	return s
}

// TestItemDetail_DangerousURLSchemes verifies that when an item's URL
// column contains a dangerous scheme (javascript:, data:, file:,
// vbscript:), the rendered page does NOT contain an executable href
// with that scheme.
//
// CONTEXT — the assessor hypothesised that html/template's contextual
// escaping does not block javascript: in href context, making this an
// XSS vector. That hypothesis is INVALIDATED here: html/template DOES
// substitute "#ZgotmplZ" for non-allow-listed URL schemes in href
// position. This test pins that defense and serves as a regression
// guard. If a future change moves to a templating layer without the
// same protection — or wraps Item.URL in template.URL / template.HTML —
// this test will fail and surface the issue.
//
// Threat-model note: per the project memory, the deployment is
// homelab/single-user. The realistic attacker is a malicious feed
// publisher. XSS in this context is bounded (the victim is the
// operator viewing their own reader) but real — session theft is
// trivial since there is no CSRF token. So while the test today
// confirms safety, the underlying ingest-time validation gap
// (resolveLink accepts these schemes verbatim) is still a
// defense-in-depth concern. See
// internal/poller/feed_test.go TestResolveLink for current behavior.
func TestItemDetail_DangerousURLSchemes(t *testing.T) {
	cases := []struct {
		name, url string
	}{
		{"javascript", "javascript:alert('xss')"},
		{"javascript mixed case", "JaVaScRiPt:alert(1)"},
		{"javascript with tab", "java\tscript:alert(1)"},
		{"data html", "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg=="},
		{"file local", "file:///etc/passwd"},
		{"vbscript", "vbscript:msgbox(1)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServerWithRealItemHref(t)
			userID := createTestUser(t, s, "alice", "testpass1")

			// Insert a feed and one item whose URL is the dangerous scheme.
			res, err := s.db.Exec(
				"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
				userID, "https://feed.example.com/", "Test Feed",
			)
			if err != nil {
				t.Fatalf("insert feed: %v", err)
			}
			feedID, _ := res.LastInsertId()
			res, err = s.db.Exec(
				`INSERT INTO items (feed_id, guid, title, url, published_at)
				 VALUES (?, ?, ?, ?, ?)`,
				feedID, "g1", "Pwn", tc.url, "2024-01-01 00:00:00",
			)
			if err != nil {
				t.Fatalf("insert item: %v", err)
			}
			itemID, _ := res.LastInsertId()

			req := authedRequest(
				t, s, userID, http.MethodGet,
				fmt.Sprintf("/items/%d", itemID),
			)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			// The rendered href must NOT be the raw dangerous URL.
			needle := fmt.Sprintf(`href="%s"`, tc.url)
			if strings.Contains(body, needle) {
				t.Errorf(
					"rendered body contains executable href %q\nbody:\n%s",
					needle, body,
				)
			}
			// Sanity: lowercased scheme literal must not appear inside an
			// href attribute value.
			lower := strings.ToLower(body)
			scheme := strings.SplitN(strings.ToLower(tc.url), ":", 2)[0]
			// "mailto:" is fine — but the cases above are not mailto.
			bad := fmt.Sprintf(`href="%s:`, scheme)
			if strings.Contains(lower, bad) {
				t.Errorf(
					"body contains href starting with %q (raw scheme leaked)\nbody:\n%s",
					bad, body,
				)
			}
		})
	}
}

// TestItemDetail_BenignSchemesPassThrough pins the fact that benign
// schemes like mailto: and https: are rendered as-is. This guards
// against an over-aggressive future fix that strips legitimate links.
func TestItemDetail_BenignSchemesPassThrough(t *testing.T) {
	cases := []struct {
		name, url string
	}{
		{"https", "https://example.com/post"},
		{"http", "http://example.com/post"},
		{"mailto", "mailto:author@example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServerWithRealItemHref(t)
			userID := createTestUser(t, s, "alice", "testpass1")

			res, err := s.db.Exec(
				"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
				userID, "https://feed.example.com/", "Test Feed",
			)
			if err != nil {
				t.Fatalf("insert feed: %v", err)
			}
			feedID, _ := res.LastInsertId()
			res, err = s.db.Exec(
				`INSERT INTO items (feed_id, guid, title, url, published_at)
				 VALUES (?, ?, ?, ?, ?)`,
				feedID, "g1", "Hello", tc.url, "2024-01-01 00:00:00",
			)
			if err != nil {
				t.Fatalf("insert item: %v", err)
			}
			itemID, _ := res.LastInsertId()

			req := authedRequest(
				t, s, userID, http.MethodGet,
				fmt.Sprintf("/items/%d", itemID),
			)
			rec := httptest.NewRecorder()
			s.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			needle := fmt.Sprintf(`href="%s"`, tc.url)
			if !strings.Contains(body, needle) {
				t.Errorf(
					"rendered body does NOT contain expected href %q\nbody:\n%s",
					needle, body,
				)
			}
		})
	}
}
