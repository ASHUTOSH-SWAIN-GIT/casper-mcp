# Casper MCP

Casper builds a queryable infrastructure graph from Terraform state and exposes it to AI coding tools over MCP.

## Local Run

Start Postgres:

```sh
docker compose up -d postgres
```

Apply migrations:

```sh
go run ./cmd/casper-mcp migrate --config .casper/config.yaml
```

Ingest the sample Terraform state:

```sh
go run ./cmd/casper-mcp ingest --config .casper/config.yaml
```

Start the MCP server over stdio:

```sh
go run ./cmd/casper-mcp serve --config .casper/config.yaml
```

## Current Tools

- `find_resource(query)`: searches resources by identifier, type, attributes, or tags.

## Current Scope

- Local raw Terraform state files
- Postgres-backed resource graph
- MCP stdio server

S3 state, Terraform Cloud, HCL convention extraction, and live AWS ingestion are later slices.
