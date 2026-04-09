package handler

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
)

func (s *Server) handleToggleStar(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	itemID, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	result, err := s.db.Exec(
		`UPDATE items SET starred = 1 - starred
		 WHERE id = ? AND feed_id IN (SELECT id FROM feeds WHERE user_id = ?)`,
		itemID, user.ID,
	)
	if err != nil {
		slog.Error("toggling star", "error", err)
		s.renderInternalError(w, r)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		slog.Error("getting rows affected for star toggle", "error", err)
		s.renderInternalError(w, r)
		return
	}
	if affected == 0 {
		s.renderError(w, r, http.StatusNotFound, "Item not found")
		return
	}

	http.Redirect(w, r, refererPath(r, fmt.Sprintf("/items/%d", itemID)), http.StatusSeeOther)
}
