package handler

import (
	"fmt"
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
		s.internalError(w, r, "toggling star", err)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		s.internalError(w, r, "getting rows affected for star toggle", err)
		return
	}
	if affected == 0 {
		s.renderError(w, r, http.StatusNotFound, "Item not found")
		return
	}

	if isHTMXRequest(r) {
		var starred int
		if err := s.db.QueryRow("SELECT starred FROM items WHERE id = ?", itemID).Scan(&starred); err != nil {
			s.internalError(w, r, "querying star state", err)
			return
		}
		s.renderFragment(w, "star_button.html", struct {
			ID      int64
			Starred bool
		}{itemID, starred == 1})
		return
	}

	http.Redirect(w, r, refererPath(r, fmt.Sprintf("/items/%d", itemID)), http.StatusSeeOther)
}
