// Package database manages SQLite database connections and schema migrations.
package database

import (
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens a SQLite database at the given path, sets recommended pragmas,
// runs any pending migrations, and returns the ready-to-use *sql.DB.
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

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// migrate applies any unapplied SQL migration files to the database.
func migrate(db *sql.DB) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	// Collect and sort migration filenames lexically.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	slices.Sort(files)

	for _, name := range files {
		version, err := parseVersion(name)
		if err != nil {
			return fmt.Errorf("parsing migration version from %q: %w", name, err)
		}

		applied, err := migrationApplied(db, version)
		if err != nil {
			return fmt.Errorf("checking migration %d: %w", version, err)
		}
		if applied {
			slog.Debug("migration already applied", "version", version)
			continue
		}

		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", name, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("beginning transaction for migration %d: %w", version, err)
		}

		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("executing migration %d: %w", version, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version) VALUES (?)", version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("recording migration %d: %w", version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %d: %w", version, err)
		}

		slog.Info("applied migration", "version", version, "file", name)
	}

	return nil
}

// migrationApplied checks whether a given migration version has already been
// recorded. It handles the bootstrap case where the schema_migrations table
// does not yet exist.
func migrationApplied(db *sql.DB, version int) (bool, error) {
	// Check whether the schema_migrations table exists at all.
	var count int
	err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'",
	).Scan(&count)
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}

	err = db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// parseVersion extracts the leading integer from a migration filename
// like "001_initial.sql" -> 1.
func parseVersion(name string) (int, error) {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0, fmt.Errorf("no underscore in filename %q", name)
	}
	return strconv.Atoi(parts[0])
}
