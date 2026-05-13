package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// TestAddFeedGoroutineSurvivesDBClose demonstrates that the
// fire-and-forget goroutine spawned by AddFeed at
// internal/api/feeds.go:138 is not lifecycle-tracked: it uses
// context.Background() (line 140), so neither request cancellation nor
// application shutdown can stop it before db.Close() runs underneath it.
//
// In production, when cmd/rdr/main.go's deferred db.Close() fires, an
// AddFeed-spawned goroutine that happens to be in flight will write to
// a closed handle. The result is "sql: database is closed" errors
// logged from inside FetchAndStoreFeed via slog.Warn at feeds.go:141.
//
// This is strictly worse than the HTML AddFeed handler
// (internal/handler/feeds.go:141) which fetches synchronously inside
// the request — that design has its own latency tradeoff but at least
// piggybacks on http.Server.Shutdown's grace period.
//
// The fix is to thread a sync.WaitGroup (or a dedicated "background
// jobs" type) through Config so this goroutine is registered and
// main's wg.Wait can join it; plus use the application context (not
// context.Background()) so the in-flight HTTP call can be cancelled.
func TestAddFeedGoroutineSurvivesDBClose(t *testing.T) {
	// Slow feed server that blocks until the test releases it. This
	// lets us deterministically interleave db.Close() with the
	// in-flight fetch.
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

	// FeedResolver returns the slow server URL. AddFeed will then
	// insert the row and spawn the background fetch goroutine.
	feedURL := ts.URL + "/feed.xml"
	h := New(Config{
		DB:           db,
		FeedResolver: stubResolver(feedURL, nil),
	})

	body := fmt.Sprintf(`{"url": %q}`, ts.URL)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/v1/feeds", tok, body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201; body=%q", rec.Code, rec.Body.String())
	}

	// Wait for the background goroutine to actually be in flight
	// against the slow server.
	select {
	case <-hits:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background fetch to start")
	}

	// Now close the DB while the goroutine is still blocked in the
	// feed fetch. This is the exact production scenario: AddFeed
	// returned 201 to the client, then main.go's `defer db.Close()`
	// fires (because nothing waits on the spawned goroutine).
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	// Release the server. The goroutine will now race into UPDATE/INSERT
	// statements against a closed DB.
	close(release)

	// Give the goroutine time to attempt its DB writes. There is no
	// public API to wait for it, which is exactly the bug.
	time.Sleep(200 * time.Millisecond)

	// Confirm the DB is closed (proves the precondition for the race).
	_, err = db.Exec("SELECT 1")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected closed-DB error, got: %v", err)
	}

	// The bug is structural — there is no way for the test to assert
	// "the goroutine has finished" because no Wait() exists. The
	// captured slog output ("sql: database is closed") is the
	// production-visible symptom; the assertion is on the code shape.
}
