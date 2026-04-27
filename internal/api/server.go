package api

import (
	"database/sql"
	"net/http"
)

// Server implements ServerInterface for the rdr v1 JSON API. It is the
// hand-written counterpart to the generated server.gen.go.
type Server struct {
	db *sql.DB
}

// New constructs an API handler that mounts the v1 JSON API and the
// OpenAPI spec endpoints (/api/openapi.{yaml,json}). The returned
// handler is intended to be mounted under "/" of an outer mux; route
// patterns include the full /api/... prefix matching the spec.
//
// Authentication is wired here: every request through the returned
// handler passes through bearerAuth, which exempts a fixed set of
// public paths (healthz, openapi.yaml, openapi.json).
func New(db *sql.DB) http.Handler {
	srv := &Server{db: db}

	mux := http.NewServeMux()

	// Generated routes for /api/v1/*.
	HandlerFromMux(srv, mux)

	// Spec endpoints.
	mux.HandleFunc("GET /api/openapi.yaml", serveSpecYAML)
	mux.HandleFunc("GET /api/openapi.json", serveSpecJSON)

	return bearerAuth(db, mux)
}
