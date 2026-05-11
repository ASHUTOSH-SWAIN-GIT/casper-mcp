# Casper MCP Tools

Tools exposed by the Casper MCP server. The agent calls these to query and reason about a Terraform infrastructure graph built from `.tf` and `.tfstate` files.

---

## find_resource

**Purpose:** Find Terraform-managed infrastructure resources by name, type, tag, or attribute.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `query` | yes | Search query, e.g. `orders-prod` or `aws_db_instance` |

**Returns:** Array of matching resources with their identifier, type, module path, and attributes.

**When to use:** When you need to look up whether a specific resource already exists before writing Terraform for it.

---

## get_dependencies

**Purpose:** Get resources that a given resource depends on, plus resources that depend on it.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `resource_id` | yes | Casper resource ID returned by `find_resource` |
| `limit` | no | Max results to return (default 50, max 500) |

**Returns:** Dependency graph for the resource — both upstream (what it needs) and downstream (what needs it).

**When to use:** Before modifying a resource, to understand what else might break.

---

## get_module_for

**Purpose:** Find Terraform modules that match an infrastructure intent.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `intent` | yes | Infrastructure intent, e.g. `postgres database`, `rds`, `read replica` |

**Returns:** Up to 10 modules from the repo that match the intent.

**When to use:** When you want to know which reusable module to call rather than writing raw resources.

---

## get_conventions

**Purpose:** Summarize Terraform code conventions for a given resource type.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `resource_type` | yes | Terraform resource type, e.g. `aws_db_instance` or `aws_security_group` |

**Returns:** Up to 10 existing resources of that type, showing the argument patterns used across the codebase.

**When to use:** Before writing a new resource block, to match the team's naming and configuration conventions.

---

## find_similar

**Purpose:** Find similar Terraform resources or modules that can be used as implementation examples.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `description` | yes | Natural-language description, e.g. `read replica`, `postgres database`, `security group` |

**Returns:** Up to 10 resources scored by similarity, with their HCL arguments included. Supports synonym expansion (e.g. `rds` → `aws_db_instance`, `vpc` → `aws_vpc`).

**When to use:** When you want concrete examples from the repo to base your Terraform on.

---

## get_context

**Purpose:** Get everything Casper knows about an infrastructure intent in a single call — existing resources, similar examples, matching modules, and conventions. Use this instead of calling `find_resource`, `find_similar`, `get_module_for`, and `get_conventions` separately.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `intent` | yes | What you are trying to build, e.g. `postgres read replica`, `S3 bucket with versioning`, `EKS node group` |

**Returns:** A JSON object with up to 4 sections, each containing up to 5 resources. Resources are deduplicated across sections — if the same resource scores highly in multiple categories it only appears once.

| Section | Source | Description |
|---------|--------|-------------|
| `existing_resources` | `find_resource` | Resources already deployed that match the intent |
| `similar_examples` | `find_similar` | Resources to use as implementation templates (with HCL args) |
| `modules` | `get_module_for` | Reusable modules that deliver the intent |
| `conventions` | `get_conventions` | How this resource type is configured across the codebase |

**When to use:** As the first call whenever you start working on an infrastructure task. One call replaces four.

---

## simulate_impact

**Purpose:** Parse proposed Terraform HCL and show what would change in the infrastructure graph — which resources get created or modified, what currently-deployed resources are in the blast radius, any broken references, and similar real examples from the repo.

**Input:**
| Param | Required | Description |
|-------|----------|-------------|
| `code` | yes | Proposed Terraform HCL — one or more `resource` blocks |

**Returns:**

| Field | Description |
|-------|-------------|
| `summary` | One-line count: `N created, M modified, K in blast radius` |
| `created` | Resources that don't exist yet — full argument list |
| `modified` | Resources that exist but would change — added/changed/removed args |
| `blast_radius` | Currently-deployed resources that reference a modified resource (downstream), or that a new resource will reference (upstream) |
| `warnings` | Broken references: proposed args reference a `type.name` not in the current graph |
| `similar_examples` | For each proposed resource type, up to 3 real examples from the repo with their HCL arguments |
| `reversibility_context` | Per-resource facts the agent uses to reason about rollback risk (see below) |
| `policy_violations` | List of org policy rules violated by the proposed change (see below) |

**`reversibility_context.resources[]` fields:**

| Field | Description |
|-------|-------------|
| `operation` | `create` or `modify` (proposed resources only — the tool analyzes partial changes, not full state replacement) |
| `current_args` | Arguments as they exist in the graph right now |
| `proposed_args` | Arguments as they would be after apply |
| `changed_args` | Per-argument before/after diff (modify only) |
| `added_args` | New arguments being added (modify only) |
| `removed_args` | Arguments being removed (modify only) |
| `lifecycle_flags.prevent_destroy` | Whether the proposed block has `lifecycle { prevent_destroy = true }` |
| `lifecycle_flags.create_before_destroy` | Whether the proposed block has `lifecycle { create_before_destroy = true }` |
| `lifecycle_flags.deletion_protection` | Whether `deletion_protection = true` is set in the resource args |
| `dependents` | Identifiers of existing resources that reference this one — affected by a rollback |
| `depends_on` | Identifiers this proposed resource references — must exist for rollback to succeed |
| `recent_commits` | Last 3 git commits that touched this resource block (hash, message, author, date). Uses git pickaxe to find exact changes to the block; falls back to recent `.tf` commits in the module dir. Only populated for modify operations. |

**`policy_violations[]` fields:**

| Field | Description |
|-------|-------------|
| `policy_id` | ID from `.casper/policies.yaml` |
| `resource` | Resource identifier (e.g. `aws_db_instance.orders`) |
| `type` | Resource type |
| `message` | Policy-level description of what the rule enforces |
| `details` | Specific failure reason (e.g. `argument "deletion_protection" must be "true" (not set)`) |

Policies are defined in `.casper/policies.yaml` in the scanned repo. Supported rule types: `must_equal`, `must_not_equal`, `required`, `min_value`. Applies to `resource` (specific type) or `"*"` (all types).

**`workflow_decision` fields:**

| Field | Description |
|-------|-------------|
| `decision` | Advisory routing outcome: `allow`, `require_approval`, `require_security_review`, or `block` |
| `matched_rules[]` | Rules that fired — each has `id` and `reason` describing the matching facts |
| `required_steps[]` | Ordered steps the team should complete before applying (e.g. `["get_team_lead_approval"]`) |
| `blocked` | `true` when decision is `block` — the change should not proceed |
| `blocked_reason` | Human-readable reason from the first blocking rule |

`workflow_decision` is advisory — the agent should follow it but no hard enforcement happens. Rules are defined in the `workflow_rules:` key in `.casper/policies.yaml`. Each rule has a `when:` condition (all non-empty fields must match) and a `decision:`. First match per resource wins; strictest decision across all resources in the change wins overall.

`when:` condition fields:

| Field | Description |
|-------|-------------|
| `env` | Environment detected from tags, module path, or identifier (`prod`, `staging`, `dev`). Fails closed to `prod` if undetectable |
| `operation` | `create`, `modify`, or `destroy` — string or list |
| `resource_type_family` | Broad family: `database`, `iam`, `network_security`, `compute`, `storage` |
| `resource_type` | Exact Terraform resource type, e.g. `aws_db_instance` |

Example:
```yaml
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

**When to use:** After drafting Terraform, before asking the user to apply it — to validate correctness, understand side effects, check org policy compliance, and reason about whether each change can be safely rolled back.

---

## describe_live_state

**Purpose:** Query live AWS state for a set of resources and compare against Terraform-managed state to detect drift. Resolves scope from a natural-language intent or explicit resource IDs, calls read-only AWS Describe APIs for each resource, and returns per-resource state plus any drift between Terraform and AWS.

**Requires:** `cloud.aws` section in `.casper/config.yaml` with `role_arn` and `regions`. Casper assumes the role for all describe calls — never writes.

**Input:**

| Param | Required | Description |
|-------|----------|-------------|
| `intent` | one of `intent` or `resource_ids` | Natural-language description, e.g. `orders database`, `payments service`. Resolves via graph search + 1-hop dependency walk |
| `resource_ids` | one of `intent` or `resource_ids` | Array of Casper resource identifiers — either `type.name` or internal ID. Skips graph resolution |

Scope is capped at 20 resources per call. If intent resolves more, narrow with `resource_ids`.

**Returns:**

| Field | Description |
|-------|-------------|
| `scope_resources` | Identifiers of all resources included in this query |
| `resources[]` | Per-resource state comparison (see below) |
| `not_in_terraform[]` | Resources AWS returned that Terraform doesn't track (v0.1: SG rules only) |
| `errors[]` | Per-resource failures — do not cause the tool call to fail |

**`resources[]` fields:**

| Field | Description |
|-------|-------------|
| `identifier` | Casper identifier, e.g. `aws_db_instance.orders_main` |
| `type` | Terraform resource type |
| `terraform_state` | Full `attributes` map from the Casper graph (sourced from state files) |
| `live_aws_state` | Flattened key→value map from the AWS Describe API response |
| `drift[]` | Fields present in Terraform state whose value differs from AWS — each entry has `field`, `terraform_value`, `aws_value` |

**Supported resource types:**

| Type | AWS API | Notes |
|------|---------|-------|
| `aws_db_instance` | `rds:DescribeDBInstances` | |
| `aws_rds_cluster` | `rds:DescribeDBClusters` | |
| `aws_db_subnet_group` | `rds:DescribeDBSubnetGroups` | |
| `aws_security_group` | `ec2:DescribeSecurityGroups` | |
| `aws_subnet` | `ec2:DescribeSubnets` | |
| `aws_vpc` | `ec2:DescribeVpcs` | |
| `aws_instance` | `ec2:DescribeInstances` | |
| `aws_s3_bucket` | `s3:HeadBucket` + versioning + tags | Partial: existence, versioning, tags only |
| `aws_iam_role` | `iam:GetRole` | IAM is global — any configured region is used |
| `aws_lambda_function` | `lambda:GetFunction` | |
| `aws_eks_cluster` | `eks:DescribeCluster` | |

Unsupported types produce an entry in `errors[]` with the full `supported_types` list.

**Region strategy:** Tries each configured region in order, stops on first success.

**Auth configuration:**

```yaml
# .casper/config.yaml
cloud:
  aws:
    role_arn: arn:aws:iam::123456789012:role/casper-readonly
    regions: [ap-south-1, us-east-1]
```

If the section is absent, the tool returns an error explaining how to configure it.

**When to use:** Before modifying or destroying a resource, to confirm what's actually deployed versus what state files claim — especially after manual changes or partial applies.

---

## dump_graph

**Purpose:** Return the complete infrastructure graph in a single call — all resources, all dependency edges, resource counts by type, and policy violations evaluated per resource. Designed to bootstrap a client-side graph view.

**Input:** None.

**Returns:**

| Field | Description |
|-------|-------------|
| `fetched_at` | ISO-8601 timestamp of when this snapshot was taken |
| `resource_count` | Total number of resources in the graph |
| `dep_count` | Total number of dependency edges |
| `resources_by_type[]` | Array of `{ type, count }` — resource counts grouped by Terraform type |
| `resources[]` | All resources (see below) |
| `dependencies[]` | All edges as `{ from, to, kind }` |

**`resources[]` fields:**

| Field | Description |
|-------|-------------|
| `id` | Internal Casper resource ID |
| `type` | Terraform resource type, e.g. `aws_db_instance` |
| `identifier` | `type.name` identifier, e.g. `aws_db_instance.orders_main` |
| `module_path` | Source path of the `.tf` file that defines this resource |
| `source` | Directory the resource was scanned from |
| `attributes` | Full attributes map (includes `arguments` sub-map with HCL arg values) |
| `tags` | Tag key/value map |
| `policy_violations[]` | Policy violations for this resource — same shape as `simulate_impact` violations |

**Notes:**
- Only available in `serve` mode (requires a `LiveStore` — not available in `ingest`/`watch`)
- For repos with many resources this response can be large — use `find_resource` or `get_context` for targeted queries
- Intended for client-side graph rendering and caching; cache invalidation should be driven by SSE events from the server

**When to use:** To seed a client-side graph store on connect, or for full-repo analysis that needs every resource at once.
