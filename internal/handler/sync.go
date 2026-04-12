package handler

import (
	"fmt"
	"net/http"
)

// handleSync triggers an async feed sync and redirects back.
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	started := s.syncFeeds != nil && s.syncFeeds(r.Context())

	if isHTMXRequest(r) {
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

// handleSyncStatus returns the current sync state as JSON.
func (s *Server) handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	syncing := s.syncStatus != nil && s.syncStatus()
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"syncing":%t}`, syncing)
}
