package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertFeedAndItems creates a feed owned by userID and inserts items with the
// given published_at timestamps. It returns the item IDs in insertion order.
func insertFeedAndItems(
	t *testing.T,
	s *Server,
	userID int64,
	publishedAts []string,
) (feedID int64, itemIDs []int64) {
	t.Helper()

	res, err := s.db.Exec(
		"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
		userID, "https://example.com/feed.xml", "Test Feed",
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, err = res.LastInsertId()
	if err != nil {
		t.Fatalf("feed LastInsertId: %v", err)
	}

	for i, pub := range publishedAts {
		res, err = s.db.Exec(
			`INSERT INTO items (feed_id, guid, title, published_at, read, starred)
			 VALUES (?, ?, ?, ?, 0, 0)`,
			feedID, fmt.Sprintf("guid%d", i), fmt.Sprintf("Item %d", i), pub,
		)
		if err != nil {
			t.Fatalf("inserting item %d: %v", i, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("item LastInsertId: %v", err)
		}
		itemIDs = append(itemIDs, id)
	}

	return feedID, itemIDs
}

// TestHandleItemDetail covers the handleItemDetail handler.
func TestHandleItemDetail(t *testing.T) {
	t.Run("owned item returns 200", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(
			t, s, userID, []string{"2024-01-01 00:00:00"},
		)
		itemID := itemIDs[0]

		req := authedRequest(
			t, s, userID,
			http.MethodGet, fmt.Sprintf("/items/%d", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("item owned by different user returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass2")
		_, itemIDs := insertFeedAndItems(
			t, s, ownerID, []string{"2024-01-01 00:00:00"},
		)
		itemID := itemIDs[0]

		req := authedRequest(
			t, s, attackerID,
			http.MethodGet, fmt.Sprintf("/items/%d", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("non-existent item ID returns 404", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodGet, "/items/999999",
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("invalid (non-numeric) item ID returns 400", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodGet, "/items/not-a-number",
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})

	t.Run("unread item is marked read after viewing", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(
			t, s, userID, []string{"2024-01-01 00:00:00"},
		)
		itemID := itemIDs[0]

		// Confirm the item starts unread.
		var readVal int
		if err := s.db.QueryRow(
			"SELECT read FROM items WHERE id = ?", itemID,
		).Scan(&readVal); err != nil {
			t.Fatalf("querying read before view: %v", err)
		}
		if readVal != 0 {
			t.Fatalf("precondition: read = %d, want 0", readVal)
		}

		req := authedRequest(
			t, s, userID,
			http.MethodGet, fmt.Sprintf("/items/%d", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		if err := s.db.QueryRow(
			"SELECT read FROM items WHERE id = ?", itemID,
		).Scan(&readVal); err != nil {
			t.Fatalf("querying read after view: %v", err)
		}
		if readVal != 1 {
			t.Errorf("read = %d after viewing, want 1", readVal)
		}
	})

	t.Run("already-read item does not change read_at on re-view", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(
			t, s, userID, []string{"2024-01-01 00:00:00"},
		)
		itemID := itemIDs[0]

		// Pre-mark the item as read with a fixed read_at.
		if _, err := s.db.Exec(
			"UPDATE items SET read = 1, read_at = '2024-06-01 12:00:00' WHERE id = ?",
			itemID,
		); err != nil {
			t.Fatalf("pre-marking item read: %v", err)
		}

		// Capture the read_at as the driver returns it, before the request.
		var readAtBefore sql.NullString
		if err := s.db.QueryRow(
			"SELECT read_at FROM items WHERE id = ?", itemID,
		).Scan(&readAtBefore); err != nil {
			t.Fatalf("querying read_at before re-view: %v", err)
		}

		req := authedRequest(
			t, s, userID,
			http.MethodGet, fmt.Sprintf("/items/%d", itemID),
		)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var readAtAfter sql.NullString
		if err := s.db.QueryRow(
			"SELECT read_at FROM items WHERE id = ?", itemID,
		).Scan(&readAtAfter); err != nil {
			t.Fatalf("querying read_at after re-view: %v", err)
		}
		if readAtAfter != readAtBefore {
			t.Errorf(
				"read_at changed: before=%q after=%q",
				readAtBefore.String, readAtAfter.String,
			)
		}
	})
}

// TestAdjacentItemID covers the adjacentItemID helper.
func TestAdjacentItemID(t *testing.T) {
	// Shared setup: one user with three items at distinct published_at values.
	// Items are ordered oldest→newest: itemIDs[0], itemIDs[1], itemIDs[2].
	const (
		pubOldest = "2024-01-01 00:00:00"
		pubMiddle = "2024-02-01 00:00:00"
		pubNewest = "2024-03-01 00:00:00"
	)

	t.Run("middle item: prev returns older item", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(t, s, userID, []string{
			pubOldest, pubMiddle, pubNewest,
		})

		got := s.adjacentItemID(userID, pubMiddle, itemIDs[1], true)
		if got == nil {
			t.Fatal("adjacentItemID(prev) = nil, want oldest item ID")
		}
		if *got != itemIDs[0] {
			t.Errorf("adjacentItemID(prev) = %d, want %d", *got, itemIDs[0])
		}
	})

	t.Run("middle item: next returns newer item", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(t, s, userID, []string{
			pubOldest, pubMiddle, pubNewest,
		})

		got := s.adjacentItemID(userID, pubMiddle, itemIDs[1], false)
		if got == nil {
			t.Fatal("adjacentItemID(next) = nil, want newest item ID")
		}
		if *got != itemIDs[2] {
			t.Errorf("adjacentItemID(next) = %d, want %d", *got, itemIDs[2])
		}
	})

	t.Run("first (oldest) item: prev returns nil", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(t, s, userID, []string{
			pubOldest, pubMiddle, pubNewest,
		})

		got := s.adjacentItemID(userID, pubOldest, itemIDs[0], true)
		if got != nil {
			t.Errorf("adjacentItemID(prev) = %d, want nil", *got)
		}
	})

	t.Run("last (newest) item: next returns nil", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		_, itemIDs := insertFeedAndItems(t, s, userID, []string{
			pubOldest, pubMiddle, pubNewest,
		})

		got := s.adjacentItemID(userID, pubNewest, itemIDs[2], false)
		if got != nil {
			t.Errorf("adjacentItemID(next) = %d, want nil", *got)
		}
	})

	t.Run("items belonging to a different user are excluded", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")
		otherID := createTestUser(t, s, "other", "testpass2")

		// The querying user has only the middle item.
		_, myItemIDs := insertFeedAndItems(t, s, userID, []string{pubMiddle})

		// The other user owns the surrounding items; they must not appear as
		// adjacent results when querying on behalf of userID.
		insertFeedAndItems(t, s, otherID, []string{pubOldest, pubNewest})

		prevGot := s.adjacentItemID(userID, pubMiddle, myItemIDs[0], true)
		if prevGot != nil {
			t.Errorf(
				"adjacentItemID(prev) = %d, want nil (other user's item leaked)",
				*prevGot,
			)
		}

		nextGot := s.adjacentItemID(userID, pubMiddle, myItemIDs[0], false)
		if nextGot != nil {
			t.Errorf(
				"adjacentItemID(next) = %d, want nil (other user's item leaked)",
				*nextGot,
			)
		}
	})
}
