package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// expectedTables lists every table (including virtual) that the initial
// migration should create.
var expectedTables = []string{
	"users",
	"sessions",
	"feeds",
	"items",
	"lists",
	"list_feeds",
	"items_fts",
	"user_settings",
	"schema_migrations",
}

func TestOpen_CreatesAllTables(t *testing.T) {
	db := openTestDB(t)

	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

func TestOpen_MigrationIdempotency(t *testing.T) {
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

	// Verify schema_migrations has exactly one row per migration file.
	// Update this constant each time a new migration file is added.
	const wantMigrations = 2
	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("querying schema_migrations: %v", err)
	}
	if count != wantMigrations {
		t.Errorf("expected %d migration records, got %d", wantMigrations, count)
	}
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

func TestMigrationApplied_UnknownVersion(t *testing.T) {
	db := openTestDB(t)

	// Version 9999 has never been applied; migrationApplied must return false.
	applied, err := migrationApplied(db, 9999)
	if err != nil {
		t.Fatalf("migrationApplied: %v", err)
	}
	if applied {
		t.Error("expected migrationApplied to return false for unknown version 9999, got true")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid", "001_initial.sql", 1, false},
		{"multi_digit", "123_foo.sql", 123, false},
		{"no_underscore", "nounderscore", 0, true},
		{"empty", "", 0, true},
		{"non_numeric_prefix", "abc_foo.sql", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
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

func FuzzParseVersion(f *testing.F) {
	f.Add("001_initial.sql")
	f.Add("002_add_users.sql")
	f.Add("")
	f.Add("notanumber_foo.sql")
	f.Add("___")
	f.Add("999999999999999999999_overflow.sql")
	f.Fuzz(func(t *testing.T, name string) {
		// Must not panic
		_, _ = parseVersion(name)
	})
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
