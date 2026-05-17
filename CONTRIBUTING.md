# Contributing to PasteLocal

Thank you for your interest in contributing to PasteLocal! This document explains how to set up your development environment, run tests, and submit changes.

## Getting Started

### Prerequisites

- Go 1.23 or newer
- `make` (optional but recommended)
- For macOS development: Xcode command line tools
- For Linux development: standard build tools

### Clone and Build

```bash
git clone https://github.com/Zen-Open-Source/PasteLocal.git
cd PasteLocal

# Build all binaries
make build

# Or build manually
go build -o bin/pastelocal ./cmd/pastelocal
go build -o bin/pastelocald ./cmd/pastelocald
go build -o bin/pastelocal-remote ./cmd/pastelocal-remote
```

## Development Workflow

### Running Tests

```bash
# Run all internal tests with race detection
go test -race ./internal/...

# Run E2E tests
go test -v -tags=e2e ./e2e/...

# Full test suite (what CI runs)
make test
```

### Linting

We enforce strict linting:

```bash
# Run all linters (what CI runs)
make lint

# Individual tools
go vet ./...
golangci-lint run ./...
gosec ./...
```

### Code Style

- Run `gofmt` before committing (`make fmt` or `gofmt -s -w .`)
- Keep functions focused and well-named
- Add tests for new behavior when practical

## Submitting Changes

1. **Fork** the repository
2. **Create a feature branch**: `git checkout -b my-feature`
3. **Make your changes** with clear, atomic commits
4. **Test thoroughly** (see above)
5. **Open a Pull Request** against `main`

### Pull Request Guidelines

- Keep PRs focused (one feature or fix per PR)
- Include a clear description of the change and why it’s needed
- Update documentation in `docs/` when behavior changes
- Make sure `go build ./...` and `go test ./internal/...` pass

## Project Structure

- `cmd/pastelocal/` — Main CLI
- `cmd/pastelocald/` — The daemon
- `cmd/pastelocal-remote/` — Remote helper (small binary installed on remote hosts)
- `cmd/relay-server/` — Experimental multi-device relay server
- `internal/server/` — Core daemon logic (handlers, history, snippets, redaction, etc.)
- `internal/clipboard/` — Cross-platform clipboard access
- `internal/crypto/` — X25519 + AES-GCM primitives (used by relay)
- `internal/relay/` — Relay client logic
- `skill/` — Claude skills

## Notes on the Relay Feature

The multi-device relay (`pastelocal relay ...` and the standalone relay server) is currently **experimental**. Contributions that improve stability, add persistence, or complete the send path are very welcome, but please coordinate first by opening an issue.

## Questions?

Open an issue with the `question` label or start a discussion. We’re happy to help you get oriented.

---

Thank you for helping make remote clipboard access better for everyone.