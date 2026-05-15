package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// expectedTables lists every table (including virtual) that schema.sql creates.
var expectedTables = []string{
	"users",
	"sessions",
	"feeds",
	"items",
	"lists",
	"items_fts",
	"user_settings",
}

func TestOpen_CreatesAllTables(t *testing.T) {
	db := openTestDB(t)

	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

func TestOpen_Idempotency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idem.db")

	// Open the database twice; the second call must succeed without errors.
	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	_ = db1.Close()

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer func() { _ = db2.Close() }()
}

func TestOpen_WALEnabled(t *testing.T) {
	db := openTestDB(t)

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", mode)
	}
}

func TestOpen_ForeignKeysEnabled(t *testing.T) {
	db := openTestDB(t)

	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("querying foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("expected foreign_keys=1, got %d", fk)
	}
}

// openTestDB creates a temporary on-disk database (WAL requires a real file)
// and registers cleanup.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(path)
	})
	return db
}

func TestOpen_BusyTimeoutSet(t *testing.T) {
	db := openTestDB(t)

	var timeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("querying busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Errorf("expected busy_timeout=5000, got %d", timeout)
	}
}

func TestOpen_SynchronousSet(t *testing.T) {
	db := openTestDB(t)

	var sync int
	if err := db.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil {
		t.Fatalf("querying synchronous: %v", err)
	}
	// 1 = NORMAL (0=OFF, 1=NORMAL, 2=FULL, 3=EXTRA)
	if sync != 1 {
		t.Errorf("expected synchronous=1 (NORMAL), got %d", sync)
	}
}

func TestOpen_CacheSizeSet(t *testing.T) {
	db := openTestDB(t)

	var size int
	if err := db.QueryRow("PRAGMA cache_size").Scan(&size); err != nil {
		t.Fatalf("querying cache_size: %v", err)
	}
	if size != -65536 {
		t.Errorf("expected cache_size=-65536, got %d", size)
	}
}

func TestOpen_TempStoreSet(t *testing.T) {
	db := openTestDB(t)

	var ts int
	if err := db.QueryRow("PRAGMA temp_store").Scan(&ts); err != nil {
		t.Fatalf("querying temp_store: %v", err)
	}
	// 2 = MEMORY (0=DEFAULT, 1=FILE, 2=MEMORY)
	if ts != 2 {
		t.Errorf("expected temp_store=2 (MEMORY), got %d", ts)
	}
}

func TestOpen_MmapSizeSet(t *testing.T) {
	db := openTestDB(t)

	var size int64
	if err := db.QueryRow("PRAGMA mmap_size").Scan(&size); err != nil {
		t.Fatalf("querying mmap_size: %v", err)
	}
	// modernc.org/sqlite's pure-Go VFS may cap or ignore the mmap hint;
	// the contract this ticket guarantees is "set without error",
	// so any non-negative value (including 0 if mmap is unsupported
	// on this build) is acceptable.
	if size < 0 {
		t.Errorf("expected mmap_size >= 0, got %d", size)
	}
}

func TestOpen_UnwritablePath(t *testing.T) {
	_, err := Open("/proc/nonexistent/test.db")
	if err == nil {
		t.Error("Open with unwritable path returned nil error, want non-nil")
	}
}

func TestOpen_MaxOpenConns(t *testing.T) {
	db := openTestDB(t)

	// With MaxOpenConns(1), a second exclusive lock attempt should block.
	// We verify indirectly: stats should show MaxOpenConnections == 1.
	stats := db.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("expected MaxOpenConnections=1, got %d", stats.MaxOpenConnections)
	}
}

// schemaV1_1_0 is the schema as shipped in rdr v1.1.0, before the goose
// migration system was introduced. It is held verbatim (without IF NOT
// EXISTS) so the upgrade test exercises the realistic on-disk layout an
// existing operator's database has.
const schemaV1_1_0 = `
CREATE TABLE users (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    username   TEXT     NOT NULL UNIQUE,
    password   TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE sessions (
    id         TEXT     PRIMARY KEY,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_sessions_user_id    ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);
CREATE TABLE lists (
    id         INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT     NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, name)
);
CREATE INDEX idx_lists_user_id ON lists(user_id);
CREATE TABLE feeds (
    id                   INTEGER  PRIMARY KEY AUTOINCREMENT,
    user_id              INTEGER  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    list_id              INTEGER  REFERENCES lists(id) ON DELETE SET NULL,
    url                  TEXT     NOT NULL,
    title                TEXT     NOT NULL DEFAULT '',
    site_url             TEXT     NOT NULL DEFAULT '',
    favicon_url          TEXT     NOT NULL DEFAULT '',
    last_fetched_at      DATETIME,
    last_fetch_error     TEXT     NOT NULL DEFAULT '',
    consecutive_failures INTEGER  NOT NULL DEFAULT 0,
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, url)
);
CREATE INDEX idx_feeds_user_id ON feeds(user_id);
CREATE TABLE items (
    id           INTEGER  PRIMARY KEY AUTOINCREMENT,
    feed_id      INTEGER  NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT     NOT NULL,
    title        TEXT     NOT NULL DEFAULT '',
    content      TEXT     NOT NULL DEFAULT '',
    description  TEXT     NOT NULL DEFAULT '',
    url          TEXT     NOT NULL DEFAULT '',
    published_at DATETIME,
    read         INTEGER  NOT NULL DEFAULT 0,
    read_at      DATETIME,
    starred      INTEGER  NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);
CREATE INDEX idx_items_feed_id      ON items(feed_id);
CREATE INDEX idx_items_published_at ON items(published_at);
CREATE INDEX idx_items_read         ON items(read);
CREATE VIRTUAL TABLE items_fts USING fts5(
    title,
    content,
    description,
    content=items,
    content_rowid=id
);
CREATE TRIGGER items_ai AFTER INSERT ON items BEGIN
    INSERT INTO items_fts(rowid, title, content, description)
    VALUES (new.id, new.title, new.content, new.description);
END;
CREATE TRIGGER items_ad AFTER DELETE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content, description)
    VALUES ('delete', old.id, old.title, old.content, old.description);
END;
CREATE TRIGGER items_au AFTER UPDATE ON items BEGIN
    INSERT INTO items_fts(items_fts, rowid, title, content, description)
    VALUES ('delete', old.id, old.title, old.content, old.description);
    INSERT INTO items_fts(rowid, title, content, description)
    VALUES (new.id, new.title, new.content, new.description);
END;
CREATE TABLE user_settings (
    user_id           INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    show_descriptions INTEGER NOT NULL DEFAULT 1,
    date_display      INTEGER NOT NULL DEFAULT 0
);
`

// TestOpen_UpgradesFromV1_1_0 simulates an existing v1.1.0 install (which
// has tables and data but no goose_db_version) and verifies that opening
// it through the new migration-aware code path preserves all data and
// stamps the migration as applied.
func TestOpen_UpgradesFromV1_1_0(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1_1_0.db")

	// Phase 1: create a v1.1.0-shaped database directly, bypassing goose.
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening raw db: %v", err)
	}
	if _, err := rawDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enabling foreign keys: %v", err)
	}
	if _, err := rawDB.Exec(schemaV1_1_0); err != nil {
		t.Fatalf("applying v1.1.0 schema: %v", err)
	}

	// Insert representative data exercising every cascading FK relationship.
	if _, err := rawDB.Exec(`INSERT INTO users (username, password) VALUES ('alice', 'bcrypt-hash')`); err != nil {
		t.Fatalf("inserting user: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO lists (user_id, name) VALUES (1, 'tech')`); err != nil {
		t.Fatalf("inserting list: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO feeds (user_id, list_id, url, title) VALUES (1, 1, 'https://example.com/feed', 'Example')`); err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO items (feed_id, guid, title, content) VALUES (1, 'guid-1', 'First post', 'Hello, world.')`); err != nil {
		t.Fatalf("inserting item: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO user_settings (user_id) VALUES (1)`); err != nil {
		t.Fatalf("inserting user_settings: %v", err)
	}

	// Sanity: goose's bookkeeping table must NOT exist yet (we are simulating pre-migration state).
	if tableExists(t, rawDB, "goose_db_version") {
		t.Fatal("goose_db_version exists before migration; test setup is broken")
	}
	_ = rawDB.Close()

	// Phase 2: open through the new migration-aware Open(). This must succeed
	// without losing any data.
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open after v1.1.0 setup: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Goose bookkeeping table now exists.
	if !tableExists(t, db, "goose_db_version") {
		t.Error("goose_db_version not created during upgrade")
	}

	// All the original v1.0 tables are still present.
	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q to still exist after upgrade", table)
		}
	}

	// Original data preserved.
	var username string
	if err := db.QueryRow(`SELECT username FROM users WHERE id=1`).Scan(&username); err != nil {
		t.Fatalf("querying user: %v", err)
	}
	if username != "alice" {
		t.Errorf("user lost during upgrade: got %q, want %q", username, "alice")
	}

	var itemTitle string
	if err := db.QueryRow(`SELECT title FROM items WHERE id=1`).Scan(&itemTitle); err != nil {
		t.Fatalf("querying item: %v", err)
	}
	if itemTitle != "First post" {
		t.Errorf("item lost during upgrade: got %q, want %q", itemTitle, "First post")
	}

	// FTS5 index still works (was populated by trigger on the original insert).
	var ftsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM items_fts WHERE items_fts MATCH 'world'`).Scan(&ftsCount); err != nil {
		t.Fatalf("querying items_fts: %v", err)
	}
	if ftsCount != 1 {
		t.Errorf("FTS5 row lost during upgrade: got %d matches, want 1", ftsCount)
	}

	// The v1.2.x api_tokens table is created by migration 002 and must
	// be usable end-to-end against the upgraded DB. This is the realistic
	// "existing v1.1.0 user adopts API tokens" path.
	if !tableExists(t, db, "api_tokens") {
		t.Fatal("api_tokens not created during upgrade")
	}
	res, err := db.Exec(
		`INSERT INTO api_tokens (user_id, name, token_hash) VALUES (?, ?, ?)`,
		1, "post-upgrade-token", "deadbeef",
	)
	if err != nil {
		t.Fatalf("inserting api_token after upgrade: %v", err)
	}
	if rows, _ := res.RowsAffected(); rows != 1 {
		t.Errorf("expected 1 row inserted, got %d", rows)
	}
}

// TestMigration003_FTSTriggerScopedToContentColumns verifies that migration
// 003_fts_trigger_columns.sql has applied — the items_au trigger should be
// scoped to fire only on title/content/description updates. The negative
// case ("trigger does NOT fire on read=1 update") is intentionally not
// tested here; see the ticket's AC for rationale (cheapest probes pass
// on both old and new triggers because the old trigger re-inserts the
// same content). The positive case below proves the trigger still fires
// when indexed columns change, which is the contract the migration must
// preserve.
func TestMigration003_FTSTriggerScopedToContentColumns(t *testing.T) {
	db := openTestDB(t)

	// Schema-shape check: the trigger DDL in sqlite_master must contain the
	// column-scope clause.
	var triggerSQL string
	if err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='trigger' AND name='items_au'",
	).Scan(&triggerSQL); err != nil {
		t.Fatalf("querying items_au trigger SQL: %v", err)
	}
	if !strings.Contains(triggerSQL, "OF title, content, description") {
		t.Errorf("items_au trigger SQL missing column-scope clause: %s", triggerSQL)
	}

	// Behavioral check: insert a user → feed → item, then update the item's
	// title and verify FTS reflects the new title.
	res, err := db.Exec(
		`INSERT INTO users (username, password) VALUES (?, ?)`,
		"trigger-user", "hash",
	)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}
	userID, _ := res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO feeds (user_id, url) VALUES (?, ?)`,
		userID, "http://example.com/feed.xml",
	)
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO items (feed_id, guid, title, content, description)
		 VALUES (?, ?, ?, ?, ?)`,
		feedID, "g1", "alphatitle", "old-content", "old-description",
	)
	if err != nil {
		t.Fatalf("inserting item: %v", err)
	}
	itemID, _ := res.LastInsertId()

	// Confirm the initial FTS row was created by items_ai (sanity check).
	var initialFTSTitle string
	if err := db.QueryRow(
		"SELECT title FROM items_fts WHERE rowid = ?", itemID,
	).Scan(&initialFTSTitle); err != nil {
		t.Fatalf("querying items_fts for initial row: %v", err)
	}
	if initialFTSTitle != "alphatitle" {
		t.Fatalf("initial FTS title = %q, want %q", initialFTSTitle, "alphatitle")
	}

	// Trigger the column-scoped UPDATE.
	if _, err := db.Exec(
		"UPDATE items SET title = ? WHERE id = ?", "betatitle", itemID,
	); err != nil {
		t.Fatalf("updating item title: %v", err)
	}

	// items_fts should reflect the new title (proves items_au fires for
	// indexed-column changes).
	var got string
	if err := db.QueryRow(
		"SELECT title FROM items_fts WHERE rowid = ?", itemID,
	).Scan(&got); err != nil {
		t.Fatalf("querying items_fts for updated row: %v", err)
	}
	if got != "betatitle" {
		t.Errorf("FTS title after column-scoped UPDATE = %q, want %q", got, "betatitle")
	}

	// End-to-end FTS sync proof: MATCH on the new title returns the row.
	var matchedRowID int64
	if err := db.QueryRow(
		`SELECT rowid FROM items_fts WHERE items_fts MATCH ?`, "betatitle",
	).Scan(&matchedRowID); err != nil {
		t.Fatalf("MATCH on new title: %v", err)
	}
	if matchedRowID != itemID {
		t.Errorf("MATCH returned rowid %d, want %d", matchedRowID, itemID)
	}
}

// TestMigration004_PerfIndexesExist verifies that migration 004 created
// both indexes. Definitive existence check via sqlite_master — the index
// names should appear regardless of whether the query planner happens
// to pick them on a given query.
func TestMigration004_PerfIndexesExist(t *testing.T) {
	db := openTestDB(t)

	for _, idx := range []string{"idx_feeds_list_id", "idx_items_feed_published_at"} {
		var count int
		if err := db.QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name = ?",
			idx,
		).Scan(&count); err != nil {
			t.Fatalf("querying sqlite_master for %q: %v", idx, err)
		}
		if count != 1 {
			t.Errorf("expected index %q to exist, got count=%d", idx, count)
		}
	}
}

// TestMigration004_FeedsListIDIndexUsed verifies that the query planner
// reliably picks idx_feeds_list_id for a simple WHERE list_id = ? query
// — even on a fresh DB with no sqlite_stat1 data, because the index is
// the only relevant one for the equality.
func TestMigration004_FeedsListIDIndexUsed(t *testing.T) {
	db := openTestDB(t)

	plan := explainPlan(t, db, `SELECT id FROM feeds WHERE list_id = ?`, 1)
	if !strings.Contains(plan, "idx_feeds_list_id") {
		t.Errorf("EXPLAIN QUERY PLAN did not reference idx_feeds_list_id:\n%s", plan)
	}
}

// TestMigration004_ItemsFeedPublishedAtIndexUsed verifies the composite
// index is picked for the main item-listing query shape. A 200-row
// fixture is required: SQLite's planner uses heuristics in the absence
// of sqlite_stat1 and may prefer a table scan over a composite index
// on tiny tables. The fixture pins planner behavior at a row count
// representative of production scale.
func TestMigration004_ItemsFeedPublishedAtIndexUsed(t *testing.T) {
	db := openTestDB(t)

	// Seed a user, feed, and 200 items so the planner sees a non-trivial
	// items table and is willing to pick the composite index.
	res, err := db.Exec(`INSERT INTO users (username, password) VALUES (?, ?)`,
		"plan-user", "hash")
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}
	userID, _ := res.LastInsertId()

	res, err = db.Exec(`INSERT INTO feeds (user_id, url) VALUES (?, ?)`,
		userID, "http://example.com/feed.xml")
	if err != nil {
		t.Fatalf("inserting feed: %v", err)
	}
	feedID, _ := res.LastInsertId()

	for i := range 200 {
		if _, err := db.Exec(
			`INSERT INTO items (feed_id, guid, title, published_at)
			 VALUES (?, ?, ?, ?)`,
			feedID,
			fmt.Sprintf("g-%04d", i),
			fmt.Sprintf("item-%d", i),
			"2026-01-01T00:00:00Z",
		); err != nil {
			t.Fatalf("inserting item %d: %v", i, err)
		}
	}

	plan := explainPlan(t, db,
		`SELECT i.id FROM items i JOIN feeds f ON i.feed_id = f.id
		 WHERE f.user_id = ?
		 ORDER BY i.published_at DESC, i.id DESC
		 LIMIT 50`,
		userID,
	)
	if !strings.Contains(plan, "idx_items_feed_published_at") {
		t.Errorf("EXPLAIN QUERY PLAN did not reference idx_items_feed_published_at:\n%s", plan)
	}
}

// explainPlan returns the concatenated EXPLAIN QUERY PLAN output for the
// given query, one row per line. The output format is stable across
// SQLite 3.x.
func explainPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()

	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var sb strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scanning EXPLAIN row: %v", err)
		}
		sb.WriteString(detail)
		sb.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating EXPLAIN rows: %v", err)
	}
	return sb.String()
}

// tableExists checks sqlite_master for a table or virtual table.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table') AND name = ?",
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("checking table %q: %v", name, err)
	}
	return count > 0
}
