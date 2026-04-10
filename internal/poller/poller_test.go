package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
)

// validRSSFeed returns a minimal valid RSS feed XML string.
func validRSSFeed(title string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>%s</title>
    <link>http://example.com</link>
    <item>
      <title>Post 1</title>
      <link>http://example.com/1</link>
      <guid>guid-1</guid>
    </item>
  </channel>
</rss>`, title)
}

func TestPollConcurrency(t *testing.T) {
	// Track peak concurrent requests.
	var inflight atomic.Int32
	var peakConcurrent atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		// Update peak if this is a new high.
		for {
			old := peakConcurrent.Load()
			if cur <= old || peakConcurrent.CompareAndSwap(old, cur) {
				break
			}
		}

		time.Sleep(100 * time.Millisecond) // simulate slow server
		inflight.Add(-1)

		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, validRSSFeed("Test Feed"))
	}))
	defer ts.Close()

	db := testutil.OpenTestDB(t)

	// Create a test user.
	if _, err := db.Exec(
		"INSERT INTO users (username, password) VALUES (?, ?)",
		"testuser", "hash",
	); err != nil {
		t.Fatal(err)
	}

	// Insert 5 feeds all pointing at the slow test server.
	for i := range 5 {
		if _, err := db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			1, fmt.Sprintf("%s/feed/%d", ts.URL, i),
		); err != nil {
			t.Fatal(err)
		}
	}

	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())

	start := time.Now()
	p.poll(context.Background())
	elapsed := time.Since(start)

	// With 5 feeds each sleeping 100ms, serial would take >= 500ms.
	// Concurrent should complete well under 500ms.
	if elapsed >= 450*time.Millisecond {
		t.Errorf("poll took %v, expected < 450ms for concurrent execution", elapsed)
	}

	// At least 2 feeds should have been in-flight concurrently.
	if peak := peakConcurrent.Load(); peak < 2 {
		t.Errorf("peak concurrent requests = %d, expected >= 2", peak)
	}

	// Verify all feeds were actually fetched (last_fetched_at updated).
	var fetched int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM feeds WHERE last_fetched_at IS NOT NULL",
	).Scan(&fetched); err != nil {
		t.Fatal(err)
	}
	if fetched != 5 {
		t.Errorf("fetched feeds = %d, want 5", fetched)
	}
}

func TestPollShutdownStopsDispatching(t *testing.T) {
	var requestCount atomic.Int32

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = fmt.Fprint(w, validRSSFeed("Test Feed"))
	}))
	defer ts.Close()

	db := testutil.OpenTestDB(t)

	if _, err := db.Exec(
		"INSERT INTO users (username, password) VALUES (?, ?)",
		"testuser", "hash",
	); err != nil {
		t.Fatal(err)
	}

	// Insert 20 feeds to ensure some won't be dispatched.
	for i := range 20 {
		if _, err := db.Exec(
			"INSERT INTO feeds (user_id, url) VALUES (?, ?)",
			1, fmt.Sprintf("%s/feed/%d", ts.URL, i),
		); err != nil {
			t.Fatal(err)
		}
	}

	// Cancel context immediately so the loop stops dispatching.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())
	p.poll(ctx)

	// With context already cancelled, no feeds should be dispatched.
	if count := requestCount.Load(); count > 0 {
		t.Errorf("expected 0 requests after cancelled context, got %d", count)
	}
}

func TestMaxPollWorkersConstant(t *testing.T) {
	if maxPollWorkers < 2 {
		t.Errorf("maxPollWorkers = %d, want >= 2", maxPollWorkers)
	}
}

func TestTriggerSync_ReturnsTrueWhenIdle(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		t.Fatal(err)
	}

	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())

	got := p.TriggerSync(context.Background())
	if !got {
		t.Error("TriggerSync returned false when no sync was running, want true")
	}
}

func TestTriggerSync_ReturnsFalseWhenAlreadySyncing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())

	// Simulate an in-progress sync by setting the flag directly.
	p.syncing.Store(true)

	got := p.TriggerSync(context.Background())
	if got {
		t.Error("TriggerSync returned true while sync was in progress, want false")
	}
}

func TestTriggerSync_ReturnsTrueAfterSyncCompletes(t *testing.T) {
	// Block the feed fetch so the poll goroutine stays in-flight long enough
	// for us to observe the busy state, then release it.
	unblock := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
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
		t.Fatal(err)
	}

	p := NewPoller(context.Background(), db, time.Hour, 0, t.TempDir())

	// First TriggerSync should start the goroutine.
	if !p.TriggerSync(context.Background()) {
		t.Fatal("first TriggerSync returned false, want true")
	}

	// Wait until the syncing flag is set (poll has acquired it).
	deadline := time.Now().Add(2 * time.Second)
	for !p.syncing.Load() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for syncing flag to be set")
		}
		time.Sleep(time.Millisecond)
	}

	// While the sync is in progress, TriggerSync must return false.
	if p.TriggerSync(context.Background()) {
		t.Error("TriggerSync returned true while sync was in progress, want false")
	}

	// Unblock the server handler so the poll goroutine can finish.
	close(unblock)

	// Wait for syncing flag to clear.
	deadline = time.Now().Add(2 * time.Second)
	for p.syncing.Load() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for syncing flag to clear")
		}
		time.Sleep(time.Millisecond)
	}

	// After the sync completes, TriggerSync must succeed again.
	if !p.TriggerSync(context.Background()) {
		t.Error("TriggerSync returned false after sync completed, want true")
	}
}
