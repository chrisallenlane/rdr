package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSqlBool_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    bool
		wantErr bool
	}{
		{"int64 nonzero", int64(1), true, false},
		{"int64 zero", int64(0), false, false},
		{"nil", nil, false, false},
		{"unsupported type", "string", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b sqlBool
			err := b.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && bool(b) != tt.want {
				t.Errorf("Scan(%v) = %v, want %v", tt.input, bool(b), tt.want)
			}
		})
	}
}

func TestVerifyOwnership(t *testing.T) {
	t.Run("row exists and belongs to user", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, "https://example.com/feed.xml",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := result.LastInsertId()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		got := s.verifyOwnership(rec, req, "feeds", feedID, userID)
		if !got {
			t.Errorf("verifyOwnership = false, want true")
		}
		if rec.Code != http.StatusOK {
			t.Errorf("response status = %d, want %d (no error should be rendered)",
				rec.Code, http.StatusOK)
		}
	})

	t.Run("row does not exist", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		got := s.verifyOwnership(rec, req, "feeds", 99999, userID)
		if got {
			t.Errorf("verifyOwnership = true, want false")
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("response status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("row belongs to different user", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		otherID := createTestUser(t, s, "other", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			ownerID, "https://example.com/feed.xml",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := result.LastInsertId()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		got := s.verifyOwnership(rec, req, "feeds", feedID, otherID)
		if got {
			t.Errorf("verifyOwnership = true, want false")
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("response status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}

func TestVerifyOwnership_InvalidTable(t *testing.T) {
	t.Run("verifyOwnership rejects invalid table", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		got := s.verifyOwnership(rec, req, "hacked_table", 1, userID)
		if got {
			t.Errorf("verifyOwnership = true, want false for invalid table")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("response status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestDeleteByID_InvalidTable(t *testing.T) {
	t.Run("deleteByID rejects invalid table", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		// Route through the mux so path parameters are available.
		// Use /feeds/1/delete as the path but the handler code checks the
		// table parameter before doing any DB work.
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/feeds/1/delete",
		)
		rec := httptest.NewRecorder()

		// Call deleteByID directly with an invalid table name.
		s.deleteByID(rec, req, "hacked_table", "Entity", "/redirect")

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
	})
}

func TestDeleteByID(t *testing.T) {
	t.Run("delete own row", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, "https://example.com/feed.xml",
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		feedID, _ := result.LastInsertId()

		// Route through the mux so the path parameter is parsed.
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/feeds/%d/delete", feedID),
		)

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		found := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == "rdr_flash" && c.Value != "" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected rdr_flash cookie to be set after delete")
		}
	})

	t.Run("delete non-existent row", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/feeds/99999/delete",
		)

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
