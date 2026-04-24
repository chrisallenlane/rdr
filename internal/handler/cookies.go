package handler

import (
	"net/http"

	"github.com/chrisallenlane/rdr/internal/middleware"
)

// setCookie writes a cookie with the application's standard security
// defaults: Path=/, SameSite=Lax, Secure when the request arrived over TLS.
// httpOnly should be true for all server-read cookies; pass false only when
// the cookie must be readable by client-side JavaScript (e.g. the theme
// cookie, which is consumed by static/js/app.js for progressive enhancement).
// Use maxAge = -1 to clear a cookie.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int, httpOnly bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		MaxAge:   maxAge,
		Path:     "/",
		HttpOnly: httpOnly,
		Secure:   middleware.IsSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}
