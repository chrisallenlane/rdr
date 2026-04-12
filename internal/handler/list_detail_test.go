package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// insertTestList inserts a list owned by userID and returns its ID.
func insertTestList(t *testing.T, s *Server, userID int64, name string) int64 {
	t.Helper()
	result, err := s.db.Exec(
		"INSERT INTO lists (user_id, name) VALUES (?, ?)",
		userID, name,
	)
	if err != nil {
		t.Fatalf("inserting list %q: %v", name, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

// insertTestFeed inserts a feed owned by userID and returns its ID.
func insertTestFeed(t *testing.T, s *Server, userID int64, feedURL string) int64 {
	t.Helper()
	result, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		userID, feedURL,
	)
	if err != nil {
		t.Fatalf("inserting feed %q: %v", feedURL, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	return id
}

// TestHandleListDetail covers GET /lists/{id}.
func TestHandleListDetail(t *testing.T) {
	t.Run("own list returns 200", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")

		req := authedRequest(t, s, userID, http.MethodGet, fmt.Sprintf("/lists/%d", listID))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("another user's list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		viewerID := createTestUser(t, s, "viewer", "testpass2")
		listID := insertTestList(t, s, ownerID, "Owner List")

		req := authedRequest(
			t, s, viewerID,
			http.MethodGet,
			fmt.Sprintf("/lists/%d", listID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("non-existent list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(t, s, userID, http.MethodGet, "/lists/99999")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

// TestHandleAddFeedToList covers POST /lists/{id}/feeds.
func TestHandleAddFeedToList(t *testing.T) {
	t.Run("add own feed to own list redirects to list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		form := url.Values{"feed_id": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		want := fmt.Sprintf("/lists/%d", listID)
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("Location = %q, want %q", loc, want)
		}

		var gotListID int64
		if err := s.db.QueryRow(
			"SELECT list_id FROM feeds WHERE id = ?",
			feedID,
		).Scan(&gotListID); err != nil {
			t.Fatalf("querying feed list_id: %v", err)
		}
		if gotListID != listID {
			t.Errorf("feed list_id = %d, want %d", gotListID, listID)
		}
	})

	t.Run("add feed to another user's list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass2")
		listID := insertTestList(t, s, ownerID, "Owner List")
		feedID := insertTestFeed(t, s, attackerID, "https://attacker.example.com/feed.xml")

		form := url.Values{"feed_id": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(
			t, s, attackerID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("add another user's feed to own list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		otherID := createTestUser(t, s, "other", "testpass2")
		listID := insertTestList(t, s, ownerID, "Owner List")
		feedID := insertTestFeed(t, s, otherID, "https://other.example.com/feed.xml")

		form := url.Values{"feed_id": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(
			t, s, ownerID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("adding a duplicate feed is silently accepted", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		// Pre-assign the feed to the list.
		if _, err := s.db.Exec(
			"UPDATE feeds SET list_id = ? WHERE id = ?",
			listID, feedID,
		); err != nil {
			t.Fatalf("pre-setting list_id: %v", err)
		}

		form := url.Values{"feed_id": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		// Duplicate should still redirect — no error surfaced to the client.
		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
	})

	t.Run("HTMX add feed returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		form := url.Values{"feed_id": {fmt.Sprintf("%d", feedID)}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
	})

	t.Run("invalid feed_id returns 400", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")

		form := url.Values{"feed_id": {"not-a-number"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}

// TestHandleRemoveFeedFromList covers POST /lists/{id}/feeds/{feedID}/delete.
func TestHandleRemoveFeedFromList(t *testing.T) {
	t.Run("remove feed from own list redirects to list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		if _, err := s.db.Exec(
			"UPDATE feeds SET list_id = ? WHERE id = ?",
			listID, feedID,
		); err != nil {
			t.Fatalf("setting list_id: %v", err)
		}

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds/%d/delete", listID, feedID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		want := fmt.Sprintf("/lists/%d", listID)
		if loc := rec.Header().Get("Location"); loc != want {
			t.Errorf("Location = %q, want %q", loc, want)
		}

		var gotListID *int64
		if err := s.db.QueryRow(
			"SELECT list_id FROM feeds WHERE id = ?",
			feedID,
		).Scan(&gotListID); err != nil {
			t.Fatalf("querying feed list_id: %v", err)
		}
		if gotListID != nil {
			t.Errorf("feed list_id = %v, want nil", gotListID)
		}
	})

	t.Run("HTMX remove feed returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		s.db.Exec("UPDATE feeds SET list_id = ? WHERE id = ?", listID, feedID)

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds/%d/delete", listID, feedID),
		)
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
	})

	t.Run("remove feed from another user's list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass2")
		listID := insertTestList(t, s, ownerID, "Owner List")
		feedID := insertTestFeed(t, s, ownerID, "https://example.com/feed.xml")

		if _, err := s.db.Exec(
			"UPDATE feeds SET list_id = ? WHERE id = ?",
			listID, feedID,
		); err != nil {
			t.Fatalf("setting list_id: %v", err)
		}

		req := authedRequest(
			t, s, attackerID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds/%d/delete", listID, feedID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("non-numeric list ID returns 400", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/lists/abc/feeds/1/delete",
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("non-numeric feed ID returns 400", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/feeds/abc/delete", listID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}
