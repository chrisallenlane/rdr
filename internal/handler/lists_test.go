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

func TestHandleListsPage(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Insert two lists so the handler has data to render.
	for _, name := range []string{"B List", "A List"} {
		if _, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, name,
		); err != nil {
			t.Fatalf("inserting list: %v", err)
		}
	}

	req := authedRequest(t, s, userID, http.MethodGet, "/lists")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandleCreateList(t *testing.T) {
	t.Run("empty name", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{"name": {""}}
		req := authedRequest(t, s, userID, http.MethodPost, "/lists")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("valid name", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		listName := "My Reading List"
		form := url.Values{"name": {listName}}
		req := authedRequest(t, s, userID, http.MethodPost, "/lists")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/lists" {
			t.Errorf("Location = %q, want /lists", loc)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE user_id = ? AND name = ?",
			userID, listName,
		).Scan(&count); err != nil {
			t.Fatalf("querying list: %v", err)
		}
		if count != 1 {
			t.Errorf("list row count = %d, want 1", count)
		}
	})

	t.Run("duplicate name", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Existing List",
		); err != nil {
			t.Fatalf("inserting list: %v", err)
		}

		form := url.Values{"name": {"Existing List"}}
		req := authedRequest(t, s, userID, http.MethodPost, "/lists")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestHandleDeleteList(t *testing.T) {
	t.Run("delete own list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "My List",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/delete", listID),
		)

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/lists" {
			t.Errorf("Location = %q, want /lists", loc)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE id = ?",
			listID,
		).Scan(&count); err != nil {
			t.Fatalf("querying list: %v", err)
		}
		if count != 0 {
			t.Errorf("list row count = %d, want 0", count)
		}
	})

	t.Run("delete non-existent list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/lists/99999/delete",
		)

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})
}
