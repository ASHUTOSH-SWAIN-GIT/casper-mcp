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
| `operation` | `create`, `modify`, or `destroy` |
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
| `recent_commits` | Last 3 git commits that touched this resource block (hash, message, author, date). Uses git pickaxe to find exact changes to the block; falls back to recent `.tf` commits in the module dir. Only populated for modify and destroy operations. |

**`policy_violations[]` fields:**

| Field | Description |
|-------|-------------|
| `policy_id` | ID from `.casper/policies.yaml` |
| `resource` | Resource identifier (e.g. `aws_db_instance.orders`) |
| `type` | Resource type |
| `message` | Policy-level description of what the rule enforces |
| `details` | Specific failure reason (e.g. `argument "deletion_protection" must be "true" (not set)`) |

Policies are defined in `.casper/policies.yaml` in the scanned repo. Supported rule types: `must_equal`, `must_not_equal`, `required`, `min_value`. Applies to `resource` (specific type) or `"*"` (all types).

**When to use:** After drafting Terraform, before asking the user to apply it — to validate correctness, understand side effects, check org policy compliance, and reason about whether each change can be safely rolled back.
