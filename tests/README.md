# Tests

All automated tests live in this directory, grouped by the component they cover. They are black-box tests: each package imports the code under test and exercises its exported API.

| Directory | Scope | Command |
|---|---|---|
| `api/` | Core HTTP API handlers | `go test ./tests/...` |
| `launcher/` | Launcher environment checks and setup helpers | `go test ./tests/...` |
| `integration/` | A running stack, over HTTP | `go test -tags integration ./tests/integration` |

Unit tests run with `make test`. Integration tests need the stack to be running (`scripts/full-up` or `marquee up`) and run with `make test-integration`. Set `MARQUEE_CORE_URL` to test a core service at a different address.

New test packages follow the same layout: `tests/<component>/`, with test fixtures under `tests/fixtures/`.
