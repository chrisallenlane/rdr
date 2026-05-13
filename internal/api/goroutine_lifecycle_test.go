package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/background"
	"github.com/chrisallenlane/rdr/internal/testutil"
	"github.com/chrisallenlane/rdr/internal/token"
)

// validRSSResponse returns a minimal parsable RSS feed.
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

// TestAddFeedGoroutineSurvivesDBClose verifies that the background goroutine
// spawned by AddFeed is tracked by the Config.Background group, so that
// bg.Wait() blocks until the in-flight fetch completes, and the DB is never
// closed underneath it.
//
// The test uses a slow feed server to hold the fetch open, then:
//  1. Confirms bg.Wait() blocks while the fetch is in flight.
//  2. Releases the feed server.
//  3. Confirms bg.Wait() unblocks after the goroutine completes.
//  4. Confirms the DB is still open (no goroutine ran against a closed handle).
func TestAddFeedGoroutineSurvivesDBClose(t *testing.T) {
	// Slow feed server that blocks until the test releases it.
	release := make(chan struct{})
	hits := make(chan struct{}, 4)
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

	db := testutil.OpenTestDB(t)
	alice := testutil.InsertUser(t, db, "alice")
	tok, _, err := token.Generate(db, alice, "alice-test", time.Time{})
	if err != nil {
		t.Fatalf("token.Generate: %v", err)
	}

	// Wire a background.Group so the goroutine is tracked.
	// Use a non-cancelled context so the in-flight HTTP request is not aborted
	// before we get a chance to observe that Wait() blocks.
	var bg background.Group
	feedURL := ts.URL + "/feed.xml"
	h := New(Config{
		DB:           db,
		Ctx:          context.Background(),
		Background:   &bg,
		FeedResolver: stubResolver(feedURL, nil),
	})

	body := fmt.Sprintf(`{"url": %q}`, ts.URL)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", tok, body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%q", rec.Code, rec.Body.String())
	}

	// Wait for the background goroutine to actually be in flight.
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background fetch to start")
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

	// Release the server. The goroutine can now finish.
	close(release)

	// Wait() should unblock once the goroutine completes.
	select {
	case <-waitDone:
		// good
	case <-time.After(5 * time.Second):
		t.Fatal("bg.Wait() did not return after releasing the feed server")
	}

	// The DB must still be open — nothing ran against a closed handle.
	if _, err := db.Exec("SELECT 1"); err != nil {
		t.Errorf("DB should still be open after bg.Wait(), got error: %v", err)
	}
}
