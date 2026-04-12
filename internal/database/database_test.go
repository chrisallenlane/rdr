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
