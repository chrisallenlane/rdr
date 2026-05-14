package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/chrisallenlane/rdr/internal/background"
	"github.com/chrisallenlane/rdr/internal/discover"
)

// Config bundles the dependencies the API needs from the host process.
// All fields except DB may be left zero-valued in tests; New supplies
// safe defaults.
type Config struct {
	// DB is the live SQLite connection. Required.
	DB *sql.DB

	// Ctx is the server-scoped context cancelled on shutdown. Background
	// goroutines use this context so they respect graceful shutdown.
	// Defaults to context.Background() if nil.
	Ctx context.Context

	// Background tracks server-scoped background goroutines so the caller
	// can wait for them before closing the database. If nil, background
	// goroutines are started untracked (fire-and-forget).
	Background *background.Group

	// FaviconsDir is the on-disk directory where downloaded favicons
	// are cached. Empty disables favicon download on feed creation.
	FaviconsDir string

	// FeedResolver maps a user-supplied URL to a feed URL via
	// auto-discovery. Defaults to discover.ResolveFeedURL.
	FeedResolver func(ctx context.Context, rawURL string) (string, error)

	// SyncFeeds triggers an account-wide feed sync. Returns true if a
	// sync was started, false if one was already in progress. Nil
	// causes /feeds/sync to behave as a no-op.
	SyncFeeds func(ctx context.Context) bool

	// SyncStatus reports whether a sync is currently in progress. Nil
	// causes /feeds/sync/status to report syncing=false.
	SyncStatus func() bool
}

// Server implements ServerInterface for the rdr v1 JSON API. It is the
// hand-written counterpart to the generated server.gen.go.
type Server struct {
	ctx          context.Context
	bg           *background.Group
	db           *sql.DB
	faviconsDir  string
	feedResolver func(ctx context.Context, rawURL string) (string, error)
	syncFeeds    func(ctx context.Context) bool
	syncStatus   func() bool
}

// New constructs an API handler that mounts the v1 JSON API and the
// OpenAPI spec endpoints (/api/openapi.{yaml,json}). The returned
// handler is intended to be mounted under "/" of an outer mux; route
// patterns include the full /api/... prefix matching the spec.
//
// Authentication is wired here: every request through the returned
// handler passes through bearerAuth, which exempts a fixed set of
// public paths (healthz, openapi.yaml, openapi.json).
func New(cfg Config) http.Handler {
	resolver := cfg.FeedResolver
	if resolver == nil {
		resolver = discover.ResolveFeedURL
	}

	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	bg := cfg.Background
	if bg == nil {
		bg = &background.Group{}
	}

	srv := &Server{
		ctx:          ctx,
		bg:           bg,
		db:           cfg.DB,
		faviconsDir:  cfg.FaviconsDir,
		feedResolver: resolver,
		syncFeeds:    cfg.SyncFeeds,
		syncStatus:   cfg.SyncStatus,
	}

	mux := http.NewServeMux()

	// Generated routes for /api/v1/*.
	HandlerFromMux(srv, mux)

	// Spec endpoints.
	mux.HandleFunc("GET /api/openapi.yaml", serveSpecYAML)
	mux.HandleFunc("GET /api/openapi.json", serveSpecJSON)

	return bearerAuth(cfg.DB, mux)
}
