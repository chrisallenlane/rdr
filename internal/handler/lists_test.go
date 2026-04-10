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

	t.Run("HTMX create returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{"name": {"New List"}}
		req := authedRequest(t, s, userID, http.MethodPost, "/lists")
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
		if !strings.Contains(rec.Body.String(), "New List") {
			t.Errorf("body should contain new list name")
		}
	})

	t.Run("HTMX duplicate name returns 422", func(t *testing.T) {
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
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
	})

	t.Run("HTMX empty name returns 422", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{"name": {""}}
		req := authedRequest(t, s, userID, http.MethodPost, "/lists")
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
		}
	})
}

func TestHandleRenameList(t *testing.T) {
	t.Run("valid rename", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Old Name",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		form := url.Values{"name": {"New Name"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != fmt.Sprintf("/lists/%d", listID) {
			t.Errorf("Location = %q, want /lists/%d", loc, listID)
		}

		var name string
		if err := s.db.QueryRow(
			"SELECT name FROM lists WHERE id = ?", listID,
		).Scan(&name); err != nil {
			t.Fatalf("querying list name: %v", err)
		}
		if name != "New Name" {
			t.Errorf("name = %q, want %q", name, "New Name")
		}
	})

	t.Run("empty name", func(t *testing.T) {
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

		form := url.Values{"name": {"  "}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// Name should be unchanged.
		var name string
		if err := s.db.QueryRow(
			"SELECT name FROM lists WHERE id = ?", listID,
		).Scan(&name); err != nil {
			t.Fatalf("querying list name: %v", err)
		}
		if name != "My List" {
			t.Errorf("name = %q, want %q", name, "My List")
		}
	})

	t.Run("duplicate name", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Taken Name",
		); err != nil {
			t.Fatalf("inserting first list: %v", err)
		}

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Other List",
		)
		if err != nil {
			t.Fatalf("inserting second list: %v", err)
		}
		listID, _ := result.LastInsertId()

		form := url.Values{"name": {"Taken Name"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}

		// Name should be unchanged.
		var name string
		if err := s.db.QueryRow(
			"SELECT name FROM lists WHERE id = ?", listID,
		).Scan(&name); err != nil {
			t.Fatalf("querying list name: %v", err)
		}
		if name != "Other List" {
			t.Errorf("name = %q, want %q", name, "Other List")
		}
	})

	t.Run("HTMX duplicate name returns 422", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		if _, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Taken Name",
		); err != nil {
			t.Fatalf("inserting first list: %v", err)
		}

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Other List",
		)
		if err != nil {
			t.Fatalf("inserting second list: %v", err)
		}
		otherID, _ := result.LastInsertId()

		form := url.Values{"name": {"Taken Name"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", otherID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}

		// Name should be unchanged.
		var name string
		if err := s.db.QueryRow(
			"SELECT name FROM lists WHERE id = ?", otherID,
		).Scan(&name); err != nil {
			t.Fatalf("querying list name: %v", err)
		}
		if name != "Other List" {
			t.Errorf("name = %q, want %q", name, "Other List")
		}
	})

	t.Run("HTMX valid rename returns 204 with triggers", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Old Name",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		form := url.Values{"name": {"New Name"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNoContent)
		}
		trigger := rec.Header().Get("HX-Trigger")
		if !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
		if !strings.Contains(trigger, "setPageTitle") {
			t.Errorf("HX-Trigger = %q, want to contain setPageTitle", trigger)
		}
	})

	t.Run("HTMX empty name returns 422 with flash", func(t *testing.T) {
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

		form := url.Values{"name": {"  "}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
		}
		if trigger := rec.Header().Get("HX-Trigger"); !strings.Contains(trigger, "showFlash") {
			t.Errorf("HX-Trigger = %q, want to contain showFlash", trigger)
		}
	})

	t.Run("non-existent list", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		form := url.Values{"name": {"New Name"}}
		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/lists/99999/rename",
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("another user's list", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			ownerID, "Owner's List",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		form := url.Values{"name": {"Hijacked"}}
		req := authedRequest(
			t, s, attackerID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/rename", listID),
		)
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}

		// Name should be unchanged.
		var name string
		if err := s.db.QueryRow(
			"SELECT name FROM lists WHERE id = ?", listID,
		).Scan(&name); err != nil {
			t.Fatalf("querying list name: %v", err)
		}
		if name != "Owner's List" {
			t.Errorf("name = %q, want %q", name, "Owner's List")
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

	t.Run("HTMX delete non-existent list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/lists/99999/delete",
		)
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("HTMX delete another user's list returns 404", func(t *testing.T) {
		s := newTestServer(t)
		ownerID := createTestUser(t, s, "owner", "testpass1")
		attackerID := createTestUser(t, s, "attacker", "testpass2")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			ownerID, "Owner's List",
		)
		if err != nil {
			t.Fatalf("inserting list: %v", err)
		}
		listID, _ := result.LastInsertId()

		req := authedRequest(
			t, s, attackerID,
			http.MethodPost,
			fmt.Sprintf("/lists/%d/delete", listID),
		)
		req.Header.Set("HX-Request", "true")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}

		// List should still exist.
		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM lists WHERE id = ?", listID,
		).Scan(&count); err != nil {
			t.Fatalf("querying list: %v", err)
		}
		if count != 1 {
			t.Errorf("list row count = %d, want 1 (should still exist)", count)
		}
	})

	t.Run("HTMX delete returns fragment", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		result, err := s.db.Exec(
			"INSERT INTO lists (user_id, name) VALUES (?, ?)",
			userID, "Doomed List",
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
}
