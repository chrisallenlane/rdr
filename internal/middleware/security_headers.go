package middleware

import "net/http"

// SecurityHeaders adds Content-Security-Policy and X-Content-Type-Options
// headers to every response. The CSP is tailored for a server-rendered app
// with minimal JavaScript: only same-origin scripts are allowed (inline
// scripts remain blocked), images are allowed from any origin (feed content),
// and styles are restricted to same-origin with inline allowed for syntax
// highlighting.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src * data:; media-src *; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}
