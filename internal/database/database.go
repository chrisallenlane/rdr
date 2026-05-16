// Package database manages SQLite database connections and migrations.
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens a SQLite database at the given path, sets recommended pragmas,
// applies any pending migrations, and returns the ready-to-use *sql.DB.
func Open(databasePath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Pragmas applied at open. modernc.org/sqlite does not support DSN
	// pragmas, so they must be set via Exec. Each entry's "why" is
	// captured inline so the next maintainer can read intent without
	// chasing commit history.
	pragmas := []struct{ name, stmt string }{
		{"journal_mode", "PRAGMA journal_mode=WAL"},
		{"foreign_keys", "PRAGMA foreign_keys=ON"},
		// 5000ms covers ordinary contention; without it, SQLite returns
		// SQLITE_BUSY immediately on any concurrent access.
		{"busy_timeout", "PRAGMA busy_timeout=5000"},
		// NORMAL fsyncs the WAL only at checkpoint, not on every commit;
		// safe under WAL mode (durable up to the last commit before crash).
		{"synchronous", "PRAGMA synchronous=NORMAL"},
		// 64 MiB page cache (negative value = KiB; -65536 KiB = 64 MiB).
		{"cache_size", "PRAGMA cache_size=-65536"},
		// In-memory temp store for sorts/groupings (ORDER BY pagination, etc.).
		{"temp_store", "PRAGMA temp_store=MEMORY"},
		// 256 MiB mmap hint. SQLite/the VFS may cap or ignore this;
		// setting it is cheap insurance and never errors on platforms
		// where mmap is unused.
		{"mmap_size", "PRAGMA mmap_size=268435456"},
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p.stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("setting %s: %w", p.name, err)
		}
	}

	// Limit to one connection so all operations share the pragmas above.
	// SQLite only allows one writer at a time anyway; serializing at the Go
	// level avoids SQLITE_BUSY when multiple goroutines access the database.
	db.SetMaxOpenConns(1)

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// runMigrations applies any pending up migrations from the embedded
// migrations/ directory. Migrations are up-only by convention; rollback is
// performed by restoring from backup.
func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	prev, _ := goose.GetDBVersion(db)
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	curr, _ := goose.GetDBVersion(db)
	if curr > prev {
		slog.Info("database migrated", "from", prev, "to", curr)
	}
	return nil
}
