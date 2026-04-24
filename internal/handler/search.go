package handler

import (
	"database/sql"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/sanitize"
)

// searchResult extends model.Item with highlighted snippets from FTS5.
type searchResult struct {
	model.Item
	TitleSnippet   template.HTML
	ContentSnippet template.HTML
}

// searchPageData carries data for the search page template.
type searchPageData struct {
	Query               string
	Results             []searchResult
	TotalResults        int
	Page                int
	TotalPages          int
	Error               string
	DateDisplayAbsolute bool
}

const searchErrMsg = `Invalid search query. Avoid special characters like ", *, (, ).`

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	htmx := isHTMXRequest(r)

	renderSearchErr := func() {
		data := searchPageData{Query: q, Error: searchErrMsg}
		if htmx {
			s.renderFragment(w, "search_results.html", data)
			return
		}
		s.render(w, r, "search.html", PageData{Content: data})
	}

	// No query: render empty search form.
	if q == "" {
		data := searchPageData{}
		if htmx {
			s.renderFragment(w, "search_results.html", data)
			return
		}
		s.render(w, r, "search.html", PageData{Content: data})
		return
	}

	// Parse page number.
	page := pageFromQuery(r)

	// Count total matching results.
	countQuery := `SELECT COUNT(*)
		FROM items_fts
		JOIN items i ON items_fts.rowid = i.id
		JOIN feeds f ON i.feed_id = f.id
		WHERE items_fts MATCH ? AND f.user_id = ?`

	var totalResults int
	if err := s.db.QueryRow(countQuery, q, user.ID).Scan(&totalResults); err != nil {
		slog.Warn("search count query failed", "query", q, "error", err)
		renderSearchErr()
		return
	}

	var totalPages, offset int
	page, totalPages, offset = paginate(totalResults, itemsPerPage, page)

	// Query FTS5 with snippets.
	searchQuery := `SELECT i.id, i.title, i.url, i.published_at, i.read,
		i.starred,
		f.title AS feed_title,
		snippet(items_fts, 0, '[[HIGHLIGHT]]', '[[/HIGHLIGHT]]', '...', 30) AS title_snippet,
		snippet(items_fts, 1, '[[HIGHLIGHT]]', '[[/HIGHLIGHT]]', '...', 60) AS content_snippet
		FROM items_fts
		JOIN items i ON items_fts.rowid = i.id
		JOIN feeds f ON i.feed_id = f.id
		WHERE items_fts MATCH ? AND f.user_id = ?
		ORDER BY rank
		LIMIT ? OFFSET ?`

	rows, err := s.db.Query(searchQuery, q, user.ID, itemsPerPage, offset)
	if err != nil {
		slog.Warn("search query failed", "query", q, "error", err)
		renderSearchErr()
		return
	}
	defer func() { _ = rows.Close() }()

	var results []searchResult
	for rows.Next() {
		var sr searchResult
		var publishedAt sql.NullString
		var read, starred sqlBool
		var titleSnippet, contentSnippet string
		if err := rows.Scan(
			&sr.ID, &sr.Title, &sr.URL, &publishedAt, &read,
			&starred,
			&sr.FeedTitle, &titleSnippet, &contentSnippet,
		); err != nil {
			s.internalError(w, r, "scanning search result row", err)
			return
		}
		sr.PublishedAt = parseTime(publishedAt)
		sr.Read = bool(read)
		sr.Starred = bool(starred)
		sr.TitleSnippet = sanitize.Snippet(titleSnippet)
		sr.ContentSnippet = sanitize.Snippet(contentSnippet)
		results = append(results, sr)
	}
	if err := rows.Err(); err != nil {
		s.internalError(w, r, "iterating search result rows", err)
		return
	}

	settings := queryUserSettings(s.db, user.ID)

	data := searchPageData{
		Query:               q,
		Results:             results,
		TotalResults:        totalResults,
		Page:                page,
		TotalPages:          totalPages,
		DateDisplayAbsolute: settings.DateDisplayAbsolute,
	}

	if htmx {
		s.renderFragment(w, "search_results.html", data)
		return
	}

	s.render(w, r, "search.html", PageData{Content: data})
}
