package handler

import (
	"fmt"
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

// insertFilterTestData inserts a common set of feeds and items into s.db for
// use by TestBuildItemFilter sub-tests. It returns the IDs that were created:
// feed1ID, feed2ID, listID (feed1 only), and the user's own userID.
//
// Item layout:
//   - item 1: feed1, unread, not starred
//   - item 2: feed1, read, starred
//   - item 3: feed2, unread, not starred
//   - item 4: feed2, read, not starred
func insertFilterTestData(t *testing.T, s *Server, userID int64) (feed1ID, feed2ID, listID int64) {
	t.Helper()

	res, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
		userID, "https://feed1.example.com/feed.xml", "Feed 1",
	)
	if err != nil {
		t.Fatalf("inserting feed1: %v", err)
	}
	feed1ID, _ = res.LastInsertId()

	res, err = s.db.Exec(
		"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
		userID, "https://feed2.example.com/feed.xml", "Feed 2",
	)
	if err != nil {
		t.Fatalf("inserting feed2: %v", err)
	}
	feed2ID, _ = res.LastInsertId()

	// List containing only feed1.
	res, err = s.db.Exec(
		"INSERT INTO lists (user_id, name) VALUES (?, ?)",
		userID, "My List",
	)
	if err != nil {
		t.Fatalf("inserting list: %v", err)
	}
	listID, _ = res.LastInsertId()
	if _, err := s.db.Exec(
		"INSERT INTO list_feeds (list_id, feed_id) VALUES (?, ?)", listID, feed1ID,
	); err != nil {
		t.Fatalf("inserting list_feed: %v", err)
	}

	type itemRow struct {
		id      int
		feedID  int64
		guid    string
		read    int
		starred int
	}
	items := []itemRow{
		{1, feed1ID, "guid-f1-unread", 0, 0},
		{2, feed1ID, "guid-f1-read-starred", 1, 1},
		{3, feed2ID, "guid-f2-unread", 0, 0},
		{4, feed2ID, "guid-f2-read", 1, 0},
	}
	for _, row := range items {
		if _, err := s.db.Exec(
			`INSERT INTO items (id, feed_id, guid, title, read, starred, published_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			row.id, row.feedID, row.guid, "Item "+row.guid,
			row.read, row.starred, "2024-01-01 00:00:00",
		); err != nil {
			t.Fatalf("inserting item %d: %v", row.id, err)
		}
	}

	return feed1ID, feed2ID, listID
}

// countFiltered runs a COUNT query using the WHERE clause produced by
// buildItemFilter and returns the result.
func countFiltered(
	t *testing.T,
	s *Server,
	userID, feedID, listID int64,
	unread, starred bool,
) int {
	t.Helper()
	where, args := buildItemFilter(userID, feedID, listID, unread, starred)
	query := "SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE " + where
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("countFiltered: %v", err)
	}
	return n
}

func TestBuildItemFilter(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")
	feed1ID, feed2ID, listID := insertFilterTestData(t, s, userID)

	// Confirm the test data is set up correctly before the sub-tests run.
	// Four items total across two feeds.
	if n := countFiltered(t, s, userID, 0, 0, false, false); n != 4 {
		t.Fatalf("base case: got %d items, want 4", n)
	}

	t.Run("base case returns all user items", func(t *testing.T) {
		got := countFiltered(t, s, userID, 0, 0, false, false)
		if got != 4 {
			t.Errorf("got %d items, want 4", got)
		}
	})

	t.Run("feed filter restricts to that feed", func(t *testing.T) {
		got := countFiltered(t, s, userID, feed1ID, 0, false, false)
		if got != 2 {
			t.Errorf("feed1 filter: got %d items, want 2", got)
		}
		got = countFiltered(t, s, userID, feed2ID, 0, false, false)
		if got != 2 {
			t.Errorf("feed2 filter: got %d items, want 2", got)
		}
	})

	t.Run("unread filter restricts to unread items", func(t *testing.T) {
		// Items 1 (feed1, unread) and 3 (feed2, unread) are unread.
		got := countFiltered(t, s, userID, 0, 0, true, false)
		if got != 2 {
			t.Errorf("unread filter: got %d items, want 2", got)
		}
	})

	t.Run("starred filter restricts to starred items", func(t *testing.T) {
		// Only item 2 (feed1, read, starred) is starred.
		got := countFiltered(t, s, userID, 0, 0, false, true)
		if got != 1 {
			t.Errorf("starred filter: got %d items, want 1", got)
		}
	})

	t.Run("list filter restricts to feeds in the list", func(t *testing.T) {
		// The list contains feed1 only, so 2 items.
		got := countFiltered(t, s, userID, 0, listID, false, false)
		if got != 2 {
			t.Errorf("list filter: got %d items, want 2", got)
		}
	})

	t.Run("combined filters are ANDed together", func(t *testing.T) {
		// feed1 + unread: only item 1.
		got := countFiltered(t, s, userID, feed1ID, 0, true, false)
		if got != 1 {
			t.Errorf("feed+unread: got %d items, want 1", got)
		}

		// feed1 + starred: only item 2.
		got = countFiltered(t, s, userID, feed1ID, 0, false, true)
		if got != 1 {
			t.Errorf("feed+starred: got %d items, want 1", got)
		}

		// list + unread: only item 1 (feed1, unread).
		got = countFiltered(t, s, userID, 0, listID, true, false)
		if got != 1 {
			t.Errorf("list+unread: got %d items, want 1", got)
		}
	})

	t.Run("filter isolates data from another user", func(t *testing.T) {
		otherID := createTestUser(t, s, "other", "testpass2")
		// Other user has no items.
		got := countFiltered(t, s, otherID, 0, 0, false, false)
		if got != 0 {
			t.Errorf("other user: got %d items, want 0", got)
		}
	})
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

	t.Run("HTMX returns fragment with flash", func(t *testing.T) {
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
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("redirect preserves list filter", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			1, userID, "https://example.com/feed", "Test Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "My List",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		if _, err := s.db.Exec(
			"INSERT INTO list_feeds (list_id, feed_id) VALUES (?, ?)",
			listID, 1,
		); err != nil {
			t.Fatalf("inserting list_feed: %v", err)
		}

		form := url.Values{"list": {fmt.Sprintf("%d", listID)}}
		req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		wantLoc := fmt.Sprintf("/items?list=%d", listID)
		if loc := rec.Header().Get("Location"); loc != wantLoc {
			t.Errorf("Location = %q, want %q", loc, wantLoc)
		}
	})

	t.Run("redirect preserves feed filter", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			userID, "https://example.com/feed", "Test Feed",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := result.LastInsertId()

		form := url.Values{"feed": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(t, s, userID, http.MethodPost, "/items/mark-read")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		wantLoc := fmt.Sprintf("/items?feed=%d", feedID)
		if loc := rec.Header().Get("Location"); loc != wantLoc {
			t.Errorf("Location = %q, want %q", loc, wantLoc)
		}
	})
}

// TestHandleItems_FilterByFeed, TestHandleItems_FilterByUnread, and
// TestHandleItems_FilterByStarred verify that the /items handler returns 200
// for each filter query parameter. The behavioral correctness of the filter
// logic itself is covered by TestBuildItemFilter, which runs the produced WHERE
// clause against real data.

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
