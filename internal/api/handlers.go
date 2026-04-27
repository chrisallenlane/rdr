package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// GetHealthz returns a static OK response. Public — no authentication.
func (s *Server) GetHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(HealthStatus{Status: Ok})
}

// GetMe returns the authenticated user's identity.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if uid == 0 {
		// Should be unreachable: bearerAuth only forwards on success.
		writeProblem(w, http.StatusUnauthorized, "", "", "")
		return
	}

	var (
		username     string
		createdAtRaw string
	)
	err := s.db.QueryRow(
		`SELECT username, created_at FROM users WHERE id = ?`,
		uid,
	).Scan(&username, &createdAtRaw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Token validated against a user row that no longer exists —
			// treat as unauthenticated rather than 500.
			writeProblem(w, http.StatusUnauthorized, "", "", "")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	createdAt, err := parseSQLiteTimestamp(createdAtRaw)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "", "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(User{
		Id:        uid,
		Username:  username,
		CreatedAt: createdAt,
	})
}

// parseSQLiteTimestamp parses the formats SQLite emits for DATETIME
// columns: the default `YYYY-MM-DD HH:MM:SS` and the RFC 3339 form some
// callers write explicitly.
func parseSQLiteTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02 15:04:05", s)
}
