package handler

import (
	"net/http"
	"strconv"

	"github.com/chrisallenlane/rdr/internal/middleware"
)

func (s *Server) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	settings := queryUserSettings(s.db, user.ID)
	s.render(w, r, "settings.html", PageData{Content: settings})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	showDescriptionsInt, err := strconv.Atoi(r.FormValue("show_descriptions"))
	if err != nil || (showDescriptionsInt != 0 && showDescriptionsInt != 1) {
		showDescriptionsInt = 1
	}

	dateDisplayInt, err := strconv.Atoi(r.FormValue("date_display"))
	if err != nil || (dateDisplayInt != 0 && dateDisplayInt != 1) {
		dateDisplayInt = 0
	}

	_, err = s.db.Exec(
		`INSERT INTO user_settings (user_id, show_descriptions, date_display)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		     show_descriptions = excluded.show_descriptions,
		     date_display = excluded.date_display`,
		user.ID, showDescriptionsInt, dateDisplayInt,
	)
	if err != nil {
		s.internalError(w, r, "updating settings", err)
		return
	}

	if isHTMXRequest(r) {
		flash(w, r, "Settings saved.")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	setFlash(w, r, "Settings saved.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
