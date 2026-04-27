// Package dbutil contains small database helpers shared by the HTML
// handler and the JSON API. Both layers serve the same data model and
// would otherwise duplicate query construction and error inspection.
package dbutil

import (
	"errors"

	sqlite "modernc.org/sqlite"
)

// BuildItemFilter constructs the WHERE clause and argument list for
// item queries scoped to a user, optionally filtered by feed, list,
// read state, and starred state.
//
// The resulting clause assumes the query joins items with feeds:
//
//	FROM items i JOIN feeds f ON i.feed_id = f.id WHERE <clause>
//
// The list filter additionally guards against IDOR by requiring the
// list to belong to the same user.
func BuildItemFilter(userID, feedID, listID int64, unread, starred bool) (string, []any) {
	where := "f.user_id = ?"
	args := []any{userID}
	if feedID > 0 {
		where += " AND f.id = ?"
		args = append(args, feedID)
	}
	if unread {
		where += " AND i.read = 0"
	}
	if starred {
		where += " AND i.starred = 1"
	}
	if listID > 0 {
		where += " AND f.list_id = ? AND ? IN (SELECT id FROM lists WHERE user_id = ?)"
		args = append(args, listID, listID, userID)
	}
	return where, args
}

// IsUniqueViolation reports whether err is a SQLite UNIQUE constraint
// violation (SQLITE_CONSTRAINT_UNIQUE = 2067).
func IsUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	const sqliteConstraintUnique = 2067
	return sqliteErr.Code() == sqliteConstraintUnique
}
