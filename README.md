# rdr

A minimalist, self-hosted RSS reader for homelabs.

## About

rdr is a lightweight RSS/Atom feed reader built with Go and SQLite. It runs as
a single binary with no external dependencies, designed for homelab deployment
on trusted networks. The UI is server-rendered HTML with a minimal hand-rolled
stylesheet. A small optional JavaScript file provides keyboard shortcuts as a
progressive enhancement — everything works without it.

## Screenshots

| Solarized Light                              | Solarized Dark                             |
| -------------------------------------------- | ------------------------------------------ |
| ![Light theme](screenshots/items-light.png)  | ![Dark theme](screenshots/items-dark.png)  |

## Features

- RSS and Atom feed support
- Full-text search across all items (SQLite FTS5)
- Feed organization with lists (a feed can belong to multiple lists)
- Four themes: Solarized Light/Dark and Modus Light/Dark (WCAG AAA high-contrast)
- Background feed polling with configurable interval
- Automatic data retention (prune old read items)
- Multi-user support with session-based authentication
- Single binary deployment or Docker
- Keyboard shortcuts for item navigation (vim-style j/k/h/l)
- Mobile-friendly responsive design

## Quick Start

### Docker

```bash
docker run -p 8080:8080 -v rdr-data:/data rdr
```

Open <http://localhost:8080>, register an account, and add feeds.

### Binary

```bash
./rdr
```

See [INSTALLING.md](INSTALLING.md) for configuration, Docker Compose setup,
and keyboard shortcuts. See [HACKING.md](HACKING.md) for development
instructions.

## License

[MIT](LICENSE)
