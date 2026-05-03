# Casper

> The infrastructure context layer for AI coding tools.

Casper builds a queryable graph of your cloud infrastructure and exposes it to AI agents via MCP. When Cursor, Claude Code, or any AI tool writes Terraform for your repo, it actually knows your environment — your resources, your modules, your conventions, your dependencies.

---

## The problem

AI agents writing infrastructure code have no idea what they're working with.

When you ask Cursor to "add a read replica for the orders database," it has to guess:

- What's the existing database called?
- What instance class is it using?
- What VPC is it in?
- What naming pattern do read replicas follow in this repo?
- Which security group should it use?
- What tags are required?

Without context, the AI writes generic best-practice code. You catch the mismatches in PR review. The cycle repeats.

The fundamental issue: **AI tools have eyes for code, but they're blind to infrastructure**.

---

## What Casper does

Casper builds a graph of your infrastructure from sources that already exist:

- Terraform state files (the resources Terraform manages)
- Your `.tf` code (your modules, conventions, structure)
- Optionally, live AWS APIs (resources Terraform doesn't manage)

The graph is stored in Postgres and exposed via an MCP server. Any AI tool that speaks MCP can query it.

When the AI writes Terraform now, it has answers:

- **What's `orders-prod`?** A `db.r5.large` Postgres instance in `vpc-abc123`, managed by `modules/postgres`.
- **What patterns are used here?** Read replicas in this repo follow `{name}-replica-{n}`, use `apply_immediately = false`, and inherit the parent's security group.
- **What does this team's RDS module look like?** It takes `name`, `instance_class`, `vpc_id`, `subnet_ids`, and produces an instance + subnet group.
- **Who owns this resource?** The orders team — based on tags and CODEOWNERS.

The AI writes code that fits your environment, first try.

---

## How it works

Three layers, end to end:

```
Sources                Graph                    Consumers
-------                -----                    ---------
Terraform state  ─┐                          ┌── Cursor (via MCP)
                  │                          │
.tf code        ──┼──→  Postgres graph  ─────┼── Claude Code (via MCP)
                  │                          │
Live AWS        ──┘                          └── Devin (via MCP)

                                              └── Custom AI agents
```

### 1. Ingestion

Casper reads from each source on a schedule (or via webhooks):

- **Terraform state files** — JSON, parsed into typed Go structs. Every resource becomes a node. Every reference becomes an edge.
- **`.tf` code** — parsed with HashiCorp's HCL libraries. Modules, conventions, and naming patterns get extracted.
- **Live AWS** *(optional)* — read-only describe calls fill in resources Terraform doesn't manage.

Cross-source linking happens automatically: a state file in `prod/services/orders` references a VPC defined in `prod/network`, Casper matches them by ID and creates an edge.

### 2. The graph

Two tables in Postgres:

```sql
resources (
  id, source, type, identifier,
  attributes JSONB, tags JSONB,
  module_path, managed_by, last_seen
);

dependencies (
  from_resource, to_resource, kind, source
);
```

Boring on purpose. JSONB handles arbitrary attributes per resource type. Recursive CTEs handle dependency traversal. No graph database needed.

### 3. The MCP server

AI tools connect to Casper via MCP and call tools like:

| Tool | What it answers |
|---|---|
| `find_resource(query)` | "What's `orders-prod`?" |
| `get_dependencies(resource_id)` | "What depends on this VPC?" |
| `find_similar(description)` | "Show me existing read replicas as examples" |
| `get_conventions(resource_type)` | "How does this team name RDS instances?" |
| `get_module_for(intent)` | "What module is used for Postgres?" |
| `find_owner(resource_id)` | "Who owns this?" |

Each tool is a Postgres query plus formatting. The AI sees structured responses; it doesn't see the database.

---

## Setting it up

You connect Casper to your repo with one config file.

### `.casper/config.yaml`

```yaml
# Where my Terraform state lives
states:
  - type: s3
    bucket: company-tfstate
    region: ap-south-1
    paths:
      - prod/**/terraform.tfstate
      - staging/**/terraform.tfstate

# Optional: AWS read-only access for live describe
cloud:
  aws:
    role_arn: arn:aws:iam::123456789012:role/casper-readonly
    regions: [ap-south-1, us-east-1]

# What Terraform code to scan
iac:
  paths:
    - "modules/**/*.tf"
    - "envs/**/*.tf"
  module_dirs:
    - modules/

# Optional: ownership signals
ownership:
  source: ./CODEOWNERS
```

That's it. Casper reads the config, ingests from each source, builds the graph. Your team never describes the infrastructure manually — the graph is auto-built from what already exists.

### Connecting an AI tool

For Claude Code, add to your MCP config:

```json
{
  "mcpServers": {
    "casper": {
      "command": "casper-mcp",
      "args": ["--config", ".casper/config.yaml"]
    }
  }
}
```

For Cursor, similar. Any MCP-compliant tool works.

The AI now has access to all the graph tools. Try it: ask Cursor "describe the orders production database" and watch it call `find_resource` and `get_dependencies`.

---

## A worked example

You're using Cursor to add a new RDS read replica.

**Without Casper:**

```
You: "Add a read replica for the orders database"

Cursor: [generates generic Terraform, guesses at conventions]
```

The output is some plausible-looking code that doesn't match your team's patterns. You spend 20 minutes in PR review fixing the naming, the module reference, the tags, the SG.

**With Casper:**

```
You: "Add a read replica for the orders database"

Cursor: [calls casper.find_resource("orders database")]
        Gets: aws_db_instance.orders_prod, db.r5.large, postgres,
              in vpc-abc123, managed by modules/postgres

Cursor: [calls casper.get_conventions("aws_db_instance")]
        Gets: read replicas use "{name}-replica-{n}",
              apply_immediately = false,
              same security group as parent

Cursor: [calls casper.find_similar("read replica")]
        Gets: 2 existing replicas as examples

Cursor: [writes Terraform that follows conventions exactly]
```

The PR opens with code that matches your team's patterns. Review focuses on the *idea* of the change, not on fixing style.

---

## What's in scope (and what's not)

### v0.1: Terraform-aware

Casper v0.1 builds the graph from Terraform sources only:

- Terraform state files (S3, Terraform Cloud, local)
- `.tf` code in the repo

This covers 70-90% of resources at any Terraform-using company. Resources created outside Terraform (AWS Console clicks, CloudFormation, etc.) are **not** in the graph yet.

### Coming later

- **Live AWS ingestion** — fills in unmanaged resources
- **Multi-repo aggregation** — graph spans across multiple Casper-installed repos
- **Kubernetes** — pull cluster state alongside cloud
- **Pulumi / CDK / OpenTofu** — same graph model, different ingest sources
- **Datadog / observability tags** — service ownership and runtime metadata
- **Drift detection** — flag when live AWS diverges from state

---

## What this is not

To be precise about the scope:

- **Not a replacement for Terraform.** Casper sits on top of it, not in place of it.
- **Not a CMDB.** Auto-built from sources, not manually curated.
- **Not a code review tool.** That's CodeRabbit/Greptile. Casper provides context to AI tools, not feedback on their output.
- **Not an apply pipeline.** Casper doesn't run `terraform apply`. Your existing CI does.
- **Not an AI agent itself.** Casper is what AI agents *talk to*, not an agent that does work.

What Casper *is*: **the infrastructure graph that AI agents query before writing code**.

---

## Why a graph, why MCP

Two architectural commitments worth being explicit about:

**Graph, not chat.** When an AI tool needs context, it shouldn't guess from natural language descriptions. It should query a structured model of reality. The graph gives it that — typed nodes, labeled edges, attribute-level detail.

**MCP, not custom API.** AI tools (Cursor, Claude Code, Devin) increasingly speak MCP natively. By exposing Casper as an MCP server, every AI tool gets graph access without custom integration code. You're agnostic to which AI tool wins.

The combination — structured graph + standard protocol — is what makes Casper useful across the whole AI coding ecosystem.

---

## Tech stack

- **Backend:** Go + chi
- **Database:** Postgres
- **MCP server:** `mark3labs/mcp-go`
- **Terraform parsing:** `hashicorp/terraform-json`, `hashicorp/terraform-config-inspect`
- **AWS SDK:** `aws-sdk-go-v2`
- **Job queue:** Postgres-backed (`river`)
- **Migrations:** `golang-migrate`
- **Local dev:** Docker Compose

Boring stack on purpose. Every component is mainstream and well-documented.

---

## Status

v0.1 is in active development. The first milestone: ingest a real Terraform state file, build a graph, query it from Claude Code via MCP, watch the AI use it to write code that fits the repo.

Watch the repo for updates.

---

## The pitch in one line

> CodeRabbit reviews your code. Casper teaches your AI tools to know your infrastructure.
