package database

import (
	"database/sql"
	"os"
	"path/filepath"
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
