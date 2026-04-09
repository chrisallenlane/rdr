# Contributing

Contributions are welcome. This document covers the basics.

## Development Setup

Requires Go 1.25+.

```bash
git clone https://github.com/chrisallenlane/rdr.git
cd rdr
make build
make test
```

## Workflow

1. Fork the repository and create a feature branch.
2. Make your changes.
3. Run `make fmt` and `make vet` before committing.
4. Run `make test` and ensure all tests pass.
5. Open a pull request with a clear description of what changed and why.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- No new JavaScript. The UI is server-rendered HTML only.
- Prefer simplicity over abstraction. Avoid adding dependencies unless
  they solve a problem that would be unreasonable to solve in-house.
- Tests should verify behavior, not implementation details.

## Project Structure

See [ARCHITECTURE.md](ARCHITECTURE.md) for an overview of the codebase
layout and design decisions.

## Reporting Issues

Open an issue on the GitHub repository. Include steps to reproduce and
any relevant log output.

## License

By contributing, you agree that your contributions will be licensed under
the [MIT License](LICENSE).
