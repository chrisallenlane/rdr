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

// postFeedForm creates an authenticated POST /feeds request with the given
// form values.
func postFeedForm(
	t *testing.T,
	s *Server,
	userID int64,
	form url.Values,
) *http.Request {
	t.Helper()
	req := authedRequest(t, s, userID, http.MethodPost, "/feeds")
	req.Body = io.NopCloser(strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestHandleAddFeed(t *testing.T) {
	t.Run("empty URL", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postFeedForm(t, s, userID, url.Values{"url": {""}}))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("invalid URL scheme", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		rec := httptest.NewRecorder()
		s.ServeHTTP(
			rec,
			postFeedForm(t, s, userID, url.Values{"url": {"ftp://example.com/feed.xml"}}),
		)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("valid URL", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		feedURL := "https://example.com/feed.xml"
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postFeedForm(t, s, userID, url.Values{"url": {feedURL}}))

		// The initial fetch will fail (no real HTTP server), but the feed row
		// should still be created and a flash cookie set before the redirect.
		if rec.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusSeeOther)
		}
		if loc := rec.Header().Get("Location"); loc != "/feeds" {
			t.Errorf("Location = %q, want /feeds", loc)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE user_id = ? AND url = ?",
			userID, feedURL,
		).Scan(&count); err != nil {
			t.Fatalf("querying feed: %v", err)
		}
		if count != 1 {
			t.Errorf("feed row count = %d, want 1", count)
		}

		found := false
		for _, c := range rec.Result().Cookies() {
			if c.Name == "rdr_flash" && c.Value != "" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected rdr_flash cookie to be set")
		}
	})

	t.Run("duplicate URL", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		feedURL := "https://example.com/feed.xml"
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, feedURL,
		); err != nil {
			t.Fatalf("inserting feed: %v", err)
		}

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, postFeedForm(t, s, userID, url.Values{"url": {feedURL}}))

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
		}
	})
}

func TestHandleFeeds(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Insert two feeds in reverse alphabetical order.
	for _, u := range []struct{ url, title string }{
		{"https://b.example.com/feed.xml", "B Feed"},
		{"https://a.example.com/feed.xml", "A Feed"},
	} {
		if _, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)",
			userID, u.url, u.title,
		); err != nil {
			t.Fatalf("inserting feed %q: %v", u.title, err)
		}
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, authedRequest(t, s, userID, http.MethodGet, "/feeds"))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify feeds are returned in alphabetical order by title.
	feeds, err := queryUserFeeds(s.db, userID)
	if err != nil {
		t.Fatalf("queryUserFeeds: %v", err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feed count = %d, want 2", len(feeds))
	}
	if feeds[0].Title != "A Feed" || feeds[1].Title != "B Feed" {
		t.Errorf("feeds not in alphabetical order: got %q, %q", feeds[0].Title, feeds[1].Title)
	}
}

func TestHandleAddFeed_InitialFetchFailure(t *testing.T) {
	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	rec := httptest.NewRecorder()
	s.ServeHTTP(
		rec,
		postFeedForm(t, s, userID, url.Values{"url": {"https://example.com/feed.xml"}}),
	)

	// The initial fetch always fails in tests (no real HTTP server), so the
	// handler must take the error path and set the "could not be fetched" flash.
	var flashValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "rdr_flash" {
			flashValue = c.Value
			break
		}
	}

	if !strings.Contains(flashValue, "could not be fetched") {
		t.Errorf(
			"flash cookie = %q, want value containing %q",
			flashValue, "could not be fetched",
		)
	}
}

func TestHandleDeleteFeed(t *testing.T) {
	t.Run("delete own feed", func(t *testing.T) {
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
		if loc := rec.Header().Get("Location"); loc != "/feeds" {
			t.Errorf("Location = %q, want /feeds", loc)
		}

		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM feeds WHERE id = ?",
			feedID,
		).Scan(&count); err != nil {
			t.Fatalf("querying feed: %v", err)
		}
		if count != 0 {
			t.Errorf("feed row count = %d, want 0", count)
		}
	})

	t.Run("delete non-existent feed", func(t *testing.T) {
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

	t.Run("delete non-numeric feed ID", func(t *testing.T) {
		s := newTestServer(t)
		userID := createTestUser(t, s, "testuser", "testpass1")

		req := authedRequest(
			t, s, userID,
			http.MethodPost,
			"/feeds/abc/delete",
		)

		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
		}
	})
}
