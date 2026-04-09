package handler

import (
	"net/http"
)

// handleSync triggers an async feed sync and redirects back.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if s.syncFeeds != nil && s.syncFeeds(r.Context()) {
		setFlash(w, "Feed sync started.")
	} else {
		setFlash(w, "A sync is already in progress.")
	}

	http.Redirect(w, r, refererPath(r, "/feeds"), http.StatusSeeOther)
}
