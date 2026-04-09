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

func (s *Server) handleLists(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	rows, err := s.db.Query(
		`SELECT l.id, l.name, l.created_at,
		        (SELECT COUNT(*) FROM list_feeds WHERE list_id = l.id) AS feed_count
		 FROM lists l WHERE l.user_id = ?
		 ORDER BY l.name ASC`,
		user.ID,
	)
	if err != nil {
		slog.Error("querying lists", "error", err)
		s.renderInternalError(w, r)
		return
	}
	defer func() { _ = rows.Close() }()

	var lists []model.List
	for rows.Next() {
		var l model.List
		var createdAt sql.NullString
		if err := rows.Scan(&l.ID, &l.Name, &createdAt, &l.FeedCount); err != nil {
			slog.Error("scanning list row", "error", err)
			s.renderInternalError(w, r)
			return
		}
		if t := parseTime(createdAt); t != nil {
			l.CreatedAt = *t
		}
		l.UserID = user.ID
		lists = append(lists, l)
	}
	if err := rows.Err(); err != nil {
		slog.Error("iterating list rows", "error", err)
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
			s.render(w, r, "lists.html", PageData{
				Content: listsPageData{Error: "A list with that name already exists."},
			})
			return
		}
		slog.Error("inserting list", "error", err)
		s.renderInternalError(w, r)
		return
	}

	setFlash(w, "List created.")
	http.Redirect(w, r, "/lists", http.StatusSeeOther)
}

func (s *Server) handleDeleteList(w http.ResponseWriter, r *http.Request) {
	s.deleteByID(w, r, "lists", "List", "/lists")
}
