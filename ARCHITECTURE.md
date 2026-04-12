# Architecture

rdr is a server-rendered Go web application backed by SQLite. It runs as a
single binary with no external dependencies.

## Directory Structure

```
cmd/rdr/            Entry point (main.go)
embed.go            go:embed directives for static/ and templates/
internal/
  config/           Environment-variable configuration (Config struct)
  database/         SQLite connection, pragma setup, schema initialization
  discover/         Feed URL auto-discovery from website URLs
  favicon/          Favicon downloading, slug computation, file management
  handler/          HTTP handlers, routing, server setup, template rendering
                    (organized by resource: auth, feeds, items, lists, search)
  httpclient/       Shared HTTP client, user-agent, and response size limits
  middleware/       Session-based authentication middleware
  model/            Shared domain types (User, Feed, Item, List)
  poller/           Background feed fetcher and data-retention pruner
  sanitize/         HTML sanitization, relative URL resolution, syntax highlighting
  testutil/         Shared test helpers (in-memory DB, fixture insertion)
static/
  css/              Stylesheets (app.css, syntax.css)
  js/               Client-side keyboard shortcuts (progressive enhancement)
  favicon.svg
templates/
  layout/           Base HTML layout (base.html)
  pages/            Per-page templates (items, feeds, lists, search, etc.)
```

## Key Design Decisions

**Single binary.** Templates and static assets are embedded via `go:embed`.
The only runtime dependency is the SQLite database file.

**Minimal JavaScript.** The entire UI is server-rendered HTML. Theme
selection, form submission, and navigation are all standard HTTP. A small
`static/js/app.js` adds keyboard shortcuts as a progressive enhancement —
everything works without it. This keeps the frontend trivially auditable
and accessible.

**Classless CSS.** The stylesheet targets semantic HTML elements directly.
Pico CSS was removed in favor of a hand-rolled Solarized-themed stylesheet
to keep the design minimal and self-contained.

**Pure Go SQLite.** Uses `modernc.org/sqlite` (a cgo-free SQLite
implementation) so the binary cross-compiles without a C toolchain. WAL
mode and foreign keys are enabled via pragmas at connection time.

**Embedded schema.** The canonical schema lives in
`internal/database/schema.sql` and is applied once on first startup.
Subsequent opens are no-ops (detected by checking for the `users` table).

## Request Flow

1. `cmd/rdr/main.go` loads config, opens the database, and starts the
   HTTP server and background poller.
2. `handler.NewServer()` parses templates and registers routes on an
   `http.ServeMux`.
3. Protected routes pass through `middleware.RequireAuth`, which looks up
   the session cookie in the database and attaches a `*model.User` to the
   request context.
4. Handlers query the database directly (no ORM), build a `PageData`
   struct, and call `s.render()` to execute the appropriate template.

## Background Poller

`poller.NewPoller()` constructs a `*Poller`. `p.Start(ctx)` is a blocking
method (called in a goroutine by `main.go`) that polls all feeds on a
configurable interval. `p.TriggerSync(ctx)` starts an async poll cycle on
demand and returns false if one is already running (used by the manual sync
button). For each feed the poll cycle:

1. Fetches the RSS/Atom XML via HTTP
2. Parses it with `gofeed`
3. Upserts items into the database (`INSERT OR IGNORE` by GUID)
4. Updates feed metadata (title, site URL, last-fetched timestamp)
5. Downloads a favicon via `favicon.Fetch` (best-effort, errors logged)

After each poll cycle, `PruneOldItems` deletes read items older than the
configured retention period (if set).

## Content Pipeline

When an item is viewed, its content passes through three sanitization steps:

1. `sanitize.ResolveRelativeURLs` -- rewrites relative `src`/`href`
   attributes to absolute URLs using the feed's base URL
2. `sanitize.HTML` -- strips dangerous tags/attributes via bluemonday
3. `sanitize.HighlightCodeBlocks` -- applies Chroma syntax highlighting
   to `<pre><code>` blocks

## Authentication

Session-based with bcrypt password hashing. Sessions are stored in the
database with an expiration timestamp. The session cookie is HttpOnly and
SameSite=Lax. There is no "remember me" -- sessions last 30 days.

## Testing

- Unit tests for pure functions (sanitize, config, helpers, poller logic)
- Integration tests for HTTP handlers (via `httptest` and an in-memory
  SQLite database)
- Fuzz tests for HTML sanitization, OPML import, feed discovery, favicon
  handling, and time parsing
- Run `make test` for the full suite, `make fuzz` for fuzz testing
