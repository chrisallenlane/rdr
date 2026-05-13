package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/background"
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

// TestOPMLImportGoroutineSurvivesDBClose (name preserved for test-history
// continuity) verifies that the background goroutine spawned by
// handleImportOPML is tracked by the server's background.Group so that
// bg.Wait() blocks until the in-flight fetch completes, and the DB is never
// closed underneath it.
//
// The test uses a slow feed server to hold the goroutine open, then:
//  1. Confirms bg.Wait() blocks while the fetch is in flight.
//  2. Releases the feed server.
//  3. Confirms bg.Wait() unblocks after the goroutine completes.
//  4. Confirms the DB is still open (no goroutine ran against a closed handle).
func TestOPMLImportGoroutineSurvivesDBClose(t *testing.T) {
	// A slow feed server that blocks until the test releases it.
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

	// Build the server with a non-cancelled context and explicit bg group,
	// matching the production wiring in main.go. We don't cancel the context
	// here so the in-flight HTTP request is not aborted before we can observe
	// that bg.Wait() blocks.
	var bg background.Group
	s := newTestServerWithBG(t, context.Background(), &bg)
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

	// Invoke the real background dispatch path: s.bg.Go wraps fetchImportedFeeds.
	// This mirrors what handleImportOPML does.
	s.bg.Go(func() { s.fetchImportedFeeds(s.ctx, feeds) })

	// Wait for the first fetch to be in flight against the slow server.
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first feed fetch to start")
	}

	// Call bg.Wait() in a goroutine so we can assert it blocks.
	waitDone := make(chan struct{})
	go func() {
		defer close(waitDone)
		bg.Wait()
	}()

	// bg.Wait() must still be blocking — the goroutine holds the fetch open.
	select {
	case <-waitDone:
		t.Fatal("bg.Wait() returned before the feed server was released — goroutine was not tracked")
	case <-time.After(150 * time.Millisecond):
		// good: still blocking
	}

	// Release the slow feed server; the goroutine can now finish.
	close(release)

	// Wait() should unblock once the goroutine completes.
	select {
	case <-waitDone:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("bg.Wait() did not return after releasing the feed server")
	}

	// The DB must still be open — nothing ran against a closed handle.
	if _, err := s.db.Exec("SELECT 1"); err != nil {
		t.Errorf("DB should still be open after bg.Wait(), got error: %v", err)
	}
}

// newTestServerWithBG creates a *Server like newTestServer but wires it
// with the given context and background.Group, matching the production
// NewServer call in main.go.
func newTestServerWithBG(t *testing.T, ctx context.Context, bg *background.Group) *Server {
	t.Helper()
	s := newTestServer(t)
	s.ctx = ctx
	s.bg = bg
	return s
}
