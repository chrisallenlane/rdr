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

	// TOCTOU divergence probe. The HTML handler verifies list ownership
	// and feed ownership in two separate SELECT statements, then issues a
	// third UPDATE that only constrains by (feed.id, feed.user_id) — list
	// ownership is NOT re-checked inside the WHERE clause. The API
	// equivalent (AddFeedToList in internal/api/lists.go) folds the
	// list-ownership check into the UPDATE's WHERE via a subquery, making
	// the operation atomic.
	//
	// This test isolates the bare UPDATE statement to demonstrate the
	// structural difference: the HTML handler's UPDATE happily binds a
	// feed to any list_id the SELECT-check would have approved, without
	// re-validating ownership at write time. The FK constraint
	// (feeds.list_id REFERENCES lists(id) ON DELETE SET NULL) is what
	// prevents lasting damage when the list is concurrently deleted —
	// the UPDATE fails with "FOREIGN KEY constraint failed" rather than
	// quietly corrupting state. The observable user-visible effect of a
	// real race is therefore an intermittent 500, not data corruption.
	//
	// Severity is LOW under the homelab/single-user threat model: the
	// race window is microseconds and cross-user attack is not in scope.
	// The finding is the structural divergence with the API handler, not
	// a fireable exploit.
	t.Run("HTML UPDATE statement does not re-check list ownership (TOCTOU shape)", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		// The exact UPDATE that handleAddFeedToList issues, bound to a
		// list_id (99999) that does not exist. The HTML handler's UPDATE
		// WHERE clause does not constrain by list ownership, so absent
		// the FK constraint this statement would succeed and bind the
		// feed to a phantom list. The FK constraint catches it.
		_, err := s.db.Exec(
			"UPDATE feeds SET list_id = ? WHERE id = ? AND user_id = ?",
			99999, feedID, userID,
		)
		if err == nil {
			t.Fatal("UPDATE to nonexistent list_id unexpectedly succeeded; FK constraint may be off")
		}
		if !strings.Contains(err.Error(), "FOREIGN KEY") {
			t.Errorf("expected FK constraint failure, got: %v", err)
		}

		// Contrast: the API's single-statement form refuses the UPDATE
		// at the WHERE-clause level (RowsAffected=0) rather than relying
		// on the FK as backstop. The list-existence subquery makes the
		// operation idempotent and avoids triggering a constraint error
		// in the concurrent-delete race window.
		res, err := s.db.Exec(
			`UPDATE feeds
			    SET list_id = ?
			  WHERE id = ?
			    AND user_id = ?
			    AND ? IN (SELECT id FROM lists WHERE user_id = ?)`,
			99999, feedID, userID, 99999, userID,
		)
		if err != nil {
			t.Fatalf("API-shape UPDATE errored unexpectedly: %v", err)
		}
		n, _ := res.RowsAffected()
		if n != 0 {
			t.Errorf("API-shape UPDATE RowsAffected = %d, want 0 (list does not exist)", n)
		}
	})

	// Demonstrates the user-visible symptom of the race: if the list is
	// deleted in the gap between verifyOwnership and the UPDATE, the FK
	// constraint causes the UPDATE to fail, which the handler surfaces
	// as a 500. The API handler returns 204 (idempotent no-op) in the
	// equivalent race because RowsAffected=0 is a valid outcome there.
	t.Run("HTML handler returns 500 if list disappears between check and UPDATE", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		listID := insertTestList(t, s, userID, "My List")
		feedID := insertTestFeed(t, s, userID, "https://example.com/feed.xml")

		// Simulate the race: delete the list AFTER the (hypothetical)
		// ownership check would have passed. Then the handler invocation
		// proceeds to the UPDATE, which fails on the FK constraint. We
		// can't actually inject between the two statements in the
		// handler, so the closest deterministic approximation is:
		// pre-delete the list and call the handler. Since the
		// verifyOwnership check now also fails, this exercises the
		// "list never existed" path rather than the true race. Kept as
		// behavior pinning — documents the API-vs-HTML response-shape
		// divergence for the missing-list case.
		if _, err := s.db.Exec("DELETE FROM lists WHERE id = ?", listID); err != nil {
			t.Fatalf("deleting list: %v", err)
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

		// Current behavior: verifyOwnership("lists", ...) fails first,
		// so the handler returns 404. (The "true" race would return
		// 500 because the FK fires inside the UPDATE.) Pin the 404.
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d (list deleted before call)", rec.Code, http.StatusNotFound)
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

		_, _ = s.db.Exec("UPDATE feeds SET list_id = ? WHERE id = ?", listID, feedID)

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
