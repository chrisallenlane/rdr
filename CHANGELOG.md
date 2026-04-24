# Changelog

## v1.0.0

### Features

- OPML import and export of lists as OPML folders
- Settings page with per-user preferences (date display format, item description previews)
- Item description previews beneath headlines in the item list
- Bold sidebar entries for feeds/lists with unread items; highlight active selection
- Loading spinners on feed-add and OPML-import forms
- Feed-to-list relationship changed from many-to-many to one-to-one (a feed belongs to at
  most one list); simplifies OPML round-trip fidelity

### Security and Quality

- Close username-enumeration timing channel on login via bcrypt decoy comparison
- Session cookie Secure flag set automatically when the request arrives over TLS
  (or with `X-Forwarded-Proto: https`)
- Flash cookie gains HttpOnly and SameSite attributes; cookie policy enforced in one place
- Favicon slug switched to SHA-256 hash to eliminate cross-user collision potential
- OPML import: feed count capped to prevent unbounded ingestion
- CSP hardened: `base-uri` and `form-action` directives added
- Fix three non-security bugs: `refererPath` query-string stripping, OPML size-vs-read error
  messages, favicon content-type sniffing fallback
- Go toolchain bumped to 1.25.9 (closes 19 reachable `crypto/tls` + `crypto/x509`
  stdlib vulnerabilities)
- `modernc.org/sqlite` bumped from v1.34.5 to v1.49.1
- Schema migration system removed; schema applied once from a single flat file
- Test coverage expanded; flaky wall-clock assertion removed from `TestPollConcurrency`

## v0.1.0

Initial release.

### Features

- RSS and Atom feed support with automatic background polling
- Full-text search across all items (SQLite FTS5)
- Feed organization with lists
- Star/bookmark individual items
- Four themes: Solarized Light/Dark and Modus Light/Dark (WCAG AAA)
- Configurable feed polling interval and read-item retention
- Multi-user support with session-based authentication
- OPML import and export
- Keyboard shortcuts for item navigation (vim-style j/k/h/l)
- Favicon fetching and display for feeds
- Mobile-friendly responsive design
- Single binary deployment or Docker (amd64 + arm64)
