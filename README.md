# Casper MCP

Casper builds a queryable infrastructure graph from Terraform state and exposes it to AI coding tools over MCP.

## Local Run

Start Postgres:

```sh
docker compose up -d postgres
```

The local Postgres container is exposed on host port `55432` to avoid clashing with a system Postgres on `5432`.

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
- `get_dependencies(resource_id)`: returns direct dependencies and dependents for a resource ID from `find_resource`.
- `get_module_for(intent)`: searches Terraform modules by path, variables, outputs, and managed resources.
- `get_conventions(resource_type)`: returns convention summaries derived from Terraform modules for a resource type.
- `find_similar(description)`: returns similar Terraform resources and modules as examples for a described change.

## Current Scope

- Local raw Terraform state files
- Terraform module scanning from `.tf` code
- Convention summaries derived from Terraform modules
- Postgres-backed resource graph
- MCP stdio server

S3 state, Terraform Cloud, HCL convention extraction, and live AWS ingestion are later slices.
