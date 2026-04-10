package handler

import (
	"net/http"
)

// handleSync triggers an async feed sync and redirects back.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	started := s.syncFeeds != nil && s.syncFeeds(r.Context())

	if isHTMXRequest(r) {
		if started {
			flash(w, r, "Feed sync started.")
		} else {
			flash(w, r, "A sync is already in progress.")
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	if started {
		setFlash(w, "Feed sync started.")
	} else {
		setFlash(w, "A sync is already in progress.")
	}
	http.Redirect(w, r, refererPath(r, "/feeds"), http.StatusSeeOther)
}
