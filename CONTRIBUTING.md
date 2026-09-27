# Contributing

Thank you for your interest in Marquee. The project is in early development, and the architecture is still settling. Please open an issue to discuss a change before starting significant work.

## Development setup

1. Install Docker (Desktop or Engine, with Compose v2), Go 1.26 or later, and Python 3.12 or later.
2. Clone the repository and run `make full-up`, or `scripts/full-up.ps1` on Windows without make. This builds the executables and images, starts the stack and verifies it.
3. Run `make check` before submitting a change. It vets, tests and validates the stack.

## Guidelines

- **Scope:** keep changes focused on one concern, and include tests for new behavior.
- **Go:** format with `gofmt`; `go vet ./...` and `go test ./...` must pass.
- **Python:** target Python 3.12. Prefer ONNX Runtime or CTranslate2 over PyTorch in the default images.
- **Dependencies:** they must be compatible with the MIT License (MIT, BSD, Apache-2.0 or MPL-2.0). Discuss any GPL or AGPL dependency in an issue first.
- **Architecture rules:** follow the rules described in [docs/architecture.md](docs/architecture.md). In particular:
  - no video re-encoding in the playback path;
  - one writing service per database;
  - hardware access only in the host agent.
- **Content sources:** do not add built-in scrapers for content sites or streaming platforms, and do not add DRM circumvention. Marquee ships without content sources.

## Commit messages

Use the imperative mood and a short summary line, for example `Add range request handling to stream server`.

## Reporting security issues

Do not open public issues for security problems. See [SECURITY.md](SECURITY.md).
