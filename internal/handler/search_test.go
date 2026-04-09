package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSearch(t *testing.T) {
	t.Run("empty query", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(t, s, userID, http.MethodGet, "/search")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("valid query with no results", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(t, s, userID, http.MethodGet, "/search?q=golang")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("valid query with results", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Insert a feed and item so FTS5 has something to match.
		if _, err := s.db.Exec(
			"INSERT INTO feeds (id, user_id, url, title) VALUES (?, ?, ?, ?)",
			1, userID, "https://example.com/feed", "Test Feed",
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		if _, err := s.db.Exec(
			`INSERT INTO items (id, feed_id, guid, title, content, read, published_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			1, 1, "guid1", "Golang is great", "A post about golang", 0,
			"2024-01-01 00:00:00",
		); err != nil {
			t.Fatalf("inserting item: %v", err)
		}

		req := authedRequest(t, s, userID, http.MethodGet, "/search?q=golang")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		// Verify the FTS5 index actually has matching results.
		var ftsCount int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM items_fts WHERE items_fts MATCH 'golang'",
		).Scan(&ftsCount); err != nil {
			t.Fatalf("querying FTS5: %v", err)
		}
		if ftsCount == 0 {
			t.Error("FTS5 returned 0 results for 'golang', expected at least 1")
		}
	})
}
