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
