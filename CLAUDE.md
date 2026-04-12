# CLAUDE.md

Project-level instructions for Claude Code.

## Project Overview

rdr is a minimalist, self-hosted RSS reader. Go backend, SQLite database,
server-rendered HTML. A small `static/js/app.js` provides keyboard shortcuts
as a progressive enhancement — all functionality works without JavaScript.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the full codebase layout.

## Build and Test

```bash
make build          # Compile to ./bin/rdr
make test           # Run all tests
make fuzz           # Run fuzz tests (10s each; override with FUZZ_TIME=30s)
make fmt            # gofmt -s -w .
make vet            # go vet ./...
make lint           # golangci-lint (must be installed separately)
```

## Key Conventions

- **Minimal JavaScript.** All UI is server-rendered HTML with standard forms.
  `static/js/app.js` adds keyboard shortcuts only; no JS is required for
  any functionality.
- **No ORM.** Handlers query SQLite directly via `database/sql`.
- **Templates use `go:embed`.** Static files and templates are embedded at
  compile time. The `embed.go` file in the project root defines the two
  `embed.FS` variables.
- **`NewServer` accepts `fs.FS`** (not `embed.FS`) so tests can provide
  synthetic filesystems via `testing/fstest.MapFS`. Its full signature is
  `NewServer(db *sql.DB, staticFiles fs.FS, templateFiles fs.FS, faviconsDir string) (*Server, error)`.
- **Handler tests** use `newTestServer(t)` from `testhelpers_test.go`,
  which creates a `*Server` with an in-memory SQLite DB and stub templates.
- **Pure Go SQLite** via `modernc.org/sqlite`. No cgo required. Timestamps
  may come back as RFC 3339 strings; `parseTime()` in
  `request_helpers.go` handles multiple formats.
- **Multiple themes.** The stylesheet supports four themes via CSS custom
  properties: Solarized Light/Dark and Modus Light/Dark (plus Auto for
  OS-preference detection). See `static/css/app.css`.

## Common Patterns

- `PageData` is the top-level struct passed to every template. `.Content`
  carries page-specific data (typed as `any`).
- `middleware.RequireAuth` attaches a `*model.User` to the request context.
  Handlers retrieve it with `middleware.UserFromContext(r.Context())`.
- `s.render(w, r, "page.html", PageData{...})` renders a page template
  against the base layout.
- SQL helpers live in `handler/query_helpers.go`: `deleteByID`,
  `verifyOwnership`, `scanFeeds`, `queryUserFeeds`, `queryUserLists`, etc.
- Request helpers live in `handler/request_helpers.go`: `parseTime`,
  `paginate`, `pageFromQuery`, `pathInt64`, `parsePositiveInt64`,
  `refererPath`, etc.
- `buildItemFilter` and `itemsHeading` live in `handler/items.go`.

## Manual Testing

When registering a user for manual testing (e.g., via Playwright or the
browser), use `testuser` / `testpass` as the credentials. The registration
form requires a minimum of 8 characters for the password.

## Database

- Schema is in `internal/database/schema.sql`.
- Schema is applied once on first startup (no versioned migrations).
- FTS5 virtual table `items_fts` is populated via triggers on `items`.
- Test databases use `testutil.OpenTestDB(t)` which returns an in-memory
  SQLite instance with the schema applied.
