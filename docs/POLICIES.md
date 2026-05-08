# Policies

Casper lets your team encode infrastructure rules in YAML, and the agent reads them automatically before applying changes. You write what's required; the MCP server enforces it on every `simulate_impact` call without anyone reminding the agent.

This is the difference between *hoping* the agent follows your conventions and *guaranteeing* it does.

## How it works

```
.casper/policies.yaml          → defines org rules (declarative YAML)
        │
        ▼
casper-mcp serve               → loads policies on startup, watches for changes
        │
        ▼
agent calls simulate_impact    → response includes policy_violations[]
        │
        ▼
agent fixes its own HCL        → re-simulates, or asks for human review
```

The agent doesn't need to know your conventions ahead of time. It drafts Terraform, calls `simulate_impact`, sees the violations, and corrects them. Same mechanism applies in `dump_graph` — you can audit existing infrastructure for policy drift.

## Where policies live

`.casper/policies.yaml` at the root of the Terraform repo Casper is scanning. The same file holds two sections:

- `policies:` — per-resource argument rules
- `workflow_rules:` — advisory routing decisions (allow / require_approval / require_security_review / block)

Both are optional. Casper runs fine with neither.

## Argument rules

Each rule names a `resource` type, declares one or more argument constraints, and gives a human-readable `message` the agent can quote back in chat.

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

  - id: no-public-buckets
    resource: aws_s3_bucket
    rules:
      - arg: acl
        must_not_equal: "public-read"
    message: "S3 buckets must not be public-read"

  - id: sg-needs-description
    resource: aws_security_group
    rules:
      - arg: description
        required: true
    message: "Security groups must have a description"

  - id: everything-needs-an-owner
    resource: "*"             # apply to every resource type
    rules:
      - arg: owner
        required: true
    message: "All resources must have an owner argument"
```

### Supported rule types

| Rule | Behavior | Example |
|---|---|---|
| `must_equal` | Argument must be present and equal to this value | `deletion_protection: "true"` |
| `must_not_equal` | Argument, if present, must not equal this value | `acl: "public-read"` |
| `required` | Argument must be present and non-empty | `description: required` |
| `min_value` | Argument must parse as a number ≥ this value | `backup_retention_period: 7` |

Multiple rules in one policy all have to pass — they `AND` together. Each violation appears as its own entry in the response, so the agent gets full diagnostics on the first call.

### Targeting

- `resource: aws_db_instance` — applies to that exact Terraform type
- `resource: "*"` — applies to every resource

## Workflow rules

Workflow rules don't enforce arguments — they classify the *change itself* and route it to a decision: `allow`, `require_approval`, `require_security_review`, or `block`. The agent reads the decision and follows it (e.g., refuses to apply, asks for approval, opens a ticket).

```yaml
workflow_rules:
  - id: prod-database-destroy-block
    when:
      env: prod
      resource_type_family: database
      operation: destroy
    decision: block
    reason: "DB destroys in prod require a manual ticket"

  - id: prod-changes-require-approval
    when:
      env: prod
      operation: [create, modify, destroy]
    decision: require_approval

  - id: iam-needs-security-review
    when:
      resource_type_family: iam
    decision: require_security_review
    required_steps: ["security_lead_approval", "iam_review_ticket"]
```

### `when:` matchers

A rule fires when **all non-empty fields** in `when:` match. Empty fields are wildcards.

| Field | Description |
|---|---|
| `env` | Inferred from tags / module path / identifier — `prod`, `staging`, or `dev`. Fails closed to `prod` if undetectable. |
| `operation` | `create`, `modify`, or `destroy`. String or list. |
| `resource_type_family` | `database`, `iam`, `network_security`, `compute`, or `storage`. |
| `resource_type` | Exact Terraform type, e.g. `aws_db_instance`. |

### Decision precedence

For a single change the **first matching rule per resource wins**. Across all resources in the change, the **strictest decision wins overall**: `block` > `require_security_review` > `require_approval` > `allow`.

So if your change touches an S3 bucket (matches a `require_approval` rule) and an IAM role (matches a `require_security_review` rule), the overall decision is `require_security_review`.

## How violations surface to the agent

In `simulate_impact` the response carries:

```json
{
  "summary": "1 created, 0 modified, 0 in blast radius",
  "created": [...],
  "policy_violations": [
    {
      "policy_id": "rds-deletion-protection",
      "resource": "aws_db_instance.orders_replica",
      "type": "aws_db_instance",
      "message": "RDS instances must have deletion_protection enabled",
      "details": "argument \"deletion_protection\" must be \"true\" (not set)"
    }
  ],
  "workflow_decision": {
    "decision": "require_approval",
    "matched_rules": [
      { "id": "prod-changes-require-approval", "reason": "env=prod, operation=create" }
    ],
    "blocked": false
  }
}
```

The agent's `simulate_impact` system prompt tells it to read this section and either fix the HCL or surface the violation to you. In `dump_graph` every resource entry carries its own `policy_violations[]`, so an agent can do a one-shot audit:

> "Run dump_graph and list every resource that violates a policy."

## Designing policies that actually help

**Make the message verb-first and specific.** "Add `deletion_protection = true` to all `aws_db_instance` resources" beats "must follow security guidelines" — the agent will repeat the message verbatim when it asks you to confirm a fix.

**Prefer wide rules over narrow ones.** `resource: "*"` with `arg: owner, required: true` covers the whole repo in one rule. Many narrow per-type rules drift over time.

**Use `workflow_rules` for things that aren't a yes/no on a single argument.** "All prod database destroys need a ticket" can't be expressed as `must_equal` — it's a routing decision. That's what `decision: block` and `decision: require_approval` are for.

**Don't recreate `terraform validate`.** If something is provably wrong at plan time, Terraform itself will catch it. Casper is for *organizational* rules — naming, tagging, deletion protection, mandatory reviews — that Terraform's type system doesn't express.

## Reload behavior

Casper watches `.casper/policies.yaml` along with the rest of the repo. Edit the file, save, and the next tool call uses the new policies — no server restart needed.

## See also

- `docs/TOOLS.md#simulate_impact` — full response schema for `policy_violations[]` and `workflow_decision`
- `docs/TOOLS.md#dump_graph` — per-resource violations across the whole repo
- `README.md` — quick policy snippet in the main quickstart
