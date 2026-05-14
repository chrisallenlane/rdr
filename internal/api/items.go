package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/chrisallenlane/rdr/internal/dbutil"
)

// ListItems implements GET /api/v1/items.
func (s *Server) ListItems(w http.ResponseWriter, r *http.Request, params ListItemsParams) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	feedID := derefInt64(params.FeedId)
	listID := derefInt64(params.ListId)
	unread := derefBool(params.Unread)
	starred := derefBool(params.Starred)
	page := derefInt(params.Page)
	if page < 1 {
		page = 1
	}

	where, args := dbutil.BuildItemFilter(uid, feedID, listID, unread, starred)

	var total int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM items i JOIN feeds f ON i.feed_id = f.id WHERE "+where,
		args...,
	).Scan(&total); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	page, _, offset := effectivePage(total, page)

	rows, err := s.db.Query(
		`SELECT i.id, i.feed_id, i.title,
		        COALESCE(NULLIF(i.description, ''), '') AS description,
		        i.url, i.published_at, i.read, i.starred,
		        f.title, f.site_url, f.url
		   FROM items i
		   JOIN feeds f ON i.feed_id = f.id
		  WHERE `+where+`
		  ORDER BY i.published_at DESC, i.id DESC
		  LIMIT ? OFFSET ?`,
		append(args, pageSize, offset)...,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]Item, 0, pageSize)
	for rows.Next() {
		it, err := scanItemRow(rows, false)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "", "", "")
			return
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	writePagination(w, r, total, page)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// GetItem implements GET /api/v1/items/{id}.
func (s *Server) GetItem(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	row := s.db.QueryRow(
		`SELECT i.id, i.feed_id, i.title, i.description, i.url, i.published_at,
		        i.read, i.starred, f.title, f.site_url, f.url, i.content
		   FROM items i
		   JOIN feeds f ON i.feed_id = f.id
		  WHERE i.id = ? AND f.user_id = ?`,
		id, uid,
	)
	it, err := scanItemRow(row, true)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeProblem(w, http.StatusNotFound, "", "Not Found", "")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(it)
}

// StarItem implements PUT /api/v1/items/{id}/star.
func (s *Server) StarItem(w http.ResponseWriter, r *http.Request, id IDPath) {
	s.toggleStar(w, r, id, true)
}

// UnstarItem implements DELETE /api/v1/items/{id}/star.
func (s *Server) UnstarItem(w http.ResponseWriter, r *http.Request, id IDPath) {
	s.toggleStar(w, r, id, false)
}

func (s *Server) toggleStar(w http.ResponseWriter, r *http.Request, id IDPath, starred bool) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	val := 0
	if starred {
		val = 1
	}
	res, err := s.db.Exec(
		`UPDATE items SET starred = ?
		   WHERE id = ?
		     AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`,
		val, id, uid,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Could be: item doesn't exist; belongs to another user; or already
		// in the desired state. The first two warrant 404; the third 204.
		// Distinguish by an existence check scoped to the caller.
		var owned int
		_ = s.db.QueryRow(
			`SELECT 1 FROM items i JOIN feeds f ON i.feed_id = f.id
			   WHERE i.id = ? AND f.user_id = ?`,
			id, uid,
		).Scan(&owned)
		if owned == 0 {
			writeProblem(w, http.StatusNotFound, "", "Not Found", "")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// MarkItemsRead implements POST /api/v1/items/mark-read.
func (s *Server) MarkItemsRead(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var body MarkReadRequest
	// Empty body is allowed and means "mark all of caller's items".
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}

	if body.FeedId != nil && body.ListId != nil {
		writeProblem(w, http.StatusBadRequest, "", "",
			"feed_id and list_id are mutually exclusive")
		return
	}

	where, args := dbutil.BuildItemFilter(
		uid,
		derefInt64(body.FeedId),
		derefInt64(body.ListId),
		derefBool(body.Unread),
		derefBool(body.Starred),
	)

	query := `UPDATE items SET read = 1, read_at = ?
	            WHERE read = 0
	              AND id IN (
	                SELECT i.id FROM items i
	                  JOIN feeds f ON i.feed_id = f.id
	                 WHERE ` + where + `
	              )`
	fullArgs := append([]any{time.Now().UTC().Format(time.RFC3339)}, args...)

	res, err := s.db.Exec(query, fullArgs...)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	n, _ := res.RowsAffected()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(MarkReadResponse{Marked: int(n)})
}

// rowScanner abstracts *sql.Row and *sql.Rows so scanItemRow can serve
// both single-row Get and multi-row List paths.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanItemRow reads a single Item from a row. If withContent is true,
// the trailing column is content (the full sanitized HTML body); else
// content is omitted (list view).
func scanItemRow(row rowScanner, withContent bool) (Item, error) {
	var (
		id, feedID                   int64
		title, description, url      string
		feedTitle, feedSite, feedURL string
		readInt, starredInt          int
		publishedAt                  sql.NullString
		content                      sql.NullString
	)

	dest := []any{
		&id, &feedID, &title, &description, &url, &publishedAt,
		&readInt, &starredInt, &feedTitle, &feedSite, &feedURL,
	}
	if withContent {
		dest = append(dest, &content)
	}

	if err := row.Scan(dest...); err != nil {
		return Item{}, err
	}

	it := Item{
		Id:          id,
		FeedId:      feedID,
		Title:       title,
		Url:         url,
		Read:        readInt != 0,
		Starred:     starredInt != 0,
		FeedTitle:   feedTitle,
		FeedSiteUrl: ptrIfNotEmpty(feedSite),
		FeedUrl:     ptrIfNotEmpty(feedURL),
	}
	if description != "" {
		it.Description = &description
	}
	if withContent && content.Valid && content.String != "" {
		it.Content = &content.String
	}
	if publishedAt.Valid {
		t, perr := parseSQLiteTimestamp(publishedAt.String)
		if perr == nil {
			it.PublishedAt = &t
		}
	}
	return it, nil
}

// ptrIfNotEmpty returns &s if s != "", else nil. Used to map empty SQL
// strings (DEFAULT ” columns) to JSON omission.
func ptrIfNotEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func derefInt(p *PageParam) int {
	if p == nil {
		return 0
	}
	return int(*p)
}
