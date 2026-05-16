package poller

import (
	"bytes"
	"database/sql"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/testutil"
)

// captureSlog redirects slog.Default() to a JSON handler writing into a
// buffer for the duration of the test. The returned function returns the
// captured output as a string. Tests using this MUST NOT call
// t.Parallel() — slog.SetDefault mutates global state.
func captureSlog(t *testing.T) func() string {
	t.Helper()

	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string { return buf.String() }
}

// insertFeed inserts a feed row and returns its id.
func insertFeed(t *testing.T, db *sql.DB, userID int64) int64 {
	t.Helper()

	res, err := db.Exec(
		`INSERT INTO feeds (user_id, url, title) VALUES (?, ?, ?)`,
		userID, "https://example.com/feed.xml", "Test Feed",
	)
	if err != nil {
		t.Fatalf("insertFeed: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insertFeed LastInsertId: %v", err)
	}
	return id
}

// insertItem inserts an item row. publishedAt is a SQLite datetime string.
// read is 0 or 1.
func insertItem(
	t *testing.T,
	db *sql.DB,
	feedID int64,
	guid string,
	publishedAt string,
	read int,
) int64 {
	t.Helper()

	res, err := db.Exec(
		`INSERT INTO items (feed_id, guid, title, published_at, read)
		 VALUES (?, ?, ?, ?, ?)`,
		feedID, guid, "Item "+guid, publishedAt, read,
	)
	if err != nil {
		t.Fatalf("insertItem(%q): %v", guid, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("insertItem LastInsertId: %v", err)
	}
	return id
}

// countItems returns the number of rows in the items table.
func countItems(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items`).Scan(&n); err != nil {
		t.Fatalf("countItems: %v", err)
	}
	return n
}

// countSessions returns the number of rows in the sessions table.
func countSessions(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("countSessions: %v", err)
	}
	return n
}

func TestPruneOldItems_ZeroRetention(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Insert an old, read item that would be deleted if retention were active.
	insertItem(t, db, feedID, "old-read-1", "2020-01-01 00:00:00", 1)

	n, err := PruneOldItems(db, 0)
	if err != nil {
		t.Fatalf("PruneOldItems(0): unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("PruneOldItems(0) = %d, want 0", n)
	}

	// Item must still be present.
	if got := countItems(t, db); got != 1 {
		t.Errorf("item count after no-op prune = %d, want 1", got)
	}
}

func TestPruneOldItems_NegativeRetention(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)
	insertItem(t, db, feedID, "old-read-1", "2020-01-01 00:00:00", 1)

	n, err := PruneOldItems(db, -5)
	if err != nil {
		t.Fatalf("PruneOldItems(-5): unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("PruneOldItems(-5) = %d, want 0", n)
	}
	if got := countItems(t, db); got != 1 {
		t.Errorf("item count after no-op prune = %d, want 1", got)
	}
}

func TestPruneOldItems_DeletesOldReadItems(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Old and read: should be deleted.
	insertItem(t, db, feedID, "old-read-1", "2020-01-01 00:00:00", 1)
	insertItem(t, db, feedID, "old-read-2", "2019-06-15 12:00:00", 1)

	// Old but unread: must be kept.
	insertItem(t, db, feedID, "old-unread-1", "2020-01-01 00:00:00", 0)

	// Recent and read: must be kept (published within the last 30 days).
	insertItem(t, db, feedID, "recent-read-1", "2099-12-31 00:00:00", 1)

	// Recent and unread: must be kept.
	insertItem(t, db, feedID, "recent-unread-1", "2099-12-31 00:00:00", 0)

	n, err := PruneOldItems(db, 30)
	if err != nil {
		t.Fatalf("PruneOldItems(30): %v", err)
	}
	if n != 2 {
		t.Errorf("PruneOldItems(30) deleted %d rows, want 2", n)
	}

	// Three items must survive.
	if got := countItems(t, db); got != 3 {
		t.Errorf("item count after prune = %d, want 3", got)
	}
}

func TestPruneOldItems_KeepsUnreadItems(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Unread items, however old, must never be pruned.
	insertItem(t, db, feedID, "old-unread-1", "2000-01-01 00:00:00", 0)
	insertItem(t, db, feedID, "old-unread-2", "2000-06-01 00:00:00", 0)

	n, err := PruneOldItems(db, 30)
	if err != nil {
		t.Fatalf("PruneOldItems(30): %v", err)
	}
	if n != 0 {
		t.Errorf(
			"PruneOldItems(30) = %d, want 0 (unread items kept)",
			n,
		)
	}
	if got := countItems(t, db); got != 2 {
		t.Errorf("item count = %d, want 2", got)
	}
}

func TestPruneOldItems_KeepsRecentItems(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Read but very recent (far future date acts as "within retention window").
	insertItem(t, db, feedID, "recent-read-1", "2099-01-01 00:00:00", 1)
	insertItem(t, db, feedID, "recent-read-2", "2099-06-01 00:00:00", 1)

	n, err := PruneOldItems(db, 30)
	if err != nil {
		t.Fatalf("PruneOldItems(30): %v", err)
	}
	if n != 0 {
		t.Errorf(
			"PruneOldItems(30) = %d, want 0 (recent read items kept)",
			n,
		)
	}
	if got := countItems(t, db); got != 2 {
		t.Errorf("item count = %d, want 2", got)
	}
}

func TestPruneOldItems_KeepsStarredItems(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Old, read, but starred: must survive pruning.
	starredID := insertItem(t, db, feedID, "old-read-starred", "2020-01-01 00:00:00", 1)
	if _, err := db.Exec("UPDATE items SET starred = 1 WHERE id = ?", starredID); err != nil {
		t.Fatalf("starring item: %v", err)
	}

	// Old, read, not starred: should be pruned.
	insertItem(t, db, feedID, "old-read-unstarred", "2020-01-01 00:00:00", 1)

	n, err := PruneOldItems(db, 30)
	if err != nil {
		t.Fatalf("PruneOldItems(30): %v", err)
	}
	if n != 1 {
		t.Errorf("PruneOldItems(30) deleted %d rows, want 1", n)
	}

	// The starred item must still be present.
	if got := countItems(t, db); got != 1 {
		t.Errorf("item count after prune = %d, want 1 (starred item kept)", got)
	}
	var survived int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM items WHERE starred = 1",
	).Scan(&survived); err != nil {
		t.Fatalf("counting starred items: %v", err)
	}
	if survived != 1 {
		t.Errorf("starred item count = %d, want 1", survived)
	}
}

func TestPruneOldItems_KeepsRecentReadItems(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")
	feedID := insertFeed(t, db, userID)

	// Item published 5 days ago, read: must be kept with 30-day retention.
	recent := time.Now().AddDate(0, 0, -5).Format("2006-01-02 15:04:05")
	insertItem(t, db, feedID, "recent-read", recent, 1)

	// Item published 60 days ago, read: should be pruned.
	old := time.Now().AddDate(0, 0, -60).Format("2006-01-02 15:04:05")
	insertItem(t, db, feedID, "old-read", old, 1)

	n, err := PruneOldItems(db, 30)
	if err != nil {
		t.Fatalf("PruneOldItems(30): %v", err)
	}
	if n != 1 {
		t.Errorf("PruneOldItems(30) deleted %d rows, want 1", n)
	}
	if got := countItems(t, db); got != 1 {
		t.Errorf("item count after prune = %d, want 1 (recent read kept)", got)
	}
}

func TestCleanExpiredSessions_DeletesExpired(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")

	// Expired sessions: must be deleted.
	testutil.InsertSession(
		t, db, userID, "expired-1",
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	testutil.InsertSession(
		t, db, userID, "expired-2",
		time.Date(2019, 6, 15, 12, 0, 0, 0, time.UTC),
	)

	// Valid sessions: must be kept.
	testutil.InsertSession(
		t, db, userID, "valid-1",
		time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC),
	)
	testutil.InsertSession(
		t, db, userID, "valid-2",
		time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
	)

	n, err := CleanExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanExpiredSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("CleanExpiredSessions deleted %d sessions, want 2", n)
	}
	if got := countSessions(t, db); got != 2 {
		t.Errorf("session count after clean = %d, want 2", got)
	}
}

func TestCleanExpiredSessions_KeepsValidSessions(t *testing.T) {
	db := testutil.OpenTestDB(t)

	userID := testutil.InsertUser(t, db, "alice")

	testutil.InsertSession(
		t, db, userID, "valid-1",
		time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC),
	)
	testutil.InsertSession(
		t, db, userID, "valid-2",
		time.Date(2099, 6, 1, 0, 0, 0, 0, time.UTC),
	)

	n, err := CleanExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanExpiredSessions: %v", err)
	}
	if n != 0 {
		t.Errorf("CleanExpiredSessions deleted %d sessions, want 0", n)
	}
	if got := countSessions(t, db); got != 2 {
		t.Errorf("session count = %d, want 2", got)
	}
}

func TestCleanExpiredSessions_EmptyTable(t *testing.T) {
	db := testutil.OpenTestDB(t)

	n, err := CleanExpiredSessions(db)
	if err != nil {
		t.Fatalf("CleanExpiredSessions on empty table: %v", err)
	}
	if n != 0 {
		t.Errorf("CleanExpiredSessions on empty table = %d, want 0", n)
	}
}

func TestRunRetention(t *testing.T) {
	t.Run("prunes items and cleans sessions", func(t *testing.T) {
		db := testutil.OpenTestDB(t)

		userID := testutil.InsertUser(t, db, "alice")
		feedID := insertFeed(t, db, userID)

		// Old read item: should be pruned.
		insertItem(t, db, feedID, "old-read-1", "2020-01-01 00:00:00", 1)
		// Recent unread: should be kept.
		insertItem(t, db, feedID, "recent-unread", "2099-12-31 00:00:00", 0)

		// Expired session: should be cleaned.
		testutil.InsertSession(t, db, userID, "expired-session",
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
		// Valid session: should be kept.
		testutil.InsertSession(t, db, userID, "valid-session",
			time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC))

		runRetention(db, 7)

		if got := countItems(t, db); got != 1 {
			t.Errorf("item count after runRetention = %d, want 1 (old-read pruned)", got)
		}
		if got := countSessions(t, db); got != 1 {
			t.Errorf("session count after runRetention = %d, want 1 (expired cleaned)", got)
		}
	})

	t.Run("with zero days skips item pruning but cleans sessions", func(t *testing.T) {
		db := testutil.OpenTestDB(t)

		userID := testutil.InsertUser(t, db, "alice")
		feedID := insertFeed(t, db, userID)

		// Old read item: should NOT be pruned when retentionDays is 0.
		insertItem(t, db, feedID, "old-read-1", "2020-01-01 00:00:00", 1)

		// Expired session: should still be cleaned.
		testutil.InsertSession(t, db, userID, "expired-session",
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
		// Valid session: should be kept.
		testutil.InsertSession(t, db, userID, "valid-session",
			time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC))

		runRetention(db, 0)

		if got := countItems(t, db); got != 1 {
			t.Errorf("item count after runRetention(0) = %d, want 1 (no pruning when disabled)", got)
		}
		if got := countSessions(t, db); got != 1 {
			t.Errorf("session count after runRetention(0) = %d, want 1 (expired cleaned)", got)
		}
	})
}

// TestRunMaintenance_Succeeds verifies the happy path: runMaintenance
// against a healthy DB completes without an error log and the
// surrounding runRetention call still works.
func TestRunMaintenance_Succeeds(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)

	runRetention(db, 0)

	if strings.Contains(getLogs(), "maintenance: PRAGMA optimize failed") {
		t.Errorf("captured logs contained unexpected optimize failure:\n%s", getLogs())
	}
}

// TestRunMaintenance_LogsErrorOnClosedDB verifies that a failing
// PRAGMA optimize is logged at WARN and runMaintenance returns
// normally (does not panic, does not abort the caller).
func TestRunMaintenance_LogsErrorOnClosedDB(t *testing.T) {
	getLogs := captureSlog(t)
	db := testutil.OpenTestDB(t)

	// Close the DB; subsequent Exec returns ErrConnDone or similar.
	if err := db.Close(); err != nil {
		t.Fatalf("closing DB: %v", err)
	}

	// runMaintenance must not panic.
	runMaintenance(db)

	logs := getLogs()
	if !strings.Contains(logs, "maintenance: PRAGMA optimize failed") {
		t.Errorf("expected WARN log for failed PRAGMA optimize, got:\n%s", logs)
	}
	// Confirm the level is WARN, not ERROR.
	if !strings.Contains(logs, `"level":"WARN"`) {
		t.Errorf("expected level=WARN in log output, got:\n%s", logs)
	}
}
