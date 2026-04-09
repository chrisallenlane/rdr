package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chrisallenlane/rdr/internal/model"
)

func TestItemsHeading(t *testing.T) {
	feeds := []model.Feed{
		{ID: 1, Title: "Go Blog"},
		{ID: 2, Title: "Rust Blog"},
	}
	lists := []model.List{
		{ID: 10, Name: "Tech"},
		{ID: 20, Name: "News"},
	}

	tests := []struct {
		name       string
		filterFeed int64
		filterList int64
		want       string
	}{
		{
			name: "no filters",
			want: "All Items",
		},
		{
			name:       "filter by existing feed",
			filterFeed: 2,
			want:       "Rust Blog",
		},
		{
			name:       "filter by non-existent feed",
			filterFeed: 999,
			want:       "All Items",
		},
		{
			name:       "filter by existing list",
			filterList: 10,
			want:       "List: Tech",
		},
		{
			name:       "filter by non-existent list",
			filterList: 999,
			want:       "All Items",
		},
		{
			name:       "feed filter takes precedence over list",
			filterFeed: 1,
			filterList: 10,
			want:       "Go Blog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := itemsHeading(tt.filterFeed, tt.filterList, feeds, lists)
			if got != tt.want {
				t.Errorf("itemsHeading() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHandleItems(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Insert a feed and an item so the handler has data to render.
	if _, err := s.db.Exec(
		"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
		1, userID, "https://example.com/feed", "Test Feed",
	); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO items (id, feed_id, guid, title, read, published_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		1, 1, "guid1", "Test Item", 0, "2024-01-01 00:00:00",
	); err != nil {
		t.Fatalf("inserting item: %v", err)
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/items")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestBuildItemFilter(t *testing.T) {
	tests := []struct {
		name         string
		userID       int64
		feedID       int64
		listID       int64
		unread       bool
		starred      bool
		wantContains []string
		wantArgCount int
	}{
		{
			name:         "base case",
			userID:       1,
			feedID:       0,
			listID:       0,
			unread:       false,
			starred:      false,
			wantContains: []string{"f.user_id = ?"},
			wantArgCount: 1,
		},
		{
			name:         "with feedID",
			userID:       1,
			feedID:       5,
			listID:       0,
			unread:       false,
			starred:      false,
			wantContains: []string{"f.user_id = ?", "f.id = ?"},
			wantArgCount: 2,
		},
		{
			name:         "with unread",
			userID:       1,
			feedID:       0,
			listID:       0,
			unread:       true,
			starred:      false,
			wantContains: []string{"f.user_id = ?", "i.read = 0"},
			wantArgCount: 1,
		},
		{
			name:         "with starred",
			userID:       1,
			feedID:       0,
			listID:       0,
			unread:       false,
			starred:      true,
			wantContains: []string{"f.user_id = ?", "i.starred = 1"},
			wantArgCount: 1,
		},
		{
			name:         "with listID",
			userID:       1,
			feedID:       0,
			listID:       3,
			unread:       false,
			starred:      false,
			wantContains: []string{"f.user_id = ?", "list_feeds", "lists"},
			// userID + filterList + filterList + userID = 4 args
			wantArgCount: 4,
		},
		{
			name:    "combined filters",
			userID:  2,
			feedID:  7,
			listID:  4,
			unread:  true,
			starred: true,
			wantContains: []string{
				"f.user_id = ?",
				"f.id = ?",
				"i.read = 0",
				"i.starred = 1",
				"list_feeds",
			},
			// userID + feedID + filterList + filterList + userID = 5 args
			wantArgCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := buildItemFilter(
				tt.userID, tt.feedID, tt.listID, tt.unread, tt.starred,
			)

			for _, substr := range tt.wantContains {
				if !strings.Contains(where, substr) {
					t.Errorf("WHERE clause %q does not contain %q", where, substr)
				}
			}

			if len(args) != tt.wantArgCount {
				t.Errorf(
					"arg count = %d, want %d (args=%v)",
					len(args), tt.wantArgCount, args,
				)
			}
		})
	}
}

func TestHandleMarkRead(t *testing.T) {
	t.Run("mark all read", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			1, userID, "https://example.com/feed", "Test Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		for i, guid := range []string{"guid1", "guid2"} {
			if _, err := s.db.Exec(
				`INSERT INTO items (id, feed_id, guid, title, read, published_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				i+1, 1, guid, "Item", 0, "2024-01-01 00:00:00",
			); err != nil {
				t.Fatalf("inserting item %d: %v", i+1, err)
			}
		}

		form := url.Values{}
		req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		var unreadCount int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM items i
			 JOIN feeds f ON i.feed_id = f.id
			 WHERE f.user_id = ? AND i.read = 0`,
			userID,
		).Scan(&unreadCount); err != nil {
			t.Fatalf("querying unread count: %v", err)
		}
		if unreadCount != 0 {
			t.Errorf("unread count = %d, want 0", unreadCount)
		}
	})
}

func TestHandleItems_FilterByFeed(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Two feeds, one item each.
	for i, title := range []string{"Feed A", "Feed B"} {
		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			i+1, userID, "https://example.com/"+title, title,
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		if _, err := s.db.Exec(
			`INSERT INTO items (id, feed_id, guid, title, read, published_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			i+1, i+1, "guid"+title, "Item "+title, 0, "2024-01-01 00:00:00",
		); err != nil {
			t.Fatalf("inserting item: %v", err)
		}
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/items?feed=1")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify the filter SQL is correct by checking item counts per feed.
	var feed1Count, feed2Count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE feed_id = 1").Scan(&feed1Count); err != nil {
		t.Fatalf("querying feed 1 items: %v", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM items WHERE feed_id = 2").Scan(&feed2Count); err != nil {
		t.Fatalf("querying feed 2 items: %v", err)
	}
	if feed1Count != 1 || feed2Count != 1 {
		t.Errorf("expected 1 item per feed, got feed1=%d feed2=%d", feed1Count, feed2Count)
	}
}

func TestHandleItems_FilterByUnread(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	if _, err := s.db.Exec(
		"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
		1, userID, "https://example.com/feed", "Test Feed",
	); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}

	// One read item, one unread item.
	for _, row := range []struct {
		id   int
		guid string
		read int
	}{
		{1, "guid-read", 1},
		{2, "guid-unread", 0},
	} {
		if _, err := s.db.Exec(
			`INSERT INTO items (id, feed_id, guid, title, read, published_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, 1, row.guid, "Item", row.read, "2024-01-01 00:00:00",
		); err != nil {
			t.Fatalf("inserting item: %v", err)
		}
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/items?unread=1")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify the unread filter would match exactly 1 item.
	var unreadCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id
		 WHERE f.user_id = ? AND i.read = 0`, userID,
	).Scan(&unreadCount); err != nil {
		t.Fatalf("querying unread count: %v", err)
	}
	if unreadCount != 1 {
		t.Errorf("unread count = %d, want 1", unreadCount)
	}
}

func TestHandleItems_FilterByStarred(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	if _, err := s.db.Exec(
		"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
		1, userID, "https://example.com/feed", "Test Feed",
	); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}

	if _, err := s.db.Exec(
		`INSERT INTO items (id, feed_id, guid, title, read, starred, published_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		1, 1, "guid1", "Starred Item", 0, 1, "2024-01-01 00:00:00",
	); err != nil {
		t.Fatalf("inserting item: %v", err)
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/items?starred=1")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify the starred filter would match exactly 1 item.
	var starredCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id
		 WHERE f.user_id = ? AND i.starred = 1`, userID,
	).Scan(&starredCount); err != nil {
		t.Fatalf("querying starred count: %v", err)
	}
	if starredCount != 1 {
		t.Errorf("starred count = %d, want 1", starredCount)
	}
}

func TestHandleMarkRead_ByFeed(t *testing.T) {
	t.Run("marks only that feed's items", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		for i, title := range []string{"Feed A", "Feed B"} {
			if _, err := s.db.Exec(
				"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
				i+1, userID, "https://example.com/"+title, title,
			); err != nil {
				t.Fatalf("inserting feed: %v", err)
			}
			if _, err := s.db.Exec(
				`INSERT INTO items (id, feed_id, guid, title, read, published_at)
				 VALUES (?, ?, ?, ?, ?, ?)`,
				i+1, i+1, "guid"+title, "Item", 0, "2024-01-01 00:00:00",
			); err != nil {
				t.Fatalf("inserting item: %v", err)
			}
		}

		form := url.Values{"feed": {"1"}}
		req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// Feed 1's item should be read; feed 2's should still be unread.
		var readCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM items WHERE feed_id = 1 AND read = 1",
		).Scan(&readCount); err != nil {
			t.Fatalf("querying read count for feed 1: %v", err)
		}
		if readCount != 1 {
			t.Errorf("feed 1 read count = %d, want 1", readCount)
		}

		var unreadCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM items WHERE feed_id = 2 AND read = 0",
		).Scan(&unreadCount); err != nil {
			t.Fatalf("querying unread count for feed 2: %v", err)
		}
		if unreadCount != 1 {
			t.Errorf("feed 2 unread count = %d, want 1", unreadCount)
		}
	})

	t.Run("returns 404 for another user's feed", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "owner", "testpass1")
		otherID := createTestUser(t, s, "other", "testpass2")

		// Feed belongs to otherID, not userID.
		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			1, otherID, "https://example.com/feed", "Other's Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		form := url.Values{"feed": {"1"}}
		req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestHandleMarkRead_ByList(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Two feeds, one item each.
	for i, title := range []string{"Feed A", "Feed B"} {
		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			i+1, userID, "https://example.com/"+title, title,
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		if _, err := s.db.Exec(
			`INSERT INTO items (id, feed_id, guid, title, read, published_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			i+1, i+1, "guid"+title, "Item", 0, "2024-01-01 00:00:00",
		); err != nil {
			t.Fatalf("inserting item: %v", err)
		}
	}

	// List containing only feed 1.
	if _, err := s.db.Exec(
		"INSERT INTO lists (id, user_id, name) VALUES (?, ?, ?)",
		1, userID, "My List",
	); err != nil {
		t.Fatalf("inserting list: %v", err)
	}
	if _, err := s.db.Exec(
		"INSERT INTO list_feeds (list_id, feed_id) VALUES (?, ?)",
		1, 1,
	); err != nil {
		t.Fatalf("inserting list_feed: %v", err)
	}

	form := url.Values{"list": {"1"}}
	req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
	req.Body = io.NopCloser(strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	// Feed 1's item (in the list) should be read.
	var readCount int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM items WHERE feed_id = 1 AND read = 1",
	).Scan(&readCount); err != nil {
		t.Fatalf("querying read count for feed 1: %v", err)
	}
	if readCount != 1 {
		t.Errorf("feed 1 (in list) read count = %d, want 1", readCount)
	}

	// Feed 2's item (not in the list) should still be unread.
	var unreadCount int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM items WHERE feed_id = 2 AND read = 0",
	).Scan(&unreadCount); err != nil {
		t.Fatalf("querying unread count for feed 2: %v", err)
	}
	if unreadCount != 1 {
		t.Errorf("feed 2 (not in list) unread count = %d, want 1", unreadCount)
	}
}
