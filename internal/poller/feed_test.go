package poller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFetchAndStoreFeed_MediaRSS(t *testing.T) {
	tests := []struct {
		name         string
		guid         string
		feedTitle    string
		rssFeed      string
		contentMust  []string
		contentMustN []string
		wantDesc     string
	}{
		{
			name:      "YouTube — Flash content uses thumbnail fallback",
			guid:      "yt-item-1",
			feedTitle: "YouTube Channel",
			rssFeed: `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>YouTube Channel</title>
    <link>https://www.youtube.com/channel/UC1</link>
    <item>
      <guid>yt-item-1</guid>
      <title>Rick Astley - Never Gonna Give You Up</title>
      <link>https://www.youtube.com/watch?v=dQw4w9WgXcQ</link>
      <media:group>
        <media:content url="https://www.youtube.com/v/dQw4w9WgXcQ" type="application/x-shockwave-flash" width="640" height="390"/>
        <media:thumbnail url="https://i.ytimg.com/vi/dQw4w9WgXcQ/hqdefault.jpg" width="480" height="360"/>
        <media:description>Never gonna give you up

Rick Astley classic.</media:description>
        <media:title>Rick Astley - Never Gonna Give You Up</media:title>
      </media:group>
    </item>
  </channel>
</rss>`,
			contentMust:  []string{"<a href=", "<img src=", "hqdefault.jpg", "Never gonna give you up"},
			contentMustN: []string{"<video", "<audio"},
			wantDesc:     "Never gonna give you up\n\nRick Astley classic.",
		},
		{
			name:      "Podcast — audio enclosure",
			guid:      "pod-ep1",
			feedTitle: "My Podcast",
			rssFeed: `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>My Podcast</title>
    <link>https://podcast.example.com</link>
    <item>
      <guid>pod-ep1</guid>
      <title>Episode 1</title>
      <link>https://podcast.example.com/episode/1</link>
      <enclosure url="https://podcast.example.com/ep1.mp3" type="audio/mpeg" length="1234567"/>
    </item>
  </channel>
</rss>`,
			contentMust: []string{"<audio", "controls", "ep1.mp3"},
		},
		{
			name:      "Vimeo — video/mp4 media:content",
			guid:      "vimeo-123456",
			feedTitle: "Vimeo Channel",
			rssFeed: `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>Vimeo Channel</title>
    <link>https://vimeo.com/channel/1</link>
    <item>
      <guid>vimeo-123456</guid>
      <title>Short Film</title>
      <link>https://vimeo.com/123456</link>
      <media:group>
        <media:content url="https://vimeo.com/external/123456.mp4" type="video/mp4" width="1280" height="720"/>
        <media:description>A short film.</media:description>
      </media:group>
    </item>
  </channel>
</rss>`,
			contentMust: []string{"<video", "controls", "123456.mp4", "A short film."},
			wantDesc:    "A short film.",
		},
		{
			name:      "standard fields win over media:group",
			guid:      "normal-item-1",
			feedTitle: "Normal Feed",
			rssFeed: `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/">
  <channel>
    <title>Normal Feed</title>
    <link>https://example.com</link>
    <item>
      <guid>normal-item-1</guid>
      <title>Normal Item</title>
      <link>https://example.com/normal</link>
      <description>Standard description text</description>
      <media:group>
        <media:thumbnail url="https://example.com/thumb.jpg"/>
        <media:description>Media description that should be ignored</media:description>
      </media:group>
    </item>
  </channel>
</rss>`,
			contentMust:  []string{"Standard description text"},
			contentMustN: []string{"Media description that should be ignored"},
			wantDesc:     "Standard description text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := testutil.OpenTestDB(t)
			userID := testutil.InsertUser(t, db, "alice")

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/rss+xml")
				if _, err := fmt.Fprint(w, tt.rssFeed); err != nil {
					http.Error(w, "write error", http.StatusInternalServerError)
				}
			}))
			defer srv.Close()

			res, err := db.Exec(
				`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
				userID, srv.URL, tt.feedTitle,
			)
			if err != nil {
				t.Fatalf("inserting feed: %v", err)
			}
			feedID, _ := res.LastInsertId()
			feed := &model.Feed{ID: feedID, UserID: userID, URL: srv.URL}

			if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
				t.Fatalf("FetchAndStoreFeed: %v", err)
			}

			var content, description string
			if err := db.QueryRow(
				`SELECT content, description FROM items WHERE feed_id = ? AND guid = ?`,
				feedID, tt.guid,
			).Scan(&content, &description); err != nil {
				t.Fatalf("querying item: %v", err)
			}

			for _, want := range tt.contentMust {
				if !strings.Contains(content, want) {
					t.Errorf("content missing %q; got %q", want, content)
				}
			}
			for _, unwanted := range tt.contentMustN {
				if strings.Contains(content, unwanted) {
					t.Errorf("content must not contain %q; got %q", unwanted, content)
				}
			}
			if tt.wantDesc != "" && description != tt.wantDesc {
				t.Errorf("description = %q, want %q", description, tt.wantDesc)
			}
		})
	}
}
