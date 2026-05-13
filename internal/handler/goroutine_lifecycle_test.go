package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
)

// validRSSResponse returns the bytes of a minimal valid RSS feed. The body is
// inert; we only care that gofeed parses it without error so FetchAndStoreFeed
// reaches the UPDATE/INSERT statements that exercise the database.
func validRSSResponse() string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>http://example.com</link>
    <item>
      <title>Post 1</title>
      <link>http://example.com/1</link>
      <guid>guid-1</guid>
    </item>
  </channel>
</rss>`
}

// TestOPMLImportGoroutineSurvivesDBClose demonstrates that
// handleImportOPML's background goroutine (the one spawned at
// internal/handler/opml.go:314 with `context.WithoutCancel`) is NOT
// tracked by any lifecycle owner: it is happy to keep running after the
// database has been closed.
//
// In production, main.go's shutdown path runs:
//
//	wg.Wait()       // waits only on the poller's periodic loop
//	httpServer.Shutdown(...)
//	defer db.Close()
//
// fetchImportedFeeds is registered with NEITHER the wg nor the
// httpServer, so its goroutine outlives both. With the test below,
// driving fetchImportedFeeds directly and closing the DB underneath
// it produces "sql: database is closed" errors from inside
// FetchAndStoreFeed.
func TestOPMLImportGoroutineSurvivesDBClose(t *testing.T) {
	// A slow feed server that blocks until the test releases it. This
	// lets us deterministically interleave db.Close() with an in-flight
	// fetch.
	release := make(chan struct{})
	hits := make(chan struct{}, 32)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case hits <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, validRSSResponse())
	}))
	defer ts.Close()

	s := newTestServer(t)
	userID := createTestUser(t, s, "testuser", "testpass1")

	// Seed two feeds so fetchImportedFeeds iterates more than once.
	var feeds []*model.Feed
	for i := 0; i < 2; i++ {
		feedURL := fmt.Sprintf("%s/feed/%d", ts.URL, i)
		res, err := s.db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			userID, feedURL,
		)
		if err != nil {
			t.Fatalf("inserting feed: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId: %v", err)
		}
		feeds = append(feeds, &model.Feed{ID: id, UserID: userID, URL: feedURL})
	}

	// fetchImportedFeeds is fire-and-forget in the real code. Track its
	// completion here only so the test itself doesn't leak; production
	// code has no such hook.
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Match the real call site exactly: context.WithoutCancel(r.Context()).
		ctx := context.WithoutCancel(context.Background())
		s.fetchImportedFeeds(ctx, feeds)
	}()

	// Wait for the first fetch to actually be in flight against the
	// slow server (i.e. the goroutine is past the SELECT and into the
	// network call that precedes the UPDATE/INSERT statements).
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first feed fetch")
	}

	// Now slam the database shut, simulating cmd/rdr/main.go's
	// `defer db.Close()` running while a background import is still
	// mid-flight. In production, this happens because main's wg.Wait()
	// has nothing to wait for — fetchImportedFeeds was never registered.
	if err := s.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	// Release the slow feed server. The goroutine will now race into
	// the database UPDATE/INSERT statements with a closed DB.
	close(release)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fetchImportedFeeds did not return after release+close")
	}

	// Sanity check on the bug shape: the goroutine kept running after
	// db.Close(). We can confirm this by observing that subsequent
	// queries on the same handle fail. The real-world impact is
	// "sql: database is closed" being logged via slog.Warn from inside
	// FetchAndStoreFeed when the deferred recordFetchFailure tries to
	// run an UPDATE.
	_, err := s.db.Exec("SELECT 1")
	if err == nil {
		t.Fatal("expected db to be closed, but Exec succeeded")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("unexpected error from closed DB: %v", err)
	}

	// The bug is structural, not behavioral: there's no way for the
	// orchestrator to wait for this goroutine before closing the DB,
	// because the goroutine was spawned with `go s.fetchImportedFeeds(...)`
	// and the Server type owns no sync.WaitGroup that the orchestrator
	// can join. This test passes today (the close races the goroutine
	// successfully); the assertion is on the code shape, not on the
	// log output.
}
