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
make generate       # Regenerate api/server.gen.go from openapi.yaml
make lint-spec      # Spectral lint of the OpenAPI document
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

## JSON API

- Spec-first OpenAPI 3.0.3 at `internal/api/openapi.yaml` is the source
  of truth. `oapi-codegen` generates `internal/api/server.gen.go`
  (server interface + types). The generated file is committed; CI fails
  if `go generate ./...` produces a diff.
- Adding/changing an endpoint: edit `openapi.yaml`, run `make generate`,
  then implement (or update) the method on `*api.Server` in the
  matching resource file (`feeds.go`, `lists.go`, `items.go`,
  `search.go`, etc.). Don't hand-edit `server.gen.go`.
- Errors are RFC 7807 — emit them via `writeProblem(w, status, type,
  title, detail)`. Empty `type`/`title` get sensible defaults.
- Pagination uses `Link` (RFC 5988) + `X-Total-Count` via
  `writePagination(w, r, total, page)`. Page size constant is
  `pageSize = 50` in `internal/api/pagination.go`.
- Bearer auth is enforced at the mux level by `bearerAuth` in
  `middleware.go`. Public paths are listed in `isPublicAPIPath`. Within
  a handler, open with `uid, ok := requireUserID(w, r); if !ok { return }`
  — that helper reads `userIDFromContext(r.Context())` and writes a
  401 problem response if the bypass list let an unauthenticated
  request through (a programming error). JSON request bodies are
  parsed via `decodeJSON(w, r, &body)`, which writes a 400 problem
  response on malformed input.
- IDOR scoping is uniform: foreign-owned resources return 404, never
  403 or "not found vs forbidden" distinguishing detail.
- Tests use `testutil.OpenTestDB(t)` plus `token.Generate` to mint a
  test bearer; `authedRequest(method, target, tok, body)` is the
  shorthand. To exercise SyncFeeds/SyncStatus or AddFeed, pass them
  through `api.Config{}` rather than reaching into the package.

## Manual Testing

When registering a user for manual testing (e.g., via Playwright or the
browser), use `testuser` / `testpass` as the credentials. The registration
form requires a minimum of 8 characters for the password.

## Database

- Migrations live in `internal/database/migrations/` as numbered SQL files
  (`001_initial.sql`, `002_*.sql`, ...) and are applied via
  [`pressly/goose`](https://github.com/pressly/goose) on every startup.
- **Up-only migrations.** Down sections are intentionally empty; rollback
  is performed by restoring from backup, not by goose.
- Migration `001_initial.sql` uses `IF NOT EXISTS` on every CREATE so it
  is a no-op against any v1.1.0-or-later install. New tables go in their
  own file (`002_*.sql`, etc).
- Adding a new table: create the next-numbered file with
  `-- +goose Up` / `-- +goose Down` markers. Do not modify existing
  migrations.
- Column changes (rename/drop/retype) still require a major-version bump
  + DB wipe; goose does not synthesize ALTER from desired-state SQL.
- FTS5 virtual table `items_fts` is populated via triggers on `items`.
- Test databases use `testutil.OpenTestDB(t)` which returns an on-disk
  SQLite instance with all migrations applied.
