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

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for a detailed overview of the
codebase structure and design decisions.
