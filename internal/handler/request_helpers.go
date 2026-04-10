package handler

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/chrisallenlane/rdr/internal/model"
)

// isHTMXRequest reports whether the request was made by HTMX.
func isHTMXRequest(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// refererPath returns the path component of the request's Referer header,
// or fallback if the header is missing, unparseable, or has an empty path.
// Using only the path prevents open redirects to external sites.
func refererPath(r *http.Request, fallback string) string {
	if parsed, err := url.Parse(r.Header.Get("Referer")); err == nil && parsed.Path != "" {
		return parsed.Path
	}
	return fallback
}

const itemsPerPage = 50

// parseTime parses a DATETIME string from SQLite into a *time.Time.
// Handles both "2006-01-02 15:04:05" and RFC 3339 formats, since the
// modernc.org/sqlite driver may return either depending on how the value
// was stored.
func parseTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	for _, layout := range []string{
		model.SQLiteDatetimeFmt,
		time.RFC3339,
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s.String); err == nil {
			return &t
		}
	}
	return nil
}

// pageFromQuery reads the "page" query parameter from r, returning 1 if
// absent or invalid.
func pageFromQuery(r *http.Request) int {
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		return p
	}
	return 1
}

// parsePositiveInt64 parses a string as int64, returning 0 if the string is
// empty, unparseable, or non-positive.
func parsePositiveInt64(s string) int64 {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// pathInt64 reads a path parameter by key and parses it as int64. If parsing
// fails it renders a 400 Bad Request response and returns (0, false). Callers
// must return immediately when ok is false.
func (s *Server) pathInt64(
	w http.ResponseWriter,
	r *http.Request,
	key string,
) (int64, bool) {
	v, err := strconv.ParseInt(r.PathValue(key), 10, 64)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid ID")
		return 0, false
	}
	return v, true
}

// paginate computes the clamped page number, total page count, and row offset
// for a query result set. total is the full result count, perPage is the page
// size, and requestedPage is the 1-based page number from the request.
func paginate(total, perPage, requestedPage int) (page, totalPages, offset int) {
	totalPages = max((total+perPage-1)/perPage, 1)
	page = max(min(requestedPage, totalPages), 1)
	offset = (page - 1) * perPage
	return
}
