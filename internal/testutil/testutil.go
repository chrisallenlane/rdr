// Package testutil provides shared test helpers for database-backed tests.
package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisallenlane/rdr/internal/database"
	"github.com/chrisallenlane/rdr/internal/model"
)

// OpenTestDB opens a temporary on-disk SQLite database, applies the schema,
// and registers cleanup with t.Cleanup. The returned *sql.DB is ready to use.
func OpenTestDB(t *testing.T) *sql.DB {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("database.Open(%q): %v", path, err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(path)
	})
	return db
}

// InsertUser inserts a user with the given username (password stored as the
// literal string "hash") and returns the new row's id.
func InsertUser(t *testing.T, db *sql.DB, username string) int64 {
	t.Helper()

	res, err := db.Exec(
		`INSERT INTO users (username, password) VALUES (?, ?)`,
		username, "hash",
	)
	if err != nil {
		t.Fatalf("InsertUser(%q): %v", username, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("InsertUser LastInsertId: %v", err)
	}
	return id
}

// InsertSession inserts a session row for the given user.
func InsertSession(
	t *testing.T,
	db *sql.DB,
	userID int64,
	sessionID string,
	expiresAt time.Time,
) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`,
		sessionID, userID, model.FormatTime(expiresAt),
	)
	if err != nil {
		t.Fatalf("InsertSession(%q): %v", sessionID, err)
	}
}
