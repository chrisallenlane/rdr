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

## CI Workflow

`.gitea/workflows/build.yaml` is author-specific. The author uses Gitea as the
primary repository host and pushes Docker images to a private Gitea registry on
each commit to `master`. GitHub contributors can safely ignore this file — it
will not run on GitHub and is not required to use or contribute to the project.

## Architecture

See [ARCHITECTURE.md](ARCHITECTURE.md) for a detailed overview of the
codebase structure and design decisions.
