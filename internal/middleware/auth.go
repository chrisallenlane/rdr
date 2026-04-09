// Package middleware contains HTTP middleware functions.
package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/model"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext extracts the authenticated user from the request context.
// Returns nil if no user is present.
func UserFromContext(ctx context.Context) *model.User {
	user, _ := ctx.Value(userContextKey).(*model.User)
	return user
}

// ContextWithUser returns a new context with the given user attached.
func ContextWithUser(ctx context.Context, user *model.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// RequireAuth returns middleware that validates the rdr_session cookie,
// loads the associated user, and injects it into the request context.
// Unauthenticated requests are redirected to /login.
func RequireAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("rdr_session")
			if err != nil || cookie.Value == "" {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			var user model.User
			err = db.QueryRow(
				`SELECT u.id, u.username, u.created_at
				 FROM sessions s
				 JOIN users u ON s.user_id = u.id
				 WHERE s.id = ? AND s.expires_at > datetime('now')`,
				cookie.Value,
			).Scan(&user.ID, &user.Username, &user.CreatedAt)
			if err != nil {
				// Invalid or expired session: clear the cookie.
				http.SetCookie(w, &http.Cookie{
					Name:   "rdr_session",
					Value:  "",
					Path:   "/",
					MaxAge: -1,
				})
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := ContextWithUser(r.Context(), &user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
