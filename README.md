# Casper MCP

An MCP server that gives AI agents a live, queryable view of your Terraform infrastructure. Point it at any Terraform directory and your agent can find resources, simulate changes, detect live AWS drift, and enforce org policies — without reading raw `.tf` files.

## What it does

- **Find resources** — search by name, type, tag, or attribute across all `.tf` and `.tfstate` files
- **Simulate changes** — parse proposed HCL and get blast radius, broken references, and policy violations before applying
- **Detect drift** — compare Terraform state against live AWS via read-only Describe APIs
- **Enforce policies** — define rules in `.casper/policies.yaml` (`must_equal`, `min_value`, `required`, `must_not_equal`)
- **Load any repo** — point at a GitHub URL and swap the graph on the fly, no restart needed

## Install

**npm / npx (no install needed):**
```bash
npx casper-mcp serve --dir /path/to/your/terraform
```

Or install globally:
```bash
npm install -g casper-mcp
```

**Homebrew (macOS/Linux):**
```bash
brew install ASHUTOSH-SWAIN-GIT/tap/casper-mcp
```

**Go install:**
```bash
go install github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/cmd/casper-mcp@latest
```

**Download binary:** grab the archive for your platform from [Releases](https://github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/releases), extract, and place `casper-mcp` on your `$PATH`.

## Quick start

```bash
npm install -g casper-mcp
casper-mcp init
```

Restart Claude Code and run `/casper` to query your infrastructure.

## Commands

```bash
# Wire up Casper for the current project (.mcp.json + /casper slash command)
casper-mcp init
casper-mcp init --global         # available in every Claude Code project

# Run the MCP server against a Terraform directory (stdio)
casper-mcp serve --dir /path/to/your/terraform

# Export an interactive HTML graph
casper-mcp export --dir /path/to/your/terraform --output graph.html
```

## Connect to Claude

### Claude Code

```bash
casper-mcp init
```

Writes `.mcp.json` and `.claude/commands/casper.md` in the current project.

For a manual project config, drop this in `.mcp.json` at the repo root:

```json
{
  "mcpServers": {
    "casper": {
      "command": "npx",
      "args": ["-y", "casper-mcp", "serve", "--dir", "."]
    }
  }
}
```

Or via the Claude Code CLI:

```bash
claude mcp add-json casper '{"type":"stdio","command":"npx","args":["-y","casper-mcp","serve","--dir","."]}' --scope user
```

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS):

```json
{
  "mcpServers": {
    "casper": {
      "command": "casper-mcp",
      "args": ["serve", "--dir", "/path/to/your/terraform"]
    }
  }
}
```

### Cursor

Add to `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "casper": {
      "command": "casper-mcp",
      "args": ["serve", "--dir", "/path/to/your/terraform"]
    }
  }
}
```

## Tools

| Tool | Description |
|------|-------------|
| `get_context` | Combined lookup — resources, examples, modules, conventions in one call |
| `find_resource` | Search by name, type, tag, or attribute |
| `get_dependencies` | Upstream + downstream dependency graph for a resource |
| `find_similar` | Find similar resources as HCL examples |
| `get_module_for` | Find reusable modules matching an intent |
| `get_conventions` | How a resource type is configured across the codebase |
| `simulate_impact` | Parse proposed HCL → blast radius, policy violations, reversibility context |
| `describe_live_state` | Compare Terraform state vs live AWS — detect drift |
| `load_repo` | Clone a GitHub repo and reload the graph without restarting |
| `dump_graph` | Full graph snapshot — all resources, edges, and policy violations |

See [docs/TOOLS.md](docs/TOOLS.md) for full parameter and response documentation.

## AWS live state (optional)

To enable drift detection, add a `cloud` section to `.casper/config.yaml` in your infra repo:

```yaml
cloud:
  aws:
    role_arn: arn:aws:iam::123456789012:role/casper-readonly   # omit to use ambient creds
    regions: [us-east-1, ap-south-1]
```

Casper only calls read-only Describe APIs — it never writes to AWS.

## Policies (optional)

Define org policies and workflow routing rules in `.casper/policies.yaml`:

```yaml
policies:
  - id: rds-deletion-protection
    resource: aws_db_instance
    rules:
      - arg: deletion_protection
        must_equal: "true"
    message: "RDS instances must have deletion_protection enabled"

  - id: rds-backup-retention
    resource: aws_db_instance
    rules:
      - arg: backup_retention_period
        min_value: 7
    message: "RDS instances must retain backups for at least 7 days"

workflow_rules:
  - id: database-destroy-block
    when:
      resource_type_family: database
      operation: destroy
    decision: block

  - id: prod-changes-require-approval
    when:
      env: prod
      operation: [create, modify, destroy]
    decision: require_approval
```

Policy violations are surfaced in `simulate_impact` and `dump_graph` responses. Workflow decisions are advisory — the agent reads them and follows them, but no hard enforcement occurs. Decisions: `allow`, `require_approval`, `require_security_review`, `block`.

## Repository layout

```
casper-mcp/
├── cmd/casper-mcp/        CLI entry point
├── internal/
│   ├── mcp/               MCP server + tool definitions
│   ├── graph/             In-memory + Postgres graph stores
│   ├── ingest/            Terraform .tf and .tfstate parsers
│   │   ├── terraformcode/
│   │   └── terraformstate/
│   ├── awslive/           Read-only AWS Describe + drift detection
│   ├── policy/            Policy rule engine
│   ├── workflow/          Workflow routing rules
│   ├── ui/                Static HTML graph exporter
│   ├── config/            Config loader
│   └── migrations/        Postgres schema migrations
├── migrations/            SQL migration files
├── tests/                 All Go tests (mirrors internal/ package layout)
├── docs/                  TOOLS.md, prd.md, DEVELOPMENT.md, test-repos.md
├── npm/                   npm wrapper that downloads the right binary
└── fixtures/              Sample Terraform state for tests
```

## Development

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for running Casper locally against a Postgres-backed graph (used for the optional UI mode).

## License

MIT
