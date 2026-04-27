package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/search"
)

// snippetMarkerOpen / snippetMarkerClose are the FTS5 highlight
// sentinels written by the snippet() function and parsed back into
// structured segments here.
const (
	snippetMarkerOpen  = "[[HIGHLIGHT]]"
	snippetMarkerClose = "[[/HIGHLIGHT]]"
)

// Search implements GET /api/v1/search.
func (s *Server) Search(w http.ResponseWriter, r *http.Request, params SearchParams) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	q := strings.TrimSpace(params.Q)
	if q == "" {
		writeProblem(w, http.StatusBadRequest, "", "", "q is required")
		return
	}
	if search.IsRejected(q) {
		writeProblem(w, http.StatusBadRequest, "", "",
			`query may not contain any of: " * ( )`)
		return
	}

	page := derefInt(params.Page)
	if page < 1 {
		page = 1
	}

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*)
		   FROM items_fts
		   JOIN items i ON items_fts.rowid = i.id
		   JOIN feeds f ON i.feed_id = f.id
		  WHERE items_fts MATCH ? AND f.user_id = ?`,
		q, uid,
	).Scan(&total); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	page, _, offset := effectivePage(total, page)

	rows, err := s.db.Query(
		`SELECT i.id, i.feed_id, i.title, i.url, i.published_at, i.read, i.starred,
		        f.title AS feed_title,
		        snippet(items_fts, 0, '`+snippetMarkerOpen+`', '`+snippetMarkerClose+`', '...', 30) AS title_snippet,
		        snippet(items_fts, 1, '`+snippetMarkerOpen+`', '`+snippetMarkerClose+`', '...', 60) AS content_snippet
		   FROM items_fts
		   JOIN items i ON items_fts.rowid = i.id
		   JOIN feeds f ON i.feed_id = f.id
		  WHERE items_fts MATCH ? AND f.user_id = ?
		  ORDER BY rank
		  LIMIT ? OFFSET ?`,
		q, uid, pageSize, offset,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	defer func() { _ = rows.Close() }()

	out := make([]SearchResult, 0, pageSize)
	for rows.Next() {
		var (
			id, feedID                   int64
			title, feedURL, feedTitle    string
			publishedAt                  sql.NullString
			readInt, starredInt          int
			titleSnippet, contentSnippet string
		)
		if err := rows.Scan(
			&id, &feedID, &title, &feedURL, &publishedAt,
			&readInt, &starredInt, &feedTitle,
			&titleSnippet, &contentSnippet,
		); err != nil {
			writeProblem(w, http.StatusInternalServerError, "", "", "")
			return
		}

		sr := SearchResult{
			Id:             id,
			FeedId:         feedID,
			Title:          title,
			Url:            feedURL,
			Read:           readInt != 0,
			Starred:        starredInt != 0,
			FeedTitle:      feedTitle,
			TitleSnippet:   parseSnippet(titleSnippet),
			ContentSnippet: parseSnippet(contentSnippet),
		}
		if publishedAt.Valid {
			if t, perr := parseSQLiteTimestamp(publishedAt.String); perr == nil {
				sr.PublishedAt = &t
			}
		}
		out = append(out, sr)
	}
	if err := rows.Err(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	writePagination(w, r, total, page)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// parseSnippet splits an FTS5 snippet on the [[HIGHLIGHT]] /
// [[/HIGHLIGHT]] sentinels into structured segments. Misnested
// markers are tolerated by treating any text outside a properly
// matched pair as plain text.
func parseSnippet(s string) []SnippetSegment {
	if s == "" {
		return []SnippetSegment{}
	}
	out := make([]SnippetSegment, 0, 4)
	for s != "" {
		open := strings.Index(s, snippetMarkerOpen)
		if open < 0 {
			out = appendSegment(out, s, false)
			break
		}
		if open > 0 {
			out = appendSegment(out, s[:open], false)
		}
		rest := s[open+len(snippetMarkerOpen):]
		close := strings.Index(rest, snippetMarkerClose)
		if close < 0 {
			// Unmatched open — treat the remainder as plain text.
			out = appendSegment(out, rest, false)
			break
		}
		out = appendSegment(out, rest[:close], true)
		s = rest[close+len(snippetMarkerClose):]
	}
	if len(out) == 0 {
		return []SnippetSegment{}
	}
	return out
}

// appendSegment skips empty text fragments and merges consecutive
// segments that share the same highlight state.
func appendSegment(segs []SnippetSegment, text string, highlight bool) []SnippetSegment {
	if text == "" {
		return segs
	}
	if n := len(segs); n > 0 && segs[n-1].Highlight == highlight {
		segs[n-1].Text += text
		return segs
	}
	return append(segs, SnippetSegment{Text: text, Highlight: highlight})
}
