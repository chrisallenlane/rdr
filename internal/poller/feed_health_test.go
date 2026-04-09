package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

func TestRecordFetchFailure(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "testuser")

	// Insert a feed.
	res, err := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		userID, "https://example.com/feed.xml",
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	// Record two consecutive failures.
	recordFetchFailure(db, feedID, "status 404")
	recordFetchFailure(db, feedID, "connection timeout")

	var lastErr string
	var failures int
	if err := db.QueryRow(
		"SELECT last_fetch_error, consecutive_failures FROM feeds WHERE id = ?",
		feedID,
	).Scan(&lastErr, &failures); err != nil {
		t.Fatalf("querying feed: %v", err)
	}

	if lastErr != "connection timeout" {
		t.Errorf("last_fetch_error = %q, want %q", lastErr, "connection timeout")
	}
	if failures != 2 {
		t.Errorf("consecutive_failures = %d, want 2", failures)
	}
}

func TestFetchAndStoreFeed_SuccessClearsErrors(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "testuser")

	// Start a test HTTP server that serves a valid RSS feed.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <item>
      <title>Post 1</title>
      <link>https://example.com/1</link>
      <guid>1</guid>
    </item>
  </channel>
</rss>`)
	}))
	defer ts.Close()

	// Insert a feed pointing at the test server, pre-populated with errors.
	res, err := db.Exec(
		`INSERT INTO feeds (user_id, url, last_fetch_error, consecutive_failures)
		 VALUES (?, ?, ?, ?)`,
		userID, ts.URL, "previous error", 5,
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	feed := &model.Feed{ID: feedID, UserID: userID, URL: ts.URL}
	if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
		t.Fatalf("FetchAndStoreFeed: %v", err)
	}

	var lastErr string
	var failures int
	var lastFetched string
	if err := db.QueryRow(
		"SELECT last_fetch_error, consecutive_failures, last_fetched_at FROM feeds WHERE id = ?",
		feedID,
	).Scan(&lastErr, &failures, &lastFetched); err != nil {
		t.Fatalf("querying feed: %v", err)
	}

	if lastErr != "" {
		t.Errorf("last_fetch_error = %q, want empty", lastErr)
	}
	if failures != 0 {
		t.Errorf("consecutive_failures = %d, want 0", failures)
	}
	if lastFetched == "" {
		t.Error("last_fetched_at should be set after successful fetch")
	}
}

func TestFetchAndStoreFeed_FailureRecordsError(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "testuser")

	// Start a test HTTP server that returns 404.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	res, err := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		userID, ts.URL,
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	feed := &model.Feed{ID: feedID, UserID: userID, URL: ts.URL}
	if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err == nil {
		t.Fatal("expected error from FetchAndStoreFeed, got nil")
	}

	var lastErr string
	var failures int
	if err := db.QueryRow(
		"SELECT last_fetch_error, consecutive_failures FROM feeds WHERE id = ?",
		feedID,
	).Scan(&lastErr, &failures); err != nil {
		t.Fatalf("querying feed: %v", err)
	}

	if lastErr == "" {
		t.Error("last_fetch_error should be set after failed fetch")
	}
	if failures != 1 {
		t.Errorf("consecutive_failures = %d, want 1", failures)
	}
}

func TestFetchAndStoreFeed_FailureDoesNotUpdateLastFetchedAt(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "testuser")

	// Start a test HTTP server that returns 500.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	res, err := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		userID, ts.URL,
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	feed := &model.Feed{ID: feedID, UserID: userID, URL: ts.URL}
	_ = FetchAndStoreFeed(context.Background(), db, feed, "")

	var lastFetched any
	if err := db.QueryRow(
		"SELECT last_fetched_at FROM feeds WHERE id = ?",
		feedID,
	).Scan(&lastFetched); err != nil {
		t.Fatalf("querying feed: %v", err)
	}

	if lastFetched != nil {
		t.Errorf("last_fetched_at = %v, want nil (should not be set on failure)", lastFetched)
	}
}
