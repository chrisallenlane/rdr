// Package database manages SQLite database connections and schema initialization.
package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema []byte

// Open opens a SQLite database at the given path, sets recommended pragmas,
// initializes the schema on first use, and returns the ready-to-use *sql.DB.
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

	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	return db, nil
}

// initSchema creates all tables on first use. It is a no-op when the schema
// is already present (detected by checking for the users table).
func initSchema(db *sql.DB) error {
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'",
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking for users table: %w", err)
	}
	if count > 0 {
		slog.Debug("schema already initialized, skipping")
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning schema transaction: %w", err)
	}

	if _, err := tx.Exec(string(schema)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("executing schema: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing schema: %w", err)
	}

	slog.Info("database schema initialized")
	return nil
}
