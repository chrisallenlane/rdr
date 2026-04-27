package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/dbutil"
)

// ListLists implements GET /api/v1/lists.
func (s *Server) ListLists(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	lists, err := s.queryLists(uid, 0)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lists)
}

// GetList implements GET /api/v1/lists/{id}.
func (s *Server) GetList(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	lists, err := s.queryLists(uid, int64(id))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	if len(lists) == 0 {
		writeProblem(w, http.StatusNotFound, "", "Not Found", "")
		return
	}
	list := lists[0]

	rows, err := s.db.Query(listFeedsQuery+" AND f.list_id = ? ORDER BY f.title ASC, f.url ASC", uid, int64(id))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	defer func() { _ = rows.Close() }()

	feeds := make([]Feed, 0)
	for rows.Next() {
		f, err := scanFeedRow(rows)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "", "", "")
			return
		}
		feeds = append(feeds, f)
	}
	if err := rows.Err(); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ListDetail{List: list, Feeds: feeds})
}

// CreateList implements POST /api/v1/lists.
func (s *Server) CreateList(w http.ResponseWriter, r *http.Request) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var body CreateListRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeProblem(w, http.StatusBadRequest, "", "", "name is required")
		return
	}

	res, err := s.db.Exec(
		`INSERT INTO lists (user_id, name) VALUES (?, ?)`,
		uid, name,
	)
	if err != nil {
		if dbutil.IsUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "", "", "a list with that name already exists")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	listID, err := res.LastInsertId()
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	lists, err := s.queryLists(uid, listID)
	if err != nil || len(lists) == 0 {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(lists[0])
}

// RenameList implements PATCH /api/v1/lists/{id}.
func (s *Server) RenameList(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var body RenameListRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeProblem(w, http.StatusBadRequest, "", "", "name is required")
		return
	}

	res, err := s.db.Exec(
		`UPDATE lists SET name = ? WHERE id = ? AND user_id = ?`,
		name, int64(id), uid,
	)
	if err != nil {
		if dbutil.IsUniqueViolation(err) {
			writeProblem(w, http.StatusConflict, "", "", "a list with that name already exists")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeProblem(w, http.StatusNotFound, "", "Not Found", "")
		return
	}

	lists, err := s.queryLists(uid, int64(id))
	if err != nil || len(lists) == 0 {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lists[0])
}

// DeleteList implements DELETE /api/v1/lists/{id}.
func (s *Server) DeleteList(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	res, err := s.db.Exec(
		`DELETE FROM lists WHERE id = ? AND user_id = ?`,
		int64(id), uid,
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

// AddFeedToList implements POST /api/v1/lists/{id}/feeds.
func (s *Server) AddFeedToList(w http.ResponseWriter, r *http.Request, id IDPath) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var body AddFeedToListRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	// UPDATE returns 1 only when both the feed AND the list are owned
	// by the caller. The list-ownership check is folded into a single
	// statement via a subquery, ensuring atomicity and avoiding TOCTOU.
	res, err := s.db.Exec(
		`UPDATE feeds
		    SET list_id = ?
		  WHERE id = ?
		    AND user_id = ?
		    AND ? IN (SELECT id FROM lists WHERE user_id = ?)`,
		int64(id), body.FeedId, uid, int64(id), uid,
	)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Could be: feed not owned, list not owned, feed already in
		// the same list. Distinguish "no-op same-list" from "404
		// ownership" by an explicit check.
		var hits int
		_ = s.db.QueryRow(
			`SELECT COUNT(*)
			   FROM feeds f
			   JOIN lists l ON l.user_id = f.user_id
			  WHERE f.id = ? AND f.user_id = ?
			    AND l.id = ? AND l.user_id = ?
			    AND f.list_id = ?`,
			body.FeedId, uid, int64(id), uid, int64(id),
		).Scan(&hits)
		if hits == 0 {
			writeProblem(w, http.StatusNotFound, "", "Not Found", "")
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// RemoveFeedFromList implements DELETE /api/v1/lists/{id}/feeds/{feedID}.
func (s *Server) RemoveFeedFromList(w http.ResponseWriter, r *http.Request, id IDPath, feedID int64) {
	uid, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Verify the list is owned by the caller. If not, 404.
	var owned int
	err := s.db.QueryRow(
		`SELECT 1 FROM lists WHERE id = ? AND user_id = ?`,
		int64(id), uid,
	).Scan(&owned)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeProblem(w, http.StatusNotFound, "", "Not Found", "")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	// Idempotent: clear list_id only if the feed is owned by the
	// caller AND currently in this list. RowsAffected==0 is fine.
	if _, err := s.db.Exec(
		`UPDATE feeds SET list_id = NULL
		   WHERE id = ? AND user_id = ? AND list_id = ?`,
		feedID, uid, int64(id),
	); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// queryLists returns lists belonging to userID, ordered by name. If
// listID > 0, the result is filtered to the single matching list (or
// empty if not found / not owned).
func (s *Server) queryLists(userID, listID int64) ([]List, error) {
	q := `SELECT l.id, l.name, l.created_at,
	             (SELECT COUNT(*) FROM feeds WHERE list_id = l.id) AS feed_count
	        FROM lists l
	       WHERE l.user_id = ?`
	args := []any{userID}
	if listID > 0 {
		q += ` AND l.id = ?`
		args = append(args, listID)
	}
	q += ` ORDER BY l.name ASC`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]List, 0)
	for rows.Next() {
		var (
			id           int64
			name         string
			createdAtRaw string
			feedCount    int
		)
		if err := rows.Scan(&id, &name, &createdAtRaw, &feedCount); err != nil {
			return nil, err
		}
		createdAt, perr := parseSQLiteTimestamp(createdAtRaw)
		if perr != nil {
			return nil, perr
		}
		out = append(out, List{
			Id:        id,
			Name:      name,
			CreatedAt: createdAt,
			FeedCount: feedCount,
		})
	}
	return out, rows.Err()
}
