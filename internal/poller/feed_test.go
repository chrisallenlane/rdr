package poller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/mmcdole/gofeed"
)

func TestItemGUID(t *testing.T) {
	tests := []struct {
		name string
		item *gofeed.Item
		want string
	}{
		{
			name: "GUID set",
			item: &gofeed.Item{GUID: "abc-123", Link: "http://example.com", Title: "Title"},
			want: "abc-123",
		},
		{
			name: "empty GUID, Link set",
			item: &gofeed.Item{GUID: "", Link: "http://example.com/post", Title: "Title"},
			want: "http://example.com/post",
		},
		{
			name: "empty GUID and Link, Title set",
			item: &gofeed.Item{GUID: "", Link: "", Title: "My Title"},
			want: "My Title",
		},
		{
			name: "all empty",
			item: &gofeed.Item{GUID: "", Link: "", Title: "", Content: ""},
			want: func() string {
				h := sha256.Sum256([]byte(""))
				return fmt.Sprintf("%x", h)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemGUID(tt.item)
			if got != tt.want {
				t.Errorf("itemGUID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemPublishedAt(t *testing.T) {
	pub := time.Date(2024, 3, 10, 12, 0, 0, 0, time.UTC)
	upd := time.Date(2024, 3, 11, 8, 30, 0, 0, time.UTC)

	tests := []struct {
		name        string
		item        *gofeed.Item
		want        string
		checkRecent bool // when true, verify result parses and is within 1s of now
	}{
		{
			name: "PublishedParsed set",
			item: &gofeed.Item{PublishedParsed: &pub},
			want: model.FormatTime(pub),
		},
		{
			name: "only UpdatedParsed set",
			item: &gofeed.Item{UpdatedParsed: &upd},
			want: model.FormatTime(upd),
		},
		{
			name:        "neither set",
			item:        &gofeed.Item{},
			checkRecent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UTC()
			got := itemPublishedAt(tt.item)

			if tt.checkRecent {
				parsed, err := time.Parse(model.SQLiteDatetimeFmt, got)
				if err != nil {
					t.Fatalf("itemPublishedAt() returned unparseable time %q: %v", got, err)
				}
				after := time.Now().UTC()
				if parsed.Before(before.Add(-time.Second)) || parsed.After(after.Add(time.Second)) {
					t.Errorf(
						"itemPublishedAt() = %q, expected a time near now (%s..%s)",
						got, before.Format(time.RFC3339), after.Format(time.RFC3339),
					)
				}
			} else if got != tt.want {
				t.Errorf("itemPublishedAt() = %q, want %q", got, tt.want)
			}
		})
	}
}

// atomFeed returns a minimal Atom feed with the given items. Each item is a
// (guid, title, content, description) tuple.
func atomFeed(items []struct{ guid, title, content, description string }) string {
	out := `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Test Feed</title>
  <id>https://example.com/feed</id>
`
	for _, it := range items {
		out += fmt.Sprintf(`  <entry>
    <id>%s</id>
    <title>%s</title>
    <content>%s</content>
    <summary>%s</summary>
  </entry>
`, it.guid, it.title, it.content, it.description)
	}
	out += `</feed>`
	return out
}

func TestFetchAndStoreFeed_GUIDDedup(t *testing.T) {
	t.Run("duplicate GUIDs are not re-inserted", func(t *testing.T) {
		db := testutil.OpenTestDB(t)
		userID := testutil.InsertUser(t, db, "alice")

		feedBody := atomFeed([]struct{ guid, title, content, description string }{
			{"guid-1", "Item 1", "Content 1", ""},
			{"guid-2", "Item 2", "Content 2", ""},
		})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/atom+xml")
			if _, err := fmt.Fprint(w, feedBody); err != nil {
				http.Error(w, "write error", http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		res, err := db.Exec(
			`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
			userID, srv.URL, "Test Feed",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := res.LastInsertId()
		feed := &model.Feed{ID: feedID, UserID: userID, URL: srv.URL}

		// First fetch: should insert 2 items.
		if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
			t.Fatalf("first FetchAndStoreFeed: %v", err)
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE feed_id = ?`, feedID).Scan(&count); err != nil {
			t.Fatalf("counting items: %v", err)
		}
		if count != 2 {
			t.Fatalf("after first fetch: item count = %d, want 2", count)
		}

		// Second fetch: should not insert any new items.
		if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
			t.Fatalf("second FetchAndStoreFeed: %v", err)
		}

		if err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE feed_id = ?`, feedID).Scan(&count); err != nil {
			t.Fatalf("counting items: %v", err)
		}
		if count != 2 {
			t.Errorf("after second fetch: item count = %d, want 2 (no duplicates)", count)
		}
	})
}

func TestFetchAndStoreFeed_ContentFallback(t *testing.T) {
	t.Run("item content falls back to description", func(t *testing.T) {
		db := testutil.OpenTestDB(t)
		userID := testutil.InsertUser(t, db, "alice")

		// RSS feed where items have Description but no Content.
		rssFeed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <item>
      <guid>guid-desc-only</guid>
      <title>Item with description only</title>
      <description>This is the description text</description>
    </item>
  </channel>
</rss>`

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			if _, err := fmt.Fprint(w, rssFeed); err != nil {
				http.Error(w, "write error", http.StatusInternalServerError)
			}
		}))
		defer srv.Close()

		res, err := db.Exec(
			`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
			userID, srv.URL, "Test Feed",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := res.LastInsertId()
		feed := &model.Feed{ID: feedID, UserID: userID, URL: srv.URL}

		if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
			t.Fatalf("FetchAndStoreFeed: %v", err)
		}

		var content string
		if err := db.QueryRow(
			`SELECT content FROM items WHERE feed_id = ? AND guid = ?`,
			feedID, "guid-desc-only",
		).Scan(&content); err != nil {
			t.Fatalf("querying item content: %v", err)
		}
		if content != "This is the description text" {
			t.Errorf("content = %q, want %q", content, "This is the description text")
		}
	})
}
