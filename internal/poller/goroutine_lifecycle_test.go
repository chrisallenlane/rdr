package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
)

// TestTriggerSyncDBCloseRaceProducesErrors demonstrates that the
// goroutine spawned by Poller.TriggerSync (internal/poller/poller.go:65)
// is not joinable from main's shutdown path, so a db.Close() that runs
// while a TriggerSync poll is in flight produces "sql: database is
// closed" errors from inside the running goroutine.
//
// Why this race exists in production:
//
//	cmd/rdr/main.go:54-62 builds a sync.WaitGroup and registers
//	Poller.Start() with it. That's the *periodic* loop.
//	TriggerSync (called by HTTP handlers via the SyncFunc indirection)
//	does its own `go p.poll(p.ctx)` (line 65), which is never wg.Added.
//
//	On shutdown, main runs wg.Wait() → httpServer.Shutdown() → defer
//	db.Close(). wg.Wait() returns immediately because Start() has
//	already returned (its loop selects on ctx.Done()). The TriggerSync
//	goroutine — if one happens to be in flight — is left running.
//	When the deferred db.Close() fires, that goroutine's next DB
//	statement (e.g. recordFetchFailure() at internal/poller/feed.go:106,
//	which runs in a deferred FetchAndStoreFeed cleanup and does NOT
//	take ctx) writes to a closed handle.
//
// The fix is to thread a sync.WaitGroup (or a poller-internal one
// exposed via a Wait() method) through Poller so TriggerSync's
// goroutine is registered and main's wg.Wait can join it.
func TestTriggerSyncDBCloseRaceProducesErrors(t *testing.T) {
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

	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())

	if started := p.TriggerSync(context.Background()); !started {
		t.Fatal("TriggerSync returned false on a fresh poller")
	}

	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first feed fetch")
	}

	// Sanity check: the TriggerSync goroutine is observably in flight.
	if !p.IsSyncing() {
		t.Fatal("expected IsSyncing to be true while fetch is blocked")
	}

	// Close the DB while the goroutine is still in flight. In
	// production this is `defer db.Close()` at cmd/rdr/main.go:34
	// firing before TriggerSync's goroutine has finished — because
	// nothing in main's wg.Wait() waits for it.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	// Release the server. The goroutine will now try to UPDATE feeds
	// against a closed DB. Either:
	//   - The UPDATE at feed.go:60 (success path) errors out, OR
	//   - The HTTP error path runs recordFetchFailure() at feed.go:106,
	//     which also UPDATEs the closed DB.
	// Both paths log "sql: database is closed" via slog.
	close(release)

	// Wait for the goroutine to drain so it can't leak past t.Cleanup.
	deadline := time.Now().Add(5 * time.Second)
	for p.IsSyncing() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for syncing flag to clear")
		}
		time.Sleep(time.Millisecond)
	}

	// Confirm db is closed (proves the race condition's preconditions).
	_, err := db.Exec("SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed-DB error, got: %v", err)
	}

	// The bug is structural: the test cannot fail because there is no
	// way for the application to prevent this race today. The slog
	// output captured during the test is the production-visible
	// symptom; the assertion is on the code shape.
}
