package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// isUniqueViolation reports whether err is a SQLite uniqueness constraint
// violation. It matches both SQLITE_CONSTRAINT_UNIQUE (triggered by an explicit
// UNIQUE constraint) and SQLITE_CONSTRAINT_PRIMARYKEY (triggered by a duplicate
// composite primary key, e.g. the list_feeds join table).
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	c := sqliteErr.Code()
	return c == sqlitelib.SQLITE_CONSTRAINT_UNIQUE ||
		c == sqlitelib.SQLITE_CONSTRAINT_PRIMARYKEY
}

// sqlBool is an int that converts to bool when scanned. SQLite stores booleans
// as integers (0/1); this type avoids the repeated var readInt int / != 0 pattern.
type sqlBool bool

func (b *sqlBool) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*b = v != 0
	case nil:
		*b = false
	default:
		return fmt.Errorf("sqlBool: unsupported type %T", src)
	}
	return nil
}

// allowedTables is the set of table names accepted by verifyOwnership and
// deleteByID. This prevents table names from being supplied dynamically in a
// way that could lead to SQL injection.
var allowedTables = map[string]bool{
	"feeds": true,
	"lists": true,
}

// scanFeeds scans rows of (id, title, url) into a []model.Feed slice.
func scanFeeds(rows *sql.Rows) ([]model.Feed, error) {
	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		if err := rows.Scan(&f.ID, &f.Title, &f.URL); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// queryUserFeeds returns the id, title, url, and unread count of every feed
// owned by userID, ordered by title. It is used to populate sidebar filter
// links. It does not use scanFeeds because it selects an additional column.
func queryUserFeeds(db *sql.DB, userID int64) ([]model.Feed, error) {
	rows, err := db.Query(
		`SELECT f.id, f.title, f.url,
		        (SELECT COUNT(*) FROM items i WHERE i.feed_id = f.id AND i.read = 0) AS unread_count
		 FROM feeds f
		 WHERE f.user_id = ?
		 ORDER BY f.title`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		if err := rows.Scan(&f.ID, &f.Title, &f.URL, &f.UnreadCount); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// queryUserLists returns the id, name, and unread count of every list owned by
// userID, ordered by name. It is used to populate sidebar filter links.
func queryUserLists(db *sql.DB, userID int64) ([]model.List, error) {
	rows, err := db.Query(
		`SELECT l.id, l.name,
		        (SELECT COUNT(*) FROM items i
		         JOIN list_feeds lf ON lf.feed_id = i.feed_id
		         WHERE lf.list_id = l.id AND i.read = 0) AS unread_count
		 FROM lists l
		 WHERE l.user_id = ?
		 ORDER BY l.name ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var lists []model.List
	for rows.Next() {
		var l model.List
		if err := rows.Scan(&l.ID, &l.Name, &l.UnreadCount); err != nil {
			return nil, err
		}
		lists = append(lists, l)
	}
	return lists, rows.Err()
}

// verifyOwnership checks that a row in table with the given id is owned by
// userID by querying SELECT COUNT(*) FROM <table> WHERE id = ? AND user_id = ?.
// It returns true if the row is found. On error it renders a 500 response; if
// the row is not found it renders a 404 response. In both failure cases it
// returns false and the caller must return immediately.
func (s *Server) verifyOwnership(
	w http.ResponseWriter,
	r *http.Request,
	table string,
	id int64,
	userID int64,
) bool {
	if !allowedTables[table] {
		slog.Error("verifyOwnership called with invalid table", "table", table)
		s.renderInternalError(w, r)
		return false
	}

	var count int
	query := fmt.Sprintf(
		"SELECT COUNT(*) FROM %s WHERE id = ? AND user_id = ?",
		table,
	)
	err := s.db.QueryRow(query, id, userID).Scan(&count)
	if err != nil {
		slog.Error("verifying ownership", "table", table, "id", id, "error", err)
		s.renderInternalError(w, r)
		return false
	}
	if count == 0 {
		s.renderError(
			w, r, http.StatusNotFound,
			fmt.Sprintf("%s not found", table),
		)
		return false
	}
	return true
}

// deleteByID handles the common pattern of deleting a user-owned row by ID,
// setting a flash message, and redirecting. It renders errors directly and
// callers must return immediately after calling it.
func (s *Server) deleteByID(w http.ResponseWriter, r *http.Request, table, entity, redirect string) {
	if !allowedTables[table] {
		slog.Error("deleteByID called with invalid table", "table", table)
		s.renderInternalError(w, r)
		return
	}

	user := middleware.UserFromContext(r.Context())

	id, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	result, err := s.db.Exec(
		fmt.Sprintf("DELETE FROM %s WHERE id = ? AND user_id = ?", table),
		id, user.ID,
	)
	if err != nil {
		slog.Error("deleting "+entity, "error", err)
		s.renderInternalError(w, r)
		return
	}

	rows, err := result.RowsAffected()
	if err != nil {
		slog.Error("checking rows affected", "error", err)
		s.renderInternalError(w, r)
		return
	}
	if rows == 0 {
		s.renderError(w, r, http.StatusNotFound, entity+" not found")
		return
	}

	setFlash(w, entity+" removed.")
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
