# Installing

## Quick Start

### Docker Compose (recommended)

```bash
docker compose up -d
```

Builds the image from the included `Dockerfile` and starts the service on
port 8080 with a named volume for persistent data. Open
<http://localhost:8080>, register an account, and add feeds.

### Docker (manual)

```bash
make docker                             # build the image (rdr:latest)
docker run -p 8080:8080 -v rdr-data:/data rdr
```

### Binary

```bash
make build
./bin/rdr
```

### Building Tagged Images

```bash
make docker                         # builds rdr:latest
docker tag rdr rdr:vX.Y.Z           # tag a release
make docker-multiarch               # builds for amd64 + arm64
```

## Configuration

All configuration is via environment variables:

| Variable             | Default                 | Description                             |
| -------------------- | ----------------------- | --------------------------------------- |
| `RDR_DATA_PATH`      | `~/.config/rdr`         | Parent directory for database/favicons  |
| `RDR_DATABASE_PATH`  | `$RDR_DATA_PATH/rdr.db` | Path to SQLite database file            |
| `RDR_LISTEN_ADDR`    | `:8080`                 | HTTP listen address                     |
| `RDR_POLL_INTERVAL`  | `6h`                    | Background feed polling interval        |
| `RDR_RETENTION_DAYS` | `0`                     | Days to retain read items (0 = forever) |

**Notes:**

- The Docker image sets `RDR_DATA_PATH=/data` so all state lives under the
  `/data` volume.
- `RDR_POLL_INTERVAL` accepts Go duration strings: `1h`, `30m`, `6h`, etc.
  Minimum 1 minute.
- `RDR_RETENTION_DAYS` only prunes *read* items. Unread items are kept
  regardless of age.

## Docker Volume Permissions

When using a host bind-mount instead of a named volume, ensure the mount
directory is writable by the `rdr` user (UID 1000) created in the Dockerfile:

```bash
mkdir -p /path/to/data
chown 1000:1000 /path/to/data
docker run -p 8080:8080 -v /path/to/data:/data rdr
```

Named volumes (as in `docker-compose.yml`) handle permissions automatically.

## Keyboard Shortcuts

Keyboard shortcuts require JavaScript. All functionality works without JS; the
shortcuts are a progressive enhancement.

### Items List (`/items`)

| Key                | Action                         |
| ------------------ | ------------------------------ |
| `j` / `ArrowDown`  | Focus next item                |
| `k` / `ArrowUp`    | Focus previous item            |
| `Enter`            | Open focused item              |
| `h`                | Go to newer page               |
| `l`                | Go to older page               |
| `d`                | Focus next sidebar link        |
| `u`                | Focus previous sidebar link    |
| `?`                | Toggle keyboard shortcuts help |

### Item Detail (`/items/{id}`)

| Key                | Action                         |
| ------------------ | ------------------------------ |
| `h`                | Go to newer article            |
| `l`                | Go to older article            |
| `?`                | Toggle keyboard shortcuts help |

Shortcuts are suppressed inside form inputs (search box, etc.).
