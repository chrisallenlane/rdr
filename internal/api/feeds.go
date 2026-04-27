package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/chrisallenlane/rdr/internal/dbutil"
	"github.com/chrisallenlane/rdr/internal/discover"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/poller"
)

// listFeedsQuery selects every column needed to build a Feed response,
// scoped to the calling user. item_count and unread_count are computed
// via correlated subqueries.
const listFeedsQuery = `
SELECT f.id, f.list_id, f.url, f.title, f.site_url, f.favicon_url,
       f.last_fetched_at, f.last_fetch_error, f.consecutive_failures,
       f.created_at,
       (SELECT COUNT(*) FROM items i WHERE i.feed_id = f.id) AS item_count,
       (SELECT COUNT(*) FROM items i WHERE i.feed_id = f.id AND i.read = 0) AS unread_count
  FROM feeds f
 WHERE f.user_id = ?`

// ListFeeds implements GET /api/v1/feeds.
func (s *Server) ListFeeds(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := s.db.Query(listFeedsQuery+" ORDER BY f.title ASC, f.url ASC", uid)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]Feed, 0)
	for rows.Next() {
		f, err := scanFeedRow(rows)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "", "", "")
			return
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GetFeed implements GET /api/v1/feeds/{id}.
func (s *Server) GetFeed(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	row := s.db.QueryRow(listFeedsQuery+" AND f.id = ?", uid, id)
	f, err := scanFeedRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeProblem(w, http.StatusNotFound, "", "Not Found", "")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(f)
}

// AddFeed implements POST /api/v1/feeds. The discovery + insert is
// synchronous; the initial fetch is fire-and-forget so the response
// returns as soon as the row exists.
func (s *Server) AddFeed(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var body AddFeedRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	parsed, err := url.Parse(body.Url)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		writeProblem(w, http.StatusBadRequest, "", "", "url must use http or https")
		return
	}

	feedURL, err := s.feedResolver(r.Context(), body.Url)
	if err != nil {
		if errors.Is(err, discover.ErrNoFeedFound) {
			writeProblem(w, http.StatusUnprocessableEntity, "", "",
				"could not find an RSS or Atom feed at this URL")
			return
		}
		writeProblem(w, http.StatusUnprocessableEntity, "", "",
			fmt.Sprintf("could not fetch the URL: %v", err))
		return
	}

	res, err := s.db.Exec(
		`INSERT INTO feeds (user_id, url) VALUES (?, ?)`,
		uid, feedURL,
	)
	if err != nil {
		if dbutil.IsUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "", "",
				"this feed is already subscribed")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	feedID, err := res.LastInsertId()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	// Fire-and-forget the initial fetch. The new feed row is returned
	// immediately so callers don't block on remote IO.
	go func(feedID int64, feedURL string, faviconsDir string, db *sql.DB) {
		feed := &model.Feed{ID: feedID, UserID: uid, URL: feedURL}
		if err := poller.FetchAndStoreFeed(context.Background(), db, feed, faviconsDir); err != nil {
			slog.Warn("api: initial feed fetch failed",
				"feed_id", feedID, "url", feedURL, "error", err)
		}
	}(feedID, feedURL, s.faviconsDir, s.db)

	// Read back the freshly-inserted row so the response carries the
	// authoritative server-side timestamps, defaulted strings, etc.
	row := s.db.QueryRow(listFeedsQuery+" AND f.id = ?", uid, feedID)
	f, err := scanFeedRow(row)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(f)
}

// DeleteFeed implements DELETE /api/v1/feeds/{id}.
func (s *Server) DeleteFeed(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	res, err := s.db.Exec(
		`DELETE FROM feeds WHERE id = ? AND user_id = ?`,
		id, uid,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeProblem(w, http.StatusNotFound, "", "Not Found", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SyncFeeds implements POST /api/v1/feeds/sync. The trigger is
// best-effort: a sync that is already in progress is left running.
func (s *Server) SyncFeeds(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if s.syncFeeds != nil {
		_ = s.syncFeeds(r.Context())
	}
	w.WriteHeader(http.StatusAccepted)
}

// GetSyncStatus implements GET /api/v1/feeds/sync/status.
func (s *Server) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	syncing := s.syncStatus != nil && s.syncStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SyncStatus{Syncing: syncing})
}

// scanFeedRow reads a single Feed from a row produced by listFeedsQuery.
// The trailing two columns are item_count and unread_count.
func scanFeedRow(row rowScanner) (Feed, error) {
	var (
		id                     int64
		listID                 sql.NullInt64
		feedURL, title         string
		siteURL, faviconURL    sql.NullString
		lastFetchedAt          sql.NullString
		lastFetchError         sql.NullString
		consecutiveFailures    int
		createdAtRaw           string
		itemCount, unreadCount int
	)

	if err := row.Scan(
		&id, &listID, &feedURL, &title, &siteURL, &faviconURL,
		&lastFetchedAt, &lastFetchError, &consecutiveFailures,
		&createdAtRaw, &itemCount, &unreadCount,
	); err != nil {
		return Feed{}, err
	}

	createdAt, err := parseSQLiteTimestamp(createdAtRaw)
	if err != nil {
		return Feed{}, err
	}

	f := Feed{
		Id:                  id,
		Url:                 feedURL,
		Title:               title,
		CreatedAt:           createdAt,
		ConsecutiveFailures: &consecutiveFailures,
		ItemCount:           itemCount,
		UnreadCount:         unreadCount,
	}
	if listID.Valid {
		v := listID.Int64
		f.ListId = &v
	}
	if siteURL.Valid && siteURL.String != "" {
		v := siteURL.String
		f.SiteUrl = &v
	}
	if faviconURL.Valid && faviconURL.String != "" {
		v := faviconURL.String
		f.FaviconUrl = &v
	}
	if lastFetchError.Valid && lastFetchError.String != "" {
		v := lastFetchError.String
		f.LastFetchError = &v
	}
	if lastFetchedAt.Valid && lastFetchedAt.String != "" {
		t, perr := parseSQLiteTimestamp(lastFetchedAt.String)
		if perr == nil {
			f.LastFetchedAt = &t
		}
	}
	return f, nil
}
