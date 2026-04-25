# Changelog

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
