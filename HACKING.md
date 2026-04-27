# Hacking

## Development

```bash
git clone https://github.com/chrisallenlane/rdr.git
cd rdr

make build            # Compile binary to ./bin/rdr
make run              # Build and run
make test             # Run tests
make fuzz             # Run fuzz tests (10s each; FUZZ_TIME=30s for longer)
make fmt              # Format code (gofmt + Prettier)
make vet              # Run go vet
make lint             # Run linters (golangci-lint + ESLint)
make a11y             # Run accessibility audit (requires running server)
make docker           # Build Docker image
make docker-multiarch # Build multi-arch image (amd64 + arm64)
make release          # Cross-compile for linux/darwin amd64/arm64
make clean            # Remove build artifacts
```

Requires Go 1.25+.

## CI Workflows

Two parallel CI workflows mirror each other in scope (lint + tests +
build + publish) but target different registries:

- `.github/workflows/build.yaml` runs on GitHub Actions and publishes to
  [ghcr.io/chrisallenlane/rdr](https://ghcr.io/chrisallenlane/rdr) (the
  public image consumed by `docker-compose.yml`).
- `.gitea/workflows/build.yaml` runs on the author's Gitea instance and
  publishes to a private Gitea registry. Author-specific; outside
  contributors can ignore it.

Both publish the same tag scheme: `:sha-<short>` on every build,
`:latest` on `master`, and `:<tag>` on `v*` tag pushes.

## Adding a JSON API endpoint

The API is spec-first. The hand-authored OpenAPI document at
`internal/api/openapi.yaml` is the source of truth; `server.gen.go` is
generated and committed.

1. **Edit the spec.** Add the path, parameters, request body, and
   responses to `internal/api/openapi.yaml`. Reuse the existing
   `Problem`, `BadRequest`, `Unauthorized`, `NotFound`, `Conflict`, and
   `UnprocessableEntity` components. New schemas go under
   `components.schemas`.
2. **Lint the spec.** `make lint-spec`.
3. **Regenerate.** `make generate` — this updates `server.gen.go` with
   the new method on `ServerInterface` and the new types. Don't
   hand-edit the generated file.
4. **Implement.** Add the new method to `*api.Server` in the resource
   file that matches its tag (`feeds.go`, `lists.go`, etc.). Read the
   user id with `userIDFromContext(r.Context())`, scope every query by
   `user_id`, and emit errors via `writeProblem(...)`.
5. **Test.** Add tests alongside `*_test.go`. The standard fixture seeds
   two users so you can assert IDOR (alice can't reach bob's resource);
   `authedRequest(method, target, tok, body)` is the shorthand.
6. **Quality bar.** `make fmt && make vet && make lint && make
   lint-spec && make test` should all pass; `go generate ./...` must
   produce no diff.
7. **Document client behavior.** If the new endpoint changes the
   client-facing API surface, update [API.md](API.md) — the spec
   covers the wire-level contract but the quickstart prose lives there.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for a detailed overview of the
codebase structure and design decisions.
