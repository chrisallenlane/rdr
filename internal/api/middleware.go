package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/chrisallenlane/rdr/internal/token"
)

// userIDContextKey is the context key under which the bearer middleware
// stashes the authenticated user's id.
type userIDContextKey struct{}

// userIDFromContext returns the authenticated user id, or 0 if none is
// attached. Handlers that have been routed through bearerAuth can rely
// on a non-zero return; if zero, that's a programming error and the
// handler should respond 401.
func userIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(userIDContextKey{}).(int64); ok {
		return v
	}
	return 0
}

// bearerAuth wraps next with a check for `Authorization: Bearer <token>`.
// On success the authenticated user id is added to the request context.
// On any failure (missing / malformed / unknown / expired) it emits a
// generic RFC 7807 401 — never differentiating, so unauthenticated
// callers cannot enumerate token state.
//
// The set of paths in publicAPIPaths bypass auth (notably healthz and
// the spec endpoints). Non-API paths are not expected to reach this
// middleware (the outer mux only routes /api/ to it).
func bearerAuth(db *sql.DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAPIPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		raw, ok := bearerTokenFromRequest(r)
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "", "", "missing or malformed Authorization header")
			return
		}

		userID, _, err := token.Validate(db, raw)
		if err != nil {
			if errors.Is(err, token.ErrInvalid) {
				writeProblem(w, http.StatusUnauthorized, "", "", "invalid or expired token")
				return
			}
			// Surface unexpected errors as 500 rather than leaking 401 ambiguity.
			writeProblem(w, http.StatusInternalServerError, "", "", "")
			return
		}

		ctx := context.WithValue(r.Context(), userIDContextKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerTokenFromRequest extracts the raw token from a properly formed
// "Authorization: Bearer <value>" header. Other schemes return false.
func bearerTokenFromRequest(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

// isPublicAPIPath returns true for endpoints that may be called without
// a bearer token. Keep the set small and exact.
func isPublicAPIPath(path string) bool {
	switch path {
	case "/api/v1/healthz", "/api/openapi.yaml", "/api/openapi.json":
		return true
	}
	return false
}
