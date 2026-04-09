package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
)

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
		if got := faviconSlug(item); got != "www-schneier-com" {
			t.Errorf("faviconSlug = %q, want %q", got, "www-schneier-com")
		}
	})

	t.Run("faviconSlug falls back to feed URL", func(t *testing.T) {
		faviconSlug := fm["faviconSlug"].(func(model.Item) string)
		item := model.Item{FeedSiteURL: "", FeedURL: "https://example.com/feed.xml"}
		if got := faviconSlug(item); got != "example-com" {
			t.Errorf("faviconSlug = %q, want %q", got, "example-com")
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

func TestTimeAgo(t *testing.T) {
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
			got := timeAgo(tt.input)
			if got != tt.want {
				t.Errorf("timeAgo(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
