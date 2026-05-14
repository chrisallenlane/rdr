package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/background"
	"github.com/chrisallenlane/rdr/internal/testutil"
)

// TestTriggerSync_BackgroundGoroutineTracked verifies that the goroutine
// spawned by Poller.TriggerSync is registered with the background.Group, so
// bg.Wait() blocks until the poll cycle finishes.
func TestTriggerSync_BackgroundGoroutineTracked(t *testing.T) {
	release := make(chan struct{})
	hits := make(chan struct{}, 4)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case hits <- struct{}{}:
		default:
		}
		<-release
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, validRSSFeed("Test Feed"))
	}))
	defer ts.Close()

	db := testutil.OpenTestDB(t)
	userID := testutil.InsertUser(t, db, "testuser")
	if _, err := db.Exec(
		"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
		userID, ts.URL+"/feed",
	); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}

	// Use a non-cancelled context so the in-flight HTTP request is not aborted
	// before we get a chance to observe that Wait() blocks.
	var bg background.Group
	p := NewPoller(context.Background(), &bg, db, time.Hour, 0, t.TempDir())

	if started := p.TriggerSync(context.Background()); !started {
		t.Fatal("TriggerSync returned false on a fresh poller")
	}

	// Wait for the goroutine to be in flight against the slow feed server.
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first feed fetch")
	}

	// Sanity: the goroutine is observably in flight.
	if !p.IsSyncing() {
		t.Fatal("expected IsSyncing to be true while fetch is blocked")
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
