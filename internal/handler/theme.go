package handler

import (
	"net/http"
)

// validThemes is the set of allowed theme values.
var validThemes = map[string]bool{
	"auto":            true,
	"solarized-light": true,
	"solarized-dark":  true,
	"modus-light":     true,
	"modus-dark":      true,
}

// themeFromRequest reads the rdr_theme cookie and returns a valid theme name,
// falling back to "auto" when the cookie is missing or holds an unrecognised
// value.
func themeFromRequest(r *http.Request) string {
	cookie, err := r.Cookie("rdr_theme")
	if err != nil {
		return "auto"
	}
	if validThemes[cookie.Value] {
		return cookie.Value
	}
	return "auto"
}

// handleThemeChange sets the theme from the form's "theme" field.
func (s *Server) handleThemeChange(w http.ResponseWriter, r *http.Request) {
	theme := r.FormValue("theme")
	if !validThemes[theme] {
		theme = "solarized-light"
	}

	// Theme cookie is readable by client JS (progressive enhancement), so
	// httpOnly=false.
	setCookie(w, r, "rdr_theme", theme, 31536000, false)

	if isHTMXRequest(r) {
		setHTMXTriggers(w, htmxTriggers{"setTheme": theme})
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.Redirect(w, r, refererPath(r, "/items"), http.StatusSeeOther)
}
