package handler

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
)

// listsPageData carries data for the lists page template.
type listsPageData struct {
	Lists []model.List
	Error string
}

// renderListsTableFragment queries the user's lists and renders the
// lists_table fragment. It returns false if an error was rendered.
func (s *Server) renderListsTableFragment(w http.ResponseWriter, r *http.Request, userID int64) bool {
	lists, err := s.queryUserListsWithCounts(userID)
	if err != nil {
		slog.Error("querying lists", "error", err)
		s.renderInternalError(w, r)
		return false
	}
	s.renderFragment(w, "lists_table.html", lists)
	return true
}

// queryUserListsWithCounts returns all lists owned by userID with feed counts.
func (s *Server) queryUserListsWithCounts(userID int64) ([]model.List, error) {
	rows, err := s.db.Query(
		`SELECT l.id, l.name, l.created_at,
		        (SELECT COUNT(*) FROM feeds WHERE list_id = l.id) AS feed_count
		 FROM lists l WHERE l.user_id = ?
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
		var createdAt sql.NullString
		if err := rows.Scan(&l.ID, &l.Name, &createdAt, &l.FeedCount); err != nil {
			return nil, err
		}
		if t := parseTime(createdAt); t != nil {
			l.CreatedAt = *t
		}
		l.UserID = userID
		lists = append(lists, l)
	}
	return lists, rows.Err()
}

func (s *Server) handleLists(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	lists, err := s.queryUserListsWithCounts(user.ID)
	if err != nil {
		slog.Error("querying lists", "error", err)
		s.renderInternalError(w, r)
		return
	}

	s.render(w, r, "lists.html", PageData{
		Content: listsPageData{Lists: lists},
	})
}

func (s *Server) handleCreateList(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	name := strings.TrimSpace(r.FormValue("name"))

	if name == "" {
		if isHTMXRequest(r) {
			flash(w, r, "List name is required.")
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		s.render(w, r, "lists.html", PageData{
			Content: listsPageData{Error: "List name is required."},
		})
		return
	}

	_, err := s.db.Exec(
		"INSERT INTO lists (user_id, name) VALUES (?, ?)",
		user.ID, name,
	)
	if err != nil {
		if isUniqueViolation(err) {
			if isHTMXRequest(r) {
				flash(w, r, "A list with that name already exists.")
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			s.render(w, r, "lists.html", PageData{
				Content: listsPageData{Error: "A list with that name already exists."},
			})
			return
		}
		slog.Error("inserting list", "error", err)
		s.renderInternalError(w, r)
		return
	}

	if isHTMXRequest(r) {
		flash(w, r, "List created.")
		s.renderListsTableFragment(w, r, user.ID)
		return
	}

	setFlash(w, r, "List created.")
	http.Redirect(w, r, "/lists", http.StatusSeeOther)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	if isHTMXRequest(r) {
		user := middleware.UserFromContext(r.Context())
		id, ok := s.pathInt64(w, r, "id")
		if !ok {
			return
		}
		if !s.verifyOwnership(w, r, "lists", id, user.ID) {
			return
		}
		if _, err := s.db.Exec("DELETE FROM lists WHERE id = ? AND user_id = ?", id, user.ID); err != nil {
			slog.Error("deleting list", "error", err)
			s.renderInternalError(w, r)
			return
		}
		flash(w, r, "List removed.")
		s.renderListsTableFragment(w, r, user.ID)
		return
	}
	s.deleteByID(w, r, "lists", "List", "/lists")
}
