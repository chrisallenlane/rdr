package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleToggleStar(t *testing.T) {
	// insertFeedAndItem is a local helper that creates a feed and item for
	// the given user, returning the item ID.
	insertFeedAndItem := func(t *testing.T, s *Server, userID int64) int64 {
		t.Helper()
		res, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			userID, "https://example.com/feed.xml", "Test Feed",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("feed LastInsertId: %v", err)
		}

		res, err = s.db.Exec(
			`INSERT INTO items (feed_id, guid, title, published_at, read, starred)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			feedID, "guid1", "Test Item", "2024-01-01 00:00:00", 0, 0,
		)
		if err != nil {
			t.Fatalf("inserting item: %v", err)
		}
		itemID, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("item LastInsertId: %v", err)
		}
		return itemID
	}

	starredValue := func(t *testing.T, s *Server, itemID int64) int {
		t.Helper()
		var v int
		if err := s.db.QueryRow(
			"SELECT starred FROM items WHERE id = ?", itemID,
		).Scan(&v); err != nil {
			t.Fatalf("querying starred: %v", err)
		}
		return v
	}

	t.Run("toggle on", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		itemID := insertFeedAndItem(t, s, userID)

		req := authedRequest(
			t, s, userID,
			http.MethodPost, fmt.Sprintf("/items/%d/star", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if got := starredValue(t, s, itemID); got != 1 {
			t.Errorf("starred = %d, want 1 after toggle on", got)
		}
	})

	t.Run("toggle off", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		itemID := insertFeedAndItem(t, s, userID)

		// Star the item first.
		if _, err := s.db.Exec(
			"UPDATE items SET starred = 1 WHERE id = ?", itemID,
		); err != nil {
			t.Fatalf("pre-starring item: %v", err)
		}

		req := authedRequest(
			t, s, userID,
			http.MethodPost, fmt.Sprintf("/items/%d/star", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if got := starredValue(t, s, itemID); got != 0 {
			t.Errorf("starred = %d, want 0 after toggle off", got)
		}
	})

	t.Run("HTMX toggle on returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		itemID := insertFeedAndItem(t, s, userID)

		req := authedRequest(
			t, s, userID,
			http.MethodPost, fmt.Sprintf("/items/%d/star", itemID),
		)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "starred") {
			t.Errorf("body = %q, want to contain 'starred'", body)
		}
		if got := starredValue(t, s, itemID); got != 1 {
			t.Errorf("starred = %d, want 1", got)
		}
	})

	t.Run("HTMX toggle off returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		itemID := insertFeedAndItem(t, s, userID)

		s.db.Exec("UPDATE items SET starred = 1 WHERE id = ?", itemID)

		req := authedRequest(
			t, s, userID,
			http.MethodPost, fmt.Sprintf("/items/%d/star", itemID),
		)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "unstarred") {
			t.Errorf("body = %q, want to contain 'unstarred'", body)
		}
		if got := starredValue(t, s, itemID); got != 0 {
			t.Errorf("starred = %d, want 0", got)
		}
	})

	t.Run("cannot star another user's item", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass1")
		itemID := insertFeedAndItem(t, s, ownerID)

		req := authedRequest(
			t, s, attackerID,
			http.MethodPost, fmt.Sprintf("/items/%d/star", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		// Item must remain unstarred.
		if got := starredValue(t, s, itemID); got != 0 {
			t.Errorf(
				"starred = %d after unauthorized attempt, want 0", got,
			)
		}
	})
}
