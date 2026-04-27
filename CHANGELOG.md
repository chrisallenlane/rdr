# Changelog

## v1.2.0 - unreleased

### Features

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
