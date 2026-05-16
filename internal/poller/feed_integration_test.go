//go:build integration

// Package poller integration tests run against real upstream feeds.
// Excluded from `make test` and CI; run via `make integration-test`.
package poller

import (
	"context"
	"testing"

	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

// TestFetchAndStoreFeed_LiveNetwork_RealFeed exercises the full feed-fetch
// pipeline against a real public feed: HTTP GET, gofeed parse,
// metadata UPDATE, item INSERTs, and (with empty faviconsDir) skips the
// favicon path. Asserts only that the fetch succeeded and at least one
// item landed in the database. Does NOT assert on item content — feed
// contents change over time.
//
// Catches: real-world TLS handling, redirect chains, response-size limits
// interacting with real bodies, user-agent acceptance.
func TestFetchAndStoreFeed_LiveNetwork_RealFeed(t *testing.T) {
	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "alice")

	// Known-stable public feed: the official Go blog. Atom format.
	// If this URL breaks, swap to any other reliably-available feed.
	const feedURL = "https://go.dev/blog/feed.atom"

	res, err := db.Exec("INSERT INTO feeds (user_id, url) VALUES (?, ?)", userID, feedURL)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()
	feed := &model.Feed{ID: feedID, UserID: userID, URL: feedURL}

	if err := FetchAndStoreFeed(context.Background(), db, feed, ""); err != nil {
		t.Fatalf("FetchAndStoreFeed: %v", err)
	}

	var itemCount int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM items WHERE feed_id = ?", feedID,
	).Scan(&itemCount); err != nil {
		t.Fatalf("counting items: %v", err)
	}
	if itemCount == 0 {
		t.Errorf("expected ≥1 item inserted, got 0")
	}

	var lastFetched any
	if err := db.QueryRow(
		"SELECT last_fetched_at FROM feeds WHERE id = ?", feedID,
	).Scan(&lastFetched); err != nil {
		t.Fatalf("querying last_fetched_at: %v", err)
	}
	if lastFetched == nil {
		t.Error("last_fetched_at should be set after successful fetch")
	}
}
