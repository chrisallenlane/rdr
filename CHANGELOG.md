# Changelog

## v1.3.0 - 2026-05-14

### Upgrade notes

First start after pulling v1.3.0 applies two automatic migrations
(`003_fts_trigger_columns.sql` and `004_perf_indexes.sql`).
`goose_db_version` advances from 2 to 4 in a single boot. Expect a
one-time `maintenance: VACUUM complete` INFO log on the first poll
cycle after the upgrade as the new daily VACUUM resets the in-process
timer. Subsequent VACUUMs run at most once per 24 hours per process
lifetime (see "Internal"). No operator action required.

**Downgrade:** v1.2.x binaries will refuse to start against a
v1.3.0-migrated database (goose detects the newer schema version).
Downgrade requires restoring a pre-upgrade backup.

**Migration retry:** If either migration fails (rare; the most likely
cause is disk-full or unwritable DB directory), rdr exits with an
error. Goose only advances the version on success, so restarting the
container after fixing the underlying issue resumes from the failed
migration. Backup restore is only required for column-change
migrations, and this release contains none.

### Features

- **Mark-read filter scoping.** Both `POST /items/mark-read` (HTML form)
  and `POST /api/v1/items/mark-read` (JSON API) now respect the
  `starred` and `unread` filters. A user viewing `/items?starred=1`
  who clicks "mark all as read" — or an API client posting
  `{"starred": true}` — now marks only the matching items, not every
  unread item in the account. The JSON API's `MarkReadRequest` schema
  gained optional `starred` and `unread` boolean fields; clients
  passing `{}`, `{"feed_id": N}`, or `{"list_id": N}` are unaffected.
  OpenAPI spec `info.version` bumped to `1.3.0` to track the app
  release.

### Bug fixes

- **Background goroutines outlived shutdown.** OPML imports
  (`POST /opml`) and JSON API feed adds (`POST /api/v1/feeds`)
  spawned fire-and-forget goroutines that were not tracked by main's
  shutdown sequence. On graceful restart they could continue writing
  to a closed database, producing `sql: database is closed` errors
  in the logs and losing import work mid-flight. A new
  `internal/background.Group` helper now registers these goroutines;
  main waits on them between `httpServer.Shutdown` and `db.Close`,
  and `Group.Close` prevents stragglers that outlived the shutdown
  deadline from racing the deferred close.
- **Favicon fetch raced for multi-feed hosts.** Two concurrent poll
  workers fetching feeds at the same host (multiple Substack,
  GitHub, Mastodon, etc.) raced in `favicon.Fetch`, occasionally
  producing orphan files on disk or `favicon_url` columns pointing
  at a non-existent extension. Per-slug `singleflight` now
  serializes the fetch and dedups the upstream HTTP request.
- **Relative URLs in feed HTML using single quotes or unquoted
  attributes were silently skipped.** The regex-based resolver only
  matched `attr="value"`. Feeds emitting `<img src='foo.jpg'>` or
  `<img src=foo.jpg>` rendered as broken images. Replaced with a
  proper HTML tokenizer that also handles `srcset` and `poster`
  and no longer falsely rewrites `data-src=` / `data-href=`.
- `adjacentItemID` now logs non-`sql.ErrNoRows` errors at WARN
  instead of silently swallowing them, so a real DB hiccup that
  hides prev/next links is observable in operator logs.
- The FTS5 sync trigger on the `items` table now only fires when
  indexed columns (title, content, description) change. Previously
  it ran on every row update, including read/star toggles,
  generating unnecessary FTS5 segment churn. Bulk mark-as-read
  operations are measurably cheaper as a result.
- Feed fetch now stores metadata and items atomically. Previously,
  a process kill or crash mid-fetch could leave a feed marked as
  "successfully fetched" while only a partial set of items had
  been persisted. Now, either everything from a fetch lands or
  nothing does; a partial state is no longer reachable. As a side
  benefit, the number of WAL commits per fetch drops from N+1 to
  1, which compounds with the `synchronous=NORMAL` pragma change
  for noticeably faster polls on multi-item feeds.

### Internal

- New `internal/background` package: a thin wrapper around
  `sync.WaitGroup` with an explicit `Close()` that drops post-Close
  `Go(fn)` submissions. Enforces the "no Add after Wait returned"
  contract at the type level rather than relying on shutdown
  timing.
- `NewServer` and `NewPoller` signatures gained `ctx` and
  `bg *background.Group` parameters; `api.Config` gained `Ctx` and
  `Background`. Internal packages; only `cmd/rdr/main.go` is
  affected.
- `internal/sanitize.ResolveRelativeURLs` rewritten from regex to
  `golang.org/x/net/html` tokenizer. Output attributes are now
  consistently double-quoted (bluemonday already does this
  downstream, so user-visible HTML is unchanged).
- Adds `golang.org/x/sync` as a direct dependency (promoted from
  indirect for `singleflight.Group`); `oapi-codegen/runtime`
  similarly promoted by `go mod tidy`.
- ~2,000 lines of test additions: reproducing tests for every
  fixed bug (durable regression artifacts) plus coverage-improvement
  tests pinning html/template's contextual URL escape, the three
  SQLite timestamp parsers, and bcrypt's long-password handling.
- Tuned SQLite connection pragmas for the rdr workload:
  `synchronous=NORMAL` (safe under WAL mode, removes per-commit
  fsyncs that the default `FULL` incurred), 64 MB page cache,
  in-memory temp store, and 256 MB mmap. No operator action required.
- Added `idx_feeds_list_id` (covers sidebar unread-count subqueries
  for lists) and a composite `idx_items_feed_published_at` (covers
  the main item listing query and prev/next navigation). Both ship
  as migration `004_perf_indexes.sql`, applied automatically on
  first startup. The migration is idempotent (`IF NOT EXISTS` on
  both indexes) — restarting the container after a failed boot
  safely retries.
- The poll cycle now runs `PRAGMA optimize` at the end of each pass.
  This is a cheap operation (a no-op in the common case, a small
  bounded `ANALYZE` when statistics are stale) that keeps the SQLite
  query planner informed as the `items` table grows. No operator
  action required; failures (if any) are logged at WARN and do not
  abort the poll cycle.
- rdr now runs SQLite `VACUUM` to reclaim disk pages freed by item
  retention. The operation runs at most once per 24 hours **per
  process lifetime** — restarting the rdr container resets the timer,
  so frequent redeploys will see VACUUM on the first poll cycle after
  each restart. On a typical homelab deployment the operation
  completes in well under a second; expect a `maintenance: VACUUM
  complete` INFO log with a `duration` field. If VACUUM fails (most
  commonly: disk full), the failure is logged and retried with
  exponential backoff (immediate → 1h → 6h → 24h) so a persistent
  failure does not generate per-poll-cycle log noise.

## v1.2.0 - 2026-04-27

### Features

- **Persistent sidebar state.** The sidebar's Feeds and Lists sections
  now remember their expand/collapse state across page loads via
  `localStorage`. State is restored before first paint by a tiny
  head-loaded `sidebar-init.js`. Falls back to the default expanded
  state when JavaScript is disabled.
- **JSON API.** All HTML resources (items, feeds, lists, search) are
  now also exposed as a v1 JSON API under `/api/v1/`. Spec-first
  OpenAPI 3.0.3 at `internal/api/openapi.yaml`, served by the running
  binary at `/api/openapi.{yaml,json}`. Errors follow RFC 7807 Problem
  Details; paginated lists use RFC 5988 `Link` headers plus
  `X-Total-Count`. Search returns structured `{text, highlight}`
  snippet segments rather than embedded sentinels. See
  [API.md](API.md) for endpoint reference and curl examples.
- **API tokens.** Mint personal access tokens (`rdr_pat_<64 hex>`)
  from **Settings → API Tokens**, name them, optionally expire them,
  and revoke them. The full token is shown once at creation and only a
  SHA-256 hash is stored. The token grants the same authority as the
  user who minted it (no granular scopes; threat model is homelab /
  trusted-network).
- **Goose-driven migrations.** Schema is now applied by
  [pressly/goose](https://github.com/pressly/goose) on every startup,
  reading numbered SQL files from `internal/database/migrations/`.
  Migrations are up-only — rollback is by restoring from backup.
  Migration `001_initial.sql` uses `IF NOT EXISTS` everywhere so it
  boots cleanly against either a fresh DB or an existing v1.1.0
  install. New tables go in their own numbered file; goose stamps
  `goose_db_version` after each successful run.

### Security

- The `rdr_new_token` cookie that carries a freshly-minted API token to
  the next `/settings` render is now scoped to `Path=/settings` (was
  `/`), so the raw token does not ride on every same-origin request
  during its 60-second lifetime.
- The `/settings` GET response now sends `Cache-Control: no-store` so
  intermediates and the browser back/forward cache cannot retain the
  rendered token list.
- HTML search no longer logs the raw user query at WARN level on FTS5
  errors — search terms can carry personal content and should not be
  persisted in logs.
- HTML search now rejects FTS5 special characters (`"`, `*`, `(`, `)`)
  up front, matching the API behavior.

### Internal

- `oapi-codegen` is wired in as a Go tool dependency
  (`go tool oapi-codegen -config gen.yaml openapi.yaml`); CI fails if
  `go generate ./...` produces a diff. Spectral lints the spec on
  every push.
- The api handler is mounted from `internal/handler/routes.go` via
  `api.New(api.Config{...})`. Test fixtures pass through stub feed
  resolvers so the suite never hits the network.
- Two small shared internal packages, `internal/dbutil/` and
  `internal/search/`, hold helpers used by both the HTML and JSON
  routes (item-filter builder, UNIQUE-violation check, FTS5 rejected-
  character set).

## v1.1.0 - 2026-04-25

### Features

- Media RSS / Yahoo Media extension support: feeds with empty content (YouTube,
  Vimeo, podcast feeds) now synthesize an HTML article from `<media:group>`,
  `<media:content>`, `<media:thumbnail>`, and `<enclosure>` data. Video and
  audio MIME types render as native `<video>`/`<audio>` players; image-only
  feeds (e.g. YouTube) render a linked thumbnail. The bluemonday sanitizer
  policy is widened to permit `<video>`, `<audio>`, and `<source>` with safe
  attributes (`controls`, `src`, `preload`, `poster`, `type`); `autoplay`,
  `loop`, and event handlers remain blocked. `src` on `<source>` and
  `poster` on `<video>` are restricted to `https?://` URLs.

## v1.0.0

Initial release.

### Features

- RSS and Atom feed support with automatic background polling
- Full-text search across all items (SQLite FTS5)
- OPML import and export, with lists exported as folders
- Feed organization via lists (a feed belongs to at most one list)
- Star/bookmark individual items
- Per-user settings: date display format (relative vs absolute), item description previews
- Item description previews beneath headlines in the item list
- Bold sidebar entries for feeds and lists with unread items; active selection highlighted
- Loading spinners on feed-add and OPML-import forms
- Four themes: Solarized Light/Dark and Modus Light/Dark (WCAG AAA)
- Configurable feed polling interval and read-item retention
- Multi-user support with session-based authentication
- Favicon fetching and display for feeds
- Keyboard shortcuts for item navigation (vim-style j/k/h/l)
- Mobile-friendly responsive design
- Single binary deployment or Docker (amd64 + arm64)
- Docker images published to [ghcr.io/chrisallenlane/rdr](https://ghcr.io/chrisallenlane/rdr)

### Security

- Login runs bcrypt against a decoy hash on unknown usernames so response time
  no longer distinguishes "user exists" from "wrong password"
- Session, theme, and flash cookies carry the `Secure` flag automatically when
  the request arrived over TLS (including `X-Forwarded-Proto: https` from a
  reverse proxy); flash cookie also carries `HttpOnly` and `SameSite=Lax`
- Favicon slug is the SHA-256 hash of the host, eliminating cross-host
  collisions that could have let one user observe or spoof another user's
  favicon files on multi-user instances
- OPML import caps the feed count to prevent a single upload from stalling the
  single-writer SQLite connection for all users
- Content-Security-Policy adds `base-uri 'self'` and `form-action 'self'`
- Built with Go 1.25.9, closing 19 reachable `crypto/tls` and `crypto/x509`
  standard-library vulnerabilities
