# Development

How to run Casper from source and exercise the optional Postgres-backed graph used by the local UI.

For the normal stdio MCP flow you do **not** need any of this — `casper-mcp serve --dir <path>` builds an in-memory graph and serves it directly.

## Build

```sh
go build ./cmd/casper-mcp
```

## Run from source

```sh
go run ./cmd/casper-mcp serve --dir /path/to/your/terraform
```

This starts the MCP server over stdio. Use it the same way the released binary works — Claude Code, Claude Desktop, and Cursor can all spawn it via `.mcp.json`.

## Postgres-backed mode (UI only)

The HTML graph at `casper-mcp ui` is backed by Postgres. The MCP server itself doesn't need it.

Start Postgres:

```sh
docker compose up -d postgres
```

The container exposes host port `55432` to avoid clashing with a system Postgres on `5432`.

Apply migrations:

```sh
go run ./cmd/casper-mcp migrate --config .casper/config.yaml
```

Ingest the sample Terraform state:

```sh
go run ./cmd/casper-mcp ingest --config .casper/config.yaml
```

Start the local graph UI:

```sh
go run ./cmd/casper-mcp ui --config .casper/config.yaml --addr :8080
```

For a static HTML export with no Postgres, use `export` instead:

```sh
go run ./cmd/casper-mcp export --dir /path/to/your/terraform --output graph.html
```

## Tests

All tests live under `tests/`, organized in subfolders mirroring the source layout under `internal/`. They run as external test packages (`package foo_test`) and only call exported APIs.

```sh
go test ./...                  # all unit tests
go test ./tests/graph/...      # one package
```

### End-to-end tests

The e2e suite under `tests/e2e/` builds the `casper-mcp` binary, spawns it over stdio with a Terraform fixture, and drives it through the official Go MCP client. It's gated behind a `//go:build e2e` tag so the default test run doesn't pull it in.

```sh
go test -tags=e2e -v ./tests/e2e/...               # local, builds binary into tmp
docker build -t casper-e2e -f tests/e2e/Dockerfile .
docker run --rm casper-e2e                          # hermetic run, same as CI
```

CI runs the Docker variant in a separate `e2e` job after unit tests pass. The fixture is plain `.tf` files in `tests/e2e/testdata/` — no `terraform init` needed.

## Lint

```sh
golangci-lint run
```

## Releasing

Tagged releases run `.github/workflows/release.yml`, which uses `goreleaser` to publish binaries for macOS and Linux. The npm wrapper in `npm/` downloads the binary matching the host platform on `npm install`.
