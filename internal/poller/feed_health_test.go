package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// TestFetchAndStoreFeed_AtomicOnContextCancellation verifies the atomicity
// invariant introduced by the transaction wrap: on any error during the
// fetch (including context cancellation that can land before BeginTx OR
// during the INSERT loop), the observable state is:
//
//   - No items inserted (count = 0).
//   - last_fetched_at unchanged (still NULL from initial INSERT).
//   - last_fetch_error is non-empty, proving recordFetchFailure fired
//     via db.Exec on a fresh autocommit AFTER the rollback defer.
//
// The deferred-order invariant — recordFetchFailure registered FIRST,
// rollback defer registered SECOND, LIFO order causing rollback to fire
// FIRST so recordFetchFailure can acquire the only DB connection — is
// the load-bearing correctness property this test guards. If either
// defer were registered in the wrong order, recordFetchFailure would
// deadlock against the held transaction for the full busy_timeout
// window and this test would either hang or last_fetch_error would be
// empty.
func TestFetchAndStoreFeed_AtomicOnContextCancellation(t *testing.T) {
	// Slow HTTP server: deliberately sleeps before responding so the
	// test can cancel the context while the request is in flight. The
	// exact landing point of the cancellation (during HTTP Do, during
	// parse, or during the INSERT loop) is non-deterministic — the
	// atomicity invariants hold regardless of which path was taken.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, validRSSFeed("Atomicity Test"))
	}))
	defer ts.Close()

	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "atomic-user")

	res, err := db.Exec("INSERT INTO feeds (user_id, url) VALUES (?, ?)", userID, ts.URL)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	feed := &model.Feed{ID: feedID, UserID: userID, URL: ts.URL}

	// Cancel the context after 10ms — well before the 200ms server delay,
	// so the HTTP request fails with context.Canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if err := FetchAndStoreFeed(ctx, db, feed, ""); err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	// Invariant 1: no items inserted.
	var itemCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM items WHERE feed_id = ?", feedID,
	).Scan(&itemCount); err != nil {
		t.Fatalf("counting items: %v", err)
	}
	if itemCount != 0 {
		t.Errorf("items inserted on failed fetch: count = %d, want 0", itemCount)
	}

	// Invariant 2: last_fetched_at unchanged (still NULL).
	var lastFetched any
	if err := db.QueryRow(
		"SELECT last_fetched_at FROM feeds WHERE id = ?", feedID,
	).Scan(&lastFetched); err != nil {
		t.Fatalf("querying last_fetched_at: %v", err)
	}
	if lastFetched != nil {
		t.Errorf("last_fetched_at = %v, want nil (must not be set on failure)", lastFetched)
	}

	// Invariant 3: last_fetch_error is set. This proves the deferred
	// recordFetchFailure fired AFTER the rollback defer — i.e., the
	// LIFO ordering of the defers is correct. If the rollback defer
	// were registered first (LIFO would fire recordFetchFailure first),
	// recordFetchFailure's db.Exec would block on the held transaction
	// for busy_timeout=5s before failing, and last_fetch_error would
	// remain empty (or the test would hang).
	var lastErr string
	if err := db.QueryRow(
		"SELECT last_fetch_error FROM feeds WHERE id = ?", feedID,
	).Scan(&lastErr); err != nil {
		t.Fatalf("querying last_fetch_error: %v", err)
	}
	if lastErr == "" {
		t.Error("last_fetch_error should be set after failure (deferred recordFetchFailure)")
	}
}
