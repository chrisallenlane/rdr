package handler

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chrisallenlane/rdr/internal/favicon"
	"github.com/chrisallenlane/rdr/internal/model"
)

func TestRenderFragment(t *testing.T) {
	t.Run("renders fragment without base layout", func(t *testing.T) {
		s := newTestServer(t)
		// Manually add a fragment template.
		tmpl := template.Must(template.New("test.html").Parse(`<div id="test">{{.Name}}</div>`))
		s.templates["fragments/test.html"] = tmpl

		rec := httptest.NewRecorder()
		s.renderFragment(rec, "test.html", struct{ Name string }{"hello"})

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
		}
		body := rec.Body.String()
		if body != `<div id="test">hello</div>` {
			t.Errorf("body = %q, want %q", body, `<div id="test">hello</div>`)
		}
	})

	t.Run("unknown fragment returns 500", func(t *testing.T) {
		s := newTestServer(t)
		rec := httptest.NewRecorder()
		s.renderFragment(rec, "nonexistent.html", nil)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestParseTemplatesWithFragments(t *testing.T) {
	fs := fstest.MapFS{
		"templates/layout/base.html":    {Data: []byte(testBaseHTML)},
		"templates/pages/test.html":     {Data: []byte(`{{define "title"}}T{{end}}{{define "content"}}c{{end}}`)},
		"templates/fragments/frag.html": {Data: []byte(`<p>{{.}}</p>`)},
	}

	s := &Server{
		mux:         http.NewServeMux(),
		faviconsDir: t.TempDir(),
	}
	if err := s.parseTemplates(fs); err != nil {
		t.Fatalf("parseTemplates: %v", err)
	}

	if _, ok := s.templates["test.html"]; !ok {
		t.Error("page template not found")
	}
	if _, ok := s.templates["fragments/frag.html"]; !ok {
		t.Error("fragment template not found")
	}
}

func TestParseTemplatesWithoutFragments(t *testing.T) {
	fs := fstest.MapFS{
		"templates/layout/base.html": {Data: []byte(testBaseHTML)},
		"templates/pages/test.html":  {Data: []byte(`{{define "title"}}T{{end}}{{define "content"}}c{{end}}`)},
	}

	s := &Server{
		mux:         http.NewServeMux(),
		faviconsDir: t.TempDir(),
	}
	if err := s.parseTemplates(fs); err != nil {
		t.Fatalf("parseTemplates without fragments dir should not error: %v", err)
	}
}

func TestSetHTMXTriggers(t *testing.T) {
	t.Run("single event", func(t *testing.T) {
		rec := httptest.NewRecorder()
		setHTMXTriggers(rec, htmxTriggers{"showFlash": "Hello"})
		got := rec.Header().Get("HX-Trigger")
		if got != `{"showFlash":"Hello"}` {
			t.Errorf("HX-Trigger = %q, want %q", got, `{"showFlash":"Hello"}`)
		}
	})

	t.Run("non-ASCII escaped to unicode", func(t *testing.T) {
		rec := httptest.NewRecorder()
		setHTMXTriggers(rec, htmxTriggers{"showFlash": "List café renamed."})
		got := rec.Header().Get("HX-Trigger")
		// Verify the header is pure ASCII.
		for _, b := range []byte(got) {
			if b > 127 {
				t.Fatalf("header contains non-ASCII byte %x in %q", b, got)
			}
		}
		// Verify it round-trips back correctly.
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if m["showFlash"] != "List café renamed." {
			t.Errorf("showFlash = %v, want 'List café renamed.'", m["showFlash"])
		}
	})

	t.Run("multiple events", func(t *testing.T) {
		rec := httptest.NewRecorder()
		setHTMXTriggers(rec, htmxTriggers{
			"showFlash":    "Renamed.",
			"setPageTitle": "New",
		})
		got := rec.Header().Get("HX-Trigger")
		// JSON key order is non-deterministic; parse and check.
		var m map[string]any
		if err := json.Unmarshal([]byte(got), &m); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if m["showFlash"] != "Renamed." {
			t.Errorf("showFlash = %v, want Renamed.", m["showFlash"])
		}
		if m["setPageTitle"] != "New" {
			t.Errorf("setPageTitle = %v, want New", m["setPageTitle"])
		}
	})
}

func TestFlash(t *testing.T) {
	t.Run("HTMX request uses HX-Trigger", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set("HX-Request", "true")
		flash(rec, r, "Done!")
		if got := rec.Header().Get("HX-Trigger"); got == "" {
			t.Error("expected HX-Trigger header to be set")
		}
		// Should NOT set a cookie.
		if cookies := rec.Result().Cookies(); len(cookies) > 0 {
			t.Error("expected no cookies for HTMX request")
		}
	})

	t.Run("normal request uses cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", nil)
		flash(rec, r, "Done!")
		if got := rec.Header().Get("HX-Trigger"); got != "" {
			t.Error("expected no HX-Trigger header for normal request")
		}
		cookies := rec.Result().Cookies()
		found := false
		for _, c := range cookies {
			if c.Name == "rdr_flash" && c.Value == "Done!" {
				found = true
			}
		}
		if !found {
			t.Error("expected rdr_flash cookie to be set")
		}
	})
}

func TestTemplateFuncMap(t *testing.T) {
	fm := templateFuncMap(t.TempDir())

	t.Run("add", func(t *testing.T) {
		add := fm["add"].(func(int, int) int)
		if got := add(3, 4); got != 7 {
			t.Errorf("add(3, 4) = %d, want 7", got)
		}
	})

	t.Run("subtract", func(t *testing.T) {
		sub := fm["subtract"].(func(int, int) int)
		if got := sub(10, 3); got != 7 {
			t.Errorf("subtract(10, 3) = %d, want 7", got)
		}
	})

	t.Run("deref non-nil", func(t *testing.T) {
		deref := fm["deref"].(func(*int64) int64)
		v := int64(42)
		if got := deref(&v); got != 42 {
			t.Errorf("deref(&42) = %d, want 42", got)
		}
	})

	t.Run("deref nil", func(t *testing.T) {
		deref := fm["deref"].(func(*int64) int64)
		if got := deref(nil); got != 0 {
			t.Errorf("deref(nil) = %d, want 0", got)
		}
	})

	t.Run("feedName with title", func(t *testing.T) {
		feedName := fm["feedName"].(func(model.Feed) string)
		f := model.Feed{Title: "My Feed", URL: "https://example.com"}
		if got := feedName(f); got != "My Feed" {
			t.Errorf("feedName = %q, want %q", got, "My Feed")
		}
	})

	t.Run("feedName without title", func(t *testing.T) {
		feedName := fm["feedName"].(func(model.Feed) string)
		f := model.Feed{URL: "https://example.com"}
		if got := feedName(f); got != "https://example.com" {
			t.Errorf("feedName = %q, want %q", got, "https://example.com")
		}
	})

	t.Run("faviconSlug with site URL", func(t *testing.T) {
		faviconSlug := fm["faviconSlug"].(func(model.Item) string)
		item := model.Item{FeedSiteURL: "https://www.schneier.com", FeedURL: "https://feeds.example.com/rss"}
		got := faviconSlug(item)
		want := favicon.Slug("https://www.schneier.com", "https://feeds.example.com/rss")
		if got != want || got == "" {
			t.Errorf("faviconSlug = %q, want %q (non-empty)", got, want)
		}
	})

	t.Run("faviconSlug falls back to feed URL", func(t *testing.T) {
		faviconSlug := fm["faviconSlug"].(func(model.Item) string)
		item := model.Item{FeedSiteURL: "", FeedURL: "https://example.com/feed.xml"}
		got := faviconSlug(item)
		want := favicon.Slug("", "https://example.com/feed.xml")
		if got != want || got == "" {
			t.Errorf("faviconSlug = %q, want %q (non-empty)", got, want)
		}
	})

	t.Run("faviconSlug empty when no URLs", func(t *testing.T) {
		faviconSlug := fm["faviconSlug"].(func(model.Item) string)
		item := model.Item{}
		if got := faviconSlug(item); got != "" {
			t.Errorf("faviconSlug = %q, want %q", got, "")
		}
	})

	t.Run("itemFeedName with title", func(t *testing.T) {
		itemFeedName := fm["itemFeedName"].(func(model.Item) string)
		item := model.Item{FeedTitle: "Schneier on Security", FeedSiteURL: "https://www.schneier.com"}
		if got := itemFeedName(item); got != "Schneier on Security" {
			t.Errorf("itemFeedName = %q, want %q", got, "Schneier on Security")
		}
	})

	t.Run("itemFeedName falls back to site domain", func(t *testing.T) {
		itemFeedName := fm["itemFeedName"].(func(model.Item) string)
		item := model.Item{FeedTitle: "", FeedSiteURL: "https://danluu.com", FeedURL: "https://danluu.com/atom.xml"}
		if got := itemFeedName(item); got != "danluu.com" {
			t.Errorf("itemFeedName = %q, want %q", got, "danluu.com")
		}
	})

	t.Run("itemFeedName falls back to feed URL domain", func(t *testing.T) {
		itemFeedName := fm["itemFeedName"].(func(model.Item) string)
		item := model.Item{FeedTitle: "", FeedSiteURL: "", FeedURL: "https://feeds.example.com/rss"}
		if got := itemFeedName(item); got != "feeds.example.com" {
			t.Errorf("itemFeedName = %q, want %q", got, "feeds.example.com")
		}
	})

	t.Run("itemFeedName empty when all empty", func(t *testing.T) {
		itemFeedName := fm["itemFeedName"].(func(model.Item) string)
		item := model.Item{}
		if got := itemFeedName(item); got != "" {
			t.Errorf("itemFeedName = %q, want %q", got, "")
		}
	})
}

func TestHandleIndex(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	req := authedRequest(t, s, userID, http.MethodGet, "/")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if loc := rec.Header().Get("Location"); loc != "/items" {
		t.Errorf("Location = %q, want %q", loc, "/items")
	}
}

func TestFormatDate_Relative(t *testing.T) {
	now := time.Now()

	thirtyDaysAgo := now.Add(-30 * 24 * time.Hour)
	expectedDate := thirtyDaysAgo.Format("Jan 2, 2006")

	nilPtr := (*time.Time)(nil)

	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "30 seconds ago",
			input: now.Add(-30 * time.Second),
			want:  "just now",
		},
		{
			name:  "exactly 1 minute ago",
			input: now.Add(-time.Minute),
			want:  "1m ago",
		},
		{
			name:  "5 minutes ago",
			input: now.Add(-5 * time.Minute),
			want:  "5m ago",
		},
		{
			name:  "exactly 1 hour ago",
			input: now.Add(-time.Hour),
			want:  "1h ago",
		},
		{
			name:  "3 hours ago",
			input: now.Add(-3 * time.Hour),
			want:  "3h ago",
		},
		{
			name:  "exactly 24 hours ago",
			input: now.Add(-24 * time.Hour),
			want:  "1d ago",
		},
		{
			name:  "2 days ago",
			input: now.Add(-2 * 24 * time.Hour),
			want:  "2d ago",
		},
		{
			name:  "exactly 7 days ago",
			input: now.Add(-7 * 24 * time.Hour),
			want:  now.Add(-7 * 24 * time.Hour).Format("Jan 2, 2006"),
		},
		{
			name:  "30 days ago",
			input: thirtyDaysAgo,
			want:  expectedDate,
		},
		{
			name:  "nil *time.Time",
			input: nilPtr,
			want:  "",
		},
		{
			name:  "non-time type",
			input: "hello",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDate(tt.input, false)
			if got != tt.want {
				t.Errorf("formatDate(%v, false) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
