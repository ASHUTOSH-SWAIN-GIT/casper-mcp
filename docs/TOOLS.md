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

**When to use:** After drafting Terraform, before asking the user to apply it — to validate correctness, understand side effects, and reason about whether each change can be safely rolled back.
