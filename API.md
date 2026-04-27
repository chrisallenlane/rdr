# JSON API

`rdr` exposes a JSON HTTP API under `/api/v1/`. The full schema is the
hand-authored OpenAPI 3.0.3 document at
[`internal/api/openapi.yaml`](internal/api/openapi.yaml); the running
server also serves it at:

- `GET /api/openapi.yaml`
- `GET /api/openapi.json`

This document is a quick-start. The spec is the source of truth.

## Authentication

All endpoints except `/api/v1/healthz` require a bearer token in the
`Authorization` header.

### Mint a token

There is no token-creation API. Sign in via the browser, open
**Settings → API Tokens**, give the token a name, optionally set an
expiration date, and submit. The full token (`rdr_pat_<64 hex chars>`)
is shown **once** at the top of the page — copy it immediately. Only a
SHA-256 hash is stored.

### Use a token

```bash
TOKEN='rdr_pat_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'

curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/api/v1/me
```

### Revoke a token

Settings → API Tokens has a revoke button per token. Revocation is
immediate.

## Errors

Errors follow [RFC 7807 Problem Details](https://tools.ietf.org/html/rfc7807).
Every 4xx and 5xx response has `Content-Type: application/problem+json`
and a body shaped like:

```json
{
  "type": "about:blank",
  "title": "Bad Request",
  "status": 400,
  "detail": "url must use http or https"
}
```

## Pagination

List endpoints that paginate set `X-Total-Count` and a [RFC 5988](https://tools.ietf.org/html/rfc5988)
`Link` header with `first`, `prev`, `next`, `last` rels as applicable.
Page size is **50** and the page number is supplied via `?page=N`.

## Endpoint summary

| Method      | Path                                       | Purpose                                |
|-------------|--------------------------------------------|----------------------------------------|
| `GET`       | `/api/v1/healthz`                          | Liveness (public, no auth)             |
| `GET`       | `/api/v1/me`                               | Identify caller                        |
| `GET`       | `/api/v1/items`                            | List items (paginated, filterable)     |
| `GET`       | `/api/v1/items/{id}`                       | Get item with full content             |
| `PUT`       | `/api/v1/items/{id}/star`                  | Star an item (idempotent)              |
| `DELETE`    | `/api/v1/items/{id}/star`                  | Unstar an item (idempotent)            |
| `POST`      | `/api/v1/items/mark-read`                  | Mark items read (all, by feed, by list)|
| `GET`       | `/api/v1/feeds`                            | List feeds                             |
| `POST`      | `/api/v1/feeds`                            | Add feed by URL (auto-discovers)       |
| `GET`       | `/api/v1/feeds/{id}`                       | Get feed                               |
| `DELETE`    | `/api/v1/feeds/{id}`                       | Delete feed                            |
| `POST`      | `/api/v1/feeds/sync`                       | Trigger a sync (returns 202)           |
| `GET`       | `/api/v1/feeds/sync/status`                | `{"syncing": bool}`                    |
| `GET`       | `/api/v1/lists`                            | List lists                             |
| `POST`      | `/api/v1/lists`                            | Create a list                          |
| `GET`       | `/api/v1/lists/{id}`                       | Get list (with feeds)                  |
| `PATCH`     | `/api/v1/lists/{id}`                       | Rename a list                          |
| `DELETE`    | `/api/v1/lists/{id}`                       | Delete a list                          |
| `POST`      | `/api/v1/lists/{id}/feeds`                 | Add a feed to a list                   |
| `DELETE`    | `/api/v1/lists/{id}/feeds/{feedID}`        | Remove a feed from a list              |
| `GET`       | `/api/v1/search?q=...&page=N`              | Full-text search                       |

## Examples

The following examples assume `TOKEN` is set and the server is on
`localhost:8080`.

### Identify the caller

```bash
curl -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/api/v1/me
```

```json
{
  "id": 1,
  "username": "alice",
  "created_at": "2026-04-01T12:34:56Z"
}
```

### List unread items

```bash
curl -H "Authorization: Bearer $TOKEN" \
     'http://localhost:8080/api/v1/items?unread=true&page=1'
```

The response is an array of `Item` objects; pagination metadata is in
the `Link` and `X-Total-Count` response headers. List responses omit
the full `content` field — fetch a single item to retrieve it.

Filters: `feed_id`, `list_id`, `unread=true`, `starred=true`. `feed_id`
and `list_id` are mutually exclusive at the query level — combine them
client-side if you need to.

### Star an item

```bash
curl -X PUT \
     -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/api/v1/items/42/star
```

Returns `204 No Content`. Idempotent.

### Mark every unread item read

```bash
curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{}' \
     http://localhost:8080/api/v1/items/mark-read
```

```json
{ "marked": 17 }
```

Pass `{"feed_id": N}` or `{"list_id": N}` to scope the mark-read.

### Add a feed

```bash
curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"url": "https://example.com/blog"}' \
     http://localhost:8080/api/v1/feeds
```

The URL may be the feed itself or a website URL — auto-discovery
follows `<link rel="alternate">` tags. The 201 response carries the
freshly-inserted row; `title` may still be empty until the initial
fetch (which runs in the background) completes.

`409 Conflict` if the caller has already subscribed to this feed.
`422 Unprocessable Entity` if no feed could be discovered or the URL
is unreachable.

### Trigger a sync

```bash
curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     http://localhost:8080/api/v1/feeds/sync
```

Returns `202 Accepted` whether or not a sync was actually started — a
sync that is already in progress is left running rather than queued.
Poll `GET /api/v1/feeds/sync/status` to know when it's done.

### Create a list and put a feed in it

```bash
LIST=$(curl -s -X POST \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"name": "tech"}' \
     http://localhost:8080/api/v1/lists | jq -r .id)

curl -X POST \
     -H "Authorization: Bearer $TOKEN" \
     -H "Content-Type: application/json" \
     -d "{\"feed_id\": 7}" \
     "http://localhost:8080/api/v1/lists/$LIST/feeds"
```

`201` on create; `409` if a list with that name already exists. The
membership endpoint returns `204` on success and `404` if either the
list or the feed is not owned by the caller.

### Search

```bash
curl -H "Authorization: Bearer $TOKEN" \
     'http://localhost:8080/api/v1/search?q=golang&page=1'
```

Snippets are returned as structured `{text, highlight}` segments —
clients render highlighting any way they like:

```json
[
  {
    "id": 42,
    "title": "How to search effectively",
    "title_snippet": [
      {"text": "How to ", "highlight": false},
      {"text": "search", "highlight": true},
      {"text": " effectively", "highlight": false}
    ],
    "content_snippet": [...]
  }
]
```

The query syntax is FTS5; the characters `"`, `*`, `(`, and `)` are
rejected up-front because they produce confusing failure modes.

## Threat model and operational notes

`rdr` is designed for homelab and trusted-network deployment. The API
inherits that posture:

- **No rate limiting.** A token holder can issue arbitrary request
  volume.
- **No CORS.** The API is intended for same-origin or non-browser
  clients (curl, MCP, scripts).
- **No granular scopes.** A token has the same authority as the user
  who minted it.
- **Tokens are bearer credentials.** Treat them like passwords; the
  full value is shown once at creation and cannot be recovered.

If you intend to expose `rdr` over the public internet, add a reverse
proxy that handles TLS, rate limiting, and access control upstream.
