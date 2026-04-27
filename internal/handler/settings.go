package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"database/sql"

	"github.com/chrisallenlane/rdr/internal/middleware"
	"github.com/chrisallenlane/rdr/internal/model"
	"github.com/chrisallenlane/rdr/internal/token"
)

// settingsContent is the .Content payload for templates/pages/settings.html.
type settingsContent struct {
	model.UserSettings
	Tokens   []token.Token
	NewToken string // populated only on the response right after creation
}

func (s *Server) handleSettingsForm(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())
	settings := queryUserSettings(s.db, user.ID)

	tokens, err := token.List(s.db, user.ID)
	if err != nil {
		s.internalError(w, r, "listing tokens", err)
		return
	}

	content := settingsContent{
		UserSettings: settings,
		Tokens:       tokens,
		NewToken:     popNewTokenCookie(w, r),
	}
	s.render(w, r, "settings.html", PageData{Content: content})
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

// handleCreateToken creates an API token for the current user. The raw
// token is shown to the user exactly once, via a short-lived cookie
// read on the next /settings render.
func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	name := r.FormValue("name")
	expiresAt, err := parseTokenExpiry(r.FormValue("expires_at"))
	if err != nil {
		setFlash(w, r, "Invalid expiry date.")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	raw, _, err := token.Generate(s.db, user.ID, name, expiresAt)
	if err != nil {
		// Most likely cause is empty name; keep the user message generic.
		setFlash(w, r, "Could not create token: "+err.Error())
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	setNewTokenCookie(w, r, raw)
	setFlash(w, r, "Token created. Copy it now — it won't be shown again.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// handleRevokeToken deletes a token belonging to the current user.
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	user := middleware.UserFromContext(r.Context())

	id, ok := s.pathInt64(w, r, "id")
	if !ok {
		return
	}

	if err := token.Revoke(s.db, user.ID, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		s.internalError(w, r, "revoking token", err)
		return
	}

	setFlash(w, r, "Token revoked.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

const newTokenCookieName = "rdr_new_token"

// setNewTokenCookie writes a short-lived (60s) cookie carrying the raw
// token so the next /settings render can show it once.
func setNewTokenCookie(w http.ResponseWriter, r *http.Request, raw string) {
	setCookie(w, r, newTokenCookieName, raw, 60, true)
}

// popNewTokenCookie returns and clears the new-token cookie set by a
// preceding token creation.
func popNewTokenCookie(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(newTokenCookieName)
	if err != nil {
		return ""
	}
	setCookie(w, r, newTokenCookieName, "", -1, true)
	return c.Value
}

// parseTokenExpiry parses an HTML <input type="date"> value (YYYY-MM-DD)
// or empty string. Empty returns the zero time, meaning "never expires".
// Past dates return an error so the user notices.
func parseTokenExpiry(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	// End-of-day in UTC so a date-only entry doesn't expire mid-day.
	t = t.Add(24 * time.Hour).Add(-time.Second)
	if !t.After(time.Now()) {
		slog.Warn("token expiry in the past", "expires_at", t)
		return time.Time{}, errors.New("expiry must be in the future")
	}
	return t, nil
}
