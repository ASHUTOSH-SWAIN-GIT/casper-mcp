# Test Repositories for Casper

Good public Terraform repos for testing ingestion, graph layout, and MCP tools.
Run against any of these with a single command — no config or database needed:

```sh
casper-mcp export --dir <cloned-repo> --output graph.html
```

---

## Tier 1 — Best for testing (large + varied)

### 1. terraform-aws-modules/terraform-aws-vpc
```sh
git clone https://github.com/terraform-aws-modules/terraform-aws-vpc
```
- ~78 `.tf` files, 8 submodules (vpc-endpoints, network-acls, flow-log, etc.)
- Good convention coverage: `aws_vpc`, `aws_subnet`, `aws_route_table`, `aws_security_group`
- Already cloned at `~/code/terraform-aws-vpc`
- **Best for:** module convention extraction, submodule relationship graph

---

### 2. aws-ia/terraform-aws-eks-blueprints
```sh
git clone https://github.com/aws-ia/terraform-aws-eks-blueprints
```
- 50+ pattern examples, 20+ modules
- Covers EKS, VPC, IAM, Karpenter, add-ons
- Rich resource variety: `aws_eks_cluster`, `aws_iam_role`, `aws_security_group`, `aws_lb`
- **Best for:** large node count (~200+ nodes), cross-module edge visualization

---

### 3. cloudposse/terraform-aws-components
```sh
git clone https://github.com/cloudposse/terraform-aws-components
```
- 500+ individual components
- Covers nearly every AWS service
- **Best for:** stress-testing layout with 1000+ nodes, community clustering at scale
- Note: large repo (~300MB), takes a few minutes to clone

---

### 4. gruntwork-io/terragrunt-infrastructure-live-example
```sh
git clone https://github.com/gruntwork-io/terragrunt-infrastructure-live-example
```
- Realistic multi-account, multi-region layout (`dev/`, `staging/`, `prod/`)
- Uses Terragrunt modules (still valid `.tf` files)
- **Best for:** testing environment-based community clustering, realistic project structure

---

## Tier 2 — Focused / medium size

### 5. terraform-aws-modules/terraform-aws-rds
```sh
git clone https://github.com/terraform-aws-modules/terraform-aws-rds
```
- 6 submodules (db-instance, db-subnet-group, db-option-group, etc.)
- Deep `aws_db_instance` convention coverage
- **Best for:** testing `get_conventions` and `find_similar` MCP tools on RDS resources

---

### 6. terraform-aws-modules/terraform-aws-ecs
```sh
git clone https://github.com/terraform-aws-modules/terraform-aws-ecs
```
- ECS cluster, service, task definition modules
- **Best for:** `aws_ecs_cluster`, `aws_ecs_service` convention extraction

---

### 7. terraform-aws-modules/terraform-aws-eks
```sh
git clone https://github.com/terraform-aws-modules/terraform-aws-eks
```
- 8 submodules, comprehensive EKS coverage
- **Best for:** complex module hierarchy, `aws_eks_node_group` conventions

---

### 8. antonbabenko/terraform-aws-atlantis
```sh
git clone https://github.com/antonbabenko/terraform-aws-atlantis
```
- Single-purpose: deploys Atlantis on AWS (ECS + ALB + Route53)
- Small enough to inspect every node individually
- **Best for:** understanding the full graph of a real single-service deployment

---

## Tier 3 — Has `.tfstate` fixtures (state resource graph)

### 9. adavarski/AWS-EKS-Terraform
```sh
git clone https://github.com/adavarski/AWS-EKS-Terraform
```
- Includes example `.tfstate` files in some branches
- Covers EKS + VPC + RDS stack
- **Best for:** testing the state resource graph (actual deployed resource nodes, not just modules)

---

### 10. Your own infrastructure (best option)
If you have a local Terraform workspace with real state:
```sh
casper-mcp export --dir ~/path/to/your/infra --output graph.html
```
- Real `.tfstate` = real resource nodes with actual IDs, tags, attributes
- Casper's MCP tools (`find_resource`, `get_dependencies`) are most useful here
- State files are never committed to the repo above — this is the only way to get them

---

## Quick comparison

| Repo | `.tf` files | Modules | `.tfstate` | Nodes (approx) | Best for |
|---|---|---|---|---|---|
| terraform-aws-vpc | ~78 | 8 | no | ~90 | conventions, submodules |
| terraform-aws-eks-blueprints | ~200 | 50+ | no | ~220 | large graph, clustering |
| cloudposse/terraform-aws-components | 5000+ | 500+ | no | 1500+ | stress test |
| terragrunt-live-example | ~40 | 10 | no | ~60 | multi-env structure |
| terraform-aws-rds | ~30 | 6 | no | ~40 | RDS conventions |
| AWS-EKS-Terraform | ~60 | 15 | yes (some) | ~80 | state graph |
| your own infra | varies | varies | yes | varies | real data |

---

## Tips

- **Combine repos** in one scan to get a richer graph. Point `--dir` at a parent folder containing multiple cloned repos.
- **State files matter** — without `.tfstate`, you only see modules and conventions (green/amber nodes). With state, you get the actual deployed resources (blue nodes) and dependency edges.
- **For MCP tools**, ingest into postgres first (`casper-mcp ingest`) so Claude can query `find_resource`, `get_dependencies`, and `find_similar` during coding sessions.
