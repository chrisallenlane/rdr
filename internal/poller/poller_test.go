package poller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/background"
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

	p := NewPoller(context.Background(), &background.Group{}, db, time.Hour, 0, "")
	p.poll(context.Background())

	// Concurrency is verified structurally by the peak-in-flight counter
	// rather than by wall-clock timing. A prior wall-clock assertion was
	// flaky on loaded CI hosts — if feeds are processed concurrently at
	// all, peakConcurrent will be > 1, which is what we actually care about.
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

	p := NewPoller(context.Background(), &background.Group{}, db, time.Hour, 0, "")
	p.poll(ctx)

	// With context already cancelled, no feeds should be dispatched.
	if count := requestCount.Load(); count > 0 {
		t.Errorf("expected 0 requests after cancelled context, got %d", count)
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

	p := NewPoller(context.Background(), &background.Group{}, db, time.Hour, 0, "")

	got := p.TriggerSync(context.Background())
	if !got {
		t.Error("TriggerSync returned false when no sync was running, want true")
	}
}

func TestTriggerSync_ReturnsFalseWhenAlreadySyncing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	p := NewPoller(context.Background(), &background.Group{}, db, time.Hour, 0, "")

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

	// Pass "" for faviconsDir so favicon.Fetch is skipped. The test feed's
	// <link>http://example.com</link> would otherwise drive a real DNS
	// lookup + HTTP request to example.com for /favicon.ico, which is the
	// dominant source of timing variance under load and against the test's
	// 2-second wait deadlines. This test pins TriggerSync sync-flag
	// semantics, not favicon behavior. See CLAUDE.md "Integration tests".
	p := NewPoller(context.Background(), &background.Group{}, db, time.Hour, 0, "")

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

// TestMaybeVacuum_RunsOnFirstCycle verifies that the zero-value lastVacuum
// satisfies the 24h gate, so VACUUM runs on the first call.
func TestMaybeVacuum_RunsOnFirstCycle(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)

	p := &Poller{db: db}
	p.maybeVacuum(context.Background())

	if !strings.Contains(getLogs(), "maintenance: VACUUM complete") {
		t.Errorf("expected VACUUM to run on first cycle; logs:\n%s", getLogs())
	}
	if p.lastVacuum.IsZero() {
		t.Error("lastVacuum should be set after successful VACUUM")
	}
	if p.consecutiveVacuumFailures != 0 {
		t.Errorf("consecutiveVacuumFailures = %d, want 0", p.consecutiveVacuumFailures)
	}
}

// TestMaybeVacuum_SkipsWithinInterval verifies that a fresh lastVacuum
// gates the second call, so VACUUM does NOT run twice within 24h.
func TestMaybeVacuum_SkipsWithinInterval(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)

	p := &Poller{db: db, lastVacuum: time.Now()}
	p.maybeVacuum(context.Background())

	if strings.Contains(getLogs(), "maintenance: VACUUM complete") {
		t.Errorf("VACUUM unexpectedly ran when within 24h interval; logs:\n%s", getLogs())
	}
}

// TestMaybeVacuum_DurationFieldPresent verifies the success log includes
// a non-zero duration field.
func TestMaybeVacuum_DurationFieldPresent(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)

	p := &Poller{db: db}
	p.maybeVacuum(context.Background())

	logs := getLogs()
	// The slog JSON handler renders time.Duration as a numeric nanosecond
	// count: "duration": 123456. Search for the field name and a non-zero
	// digit immediately after (with optional whitespace).
	if !strings.Contains(logs, `"duration"`) {
		t.Errorf("VACUUM success log missing duration field; logs:\n%s", logs)
	}
	// Crude but adequate: assert duration is not the literal 0.
	if strings.Contains(logs, `"duration":0`) {
		t.Errorf("VACUUM duration was 0; logs:\n%s", logs)
	}
}

// TestMaybeVacuum_FailureBackoffEscalates simulates repeated VACUUM
// failures by passing a closed DB and asserts:
//   - consecutiveVacuumFailures increments on each call
//   - lastVacuum is updated on each attempt (so the backoff window applies)
//   - log level escalates from WARN to ERROR once failures >= 3
func TestMaybeVacuum_FailureBackoffEscalates(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)
	if err := db.Close(); err != nil {
		t.Fatalf("closing DB: %v", err)
	}

	p := &Poller{db: db}

	// Failure 1: WARN, counter = 1.
	p.maybeVacuum(context.Background())
	if p.consecutiveVacuumFailures != 1 {
		t.Errorf("after 1st failure: counter = %d, want 1", p.consecutiveVacuumFailures)
	}
	if p.lastVacuum.IsZero() {
		t.Error("lastVacuum should be updated even on failure (to gate backoff)")
	}

	// Failure 2: still WARN, counter = 2. Need to force the interval gate
	// to pass — case 2 returns 1h, so we set lastVacuum back to >= 1h ago.
	p.lastVacuum = time.Now().Add(-2 * time.Hour)
	p.maybeVacuum(context.Background())
	if p.consecutiveVacuumFailures != 2 {
		t.Errorf("after 2nd failure: counter = %d, want 2", p.consecutiveVacuumFailures)
	}

	// Failure 3: ERROR, counter = 3. case 3 returns 6h.
	p.lastVacuum = time.Now().Add(-7 * time.Hour)
	p.maybeVacuum(context.Background())
	if p.consecutiveVacuumFailures != 3 {
		t.Errorf("after 3rd failure: counter = %d, want 3", p.consecutiveVacuumFailures)
	}

	logs := getLogs()
	warnCount := strings.Count(logs, `"level":"WARN","msg":"maintenance: VACUUM failed"`)
	errorCount := strings.Count(logs, `"level":"ERROR","msg":"maintenance: VACUUM failed"`)
	if warnCount != 2 {
		t.Errorf("expected 2 WARN failure logs (failures 1 and 2), got %d; logs:\n%s",
			warnCount, logs)
	}
	if errorCount != 1 {
		t.Errorf("expected 1 ERROR failure log (failure 3), got %d; logs:\n%s",
			errorCount, logs)
	}
}

// TestVacuumFailureBackoff_Schedule pins the exact retry intervals so a
// future maintainer cannot quietly change the cadence and have all the
// other tests still pass.
func TestVacuumFailureBackoff_Schedule(t *testing.T) {
	p := &Poller{}

	tests := []struct {
		failures int
		want     time.Duration
	}{
		{1, 0},
		{2, 1 * time.Hour},
		{3, 6 * time.Hour},
		{4, 24 * time.Hour},
		{10, 24 * time.Hour}, // default arm caps at 24h
	}
	for _, tc := range tests {
		p.consecutiveVacuumFailures = tc.failures
		got := p.vacuumFailureBackoff()
		if got != tc.want {
			t.Errorf("vacuumFailureBackoff() with failures=%d = %v, want %v",
				tc.failures, got, tc.want)
		}
	}
}
