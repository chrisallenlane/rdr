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

	// Set pragmas via Exec (modernc.org/sqlite does not support DSN pragmas).
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting journal_mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting foreign_keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	// NORMAL fsyncs the WAL only at checkpoint, not on every commit. Safe
	// under WAL mode: durable up to the last commit before a crash.
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting synchronous: %w", err)
	}
	// 64 MiB page cache (negative value = KiB; -65536 KiB = 64 MiB).
	if _, err := db.Exec("PRAGMA cache_size=-65536"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting cache_size: %w", err)
	}
	// In-memory temp store for sorts/groupings (ORDER BY pagination, etc.).
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting temp_store: %w", err)
	}
	// 256 MiB mmap hint. SQLite/the VFS may cap or ignore this; setting it
	// is cheap insurance and never errors on platforms where mmap is unused.
	if _, err := db.Exec("PRAGMA mmap_size=268435456"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("setting mmap_size: %w", err)
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
