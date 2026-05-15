package poller

import (
	"database/sql"
	"log/slog"
)

// PruneOldItems deletes read items older than retentionDays.
// Returns count of deleted items.
func PruneOldItems(db *sql.DB, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	result, err := db.Exec(`
		DELETE FROM items
		WHERE published_at < datetime('now', '-' || ? || ' days')
		AND read = 1
		AND starred = 0
	`, retentionDays)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// CleanExpiredSessions deletes expired sessions.
func CleanExpiredSessions(db *sql.DB) (int64, error) {
	result, err := db.Exec(
		`DELETE FROM sessions WHERE expires_at < datetime('now')`,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// runRetention prunes old items and cleans expired sessions, logging results.
func runRetention(db *sql.DB, retentionDays int) {
	if retentionDays > 0 {
		n, err := PruneOldItems(db, retentionDays)
		if err != nil {
			slog.Error(
				"retention: failed to prune old items",
				"error", err,
			)
		} else {
			slog.Info(
				"pruned old items",
				"count", n,
				"retention_days", retentionDays,
			)
		}
	} else {
		slog.Info("retention disabled, skipping prune")
	}

	n, err := CleanExpiredSessions(db)
	if err != nil {
		slog.Error(
			"retention: failed to clean expired sessions",
			"error", err,
		)
	} else {
		slog.Info("cleaned expired sessions", "count", n)
	}

	runMaintenance(db)
}

// runMaintenance runs cheap SQLite maintenance tasks. Called from
// runRetention at the end of each poll cycle. PRAGMA optimize runs a
// partial ANALYZE only on tables whose statistics are stale, completing
// in milliseconds on a healthy DB. It's designed to be called regularly.
// Errors are logged at WARN but do not abort the poll cycle.
func runMaintenance(db *sql.DB) {
	if _, err := db.Exec("PRAGMA optimize"); err != nil {
		slog.Warn("maintenance: PRAGMA optimize failed", "error", err)
	}
}
