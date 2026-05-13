package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
)

type SimulateFunc func(code string) (*graph.ImpactResult, error)

// RenderFunc writes the live graph to disk and returns the absolute output path,
// the directory the graph was scanned from, and resource/edge counts.
// Passed as nil when no HTML rendering is configured.
type RenderFunc func(ctx context.Context) (path string, dir string, resourceCount int, edgeCount int, err error)

// StateSourcesFunc returns the latest per-source fetch status for remote
// Terraform state files (S3 backends, future TFC, etc.). Backed by an atomic
// in main.go so every call reflects the freshest scan. Nil when no remote
// state sources have been discovered.
type StateSourcesFunc func() []ingest.StateSourceStatus

func New(store graph.Querier, simulate SimulateFunc, awsClient *awslive.Client, policies []policy.Policy, render RenderFunc, stateSources StateSourcesFunc) *server.MCPServer {
	s := server.NewMCPServer(
		"casper",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(`Casper gives you a live, queryable view of this repository's Terraform infrastructure. Prefer Casper tools over reading or grepping .tf files — the graph is structured, complete, and stays in sync with disk.

Recommended workflow:
1. For broad questions, call get_context — it returns matching resources, similar examples, modules, and conventions in one shot.
2. For targeted lookups (one resource, all of a type, one provider), call find_resource with type/provider/query filters. NEVER reach for grep/Read on .tf files when find_resource can answer the question.
3. For "what providers / what's deployed at a high level", call list_providers — much cheaper than dump_graph + parsing.
4. Use dump_graph only for full audits or visualizations. Don't dump and re-parse.
5. Before presenting authored Terraform, call simulate_impact — it returns blast radius, broken refs, similar examples, reversibility, and policy violations.

Other tools available: find_similar (HCL examples), get_module_for (reusable modules), get_conventions (codebase patterns), get_dependencies (graph walk), describe_live_state (AWS drift), render_graph (interactive HTML), list_state_sources (which remote S3 backends loaded).`),
	)

	s.AddTool(
		mcp.NewTool(
			"find_resource",
			mcp.WithDescription("Search the graph for Terraform-managed resources. Filter by name substring, resource type (e.g. aws_db_instance), provider (aws, kubernetes, datadog…), tag, or attribute. Returns up to `limit` matches with full arguments and source location.\n\nALWAYS prefer this over running grep/Read on .tf files — it returns structured data, scales to large repos, and stays in sync with the graph. Reach for dump_graph only when you genuinely need every node (audits, full visualizations); for any filtered question, use this tool."),
			mcp.WithTitleAnnotation("Find Resource"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("query", mcp.Description("Free-text query matched against name, type, tags, attributes. Optional if you pass type or provider.")),
			mcp.WithString("type", mcp.Description("Restrict to a Terraform resource type (e.g. aws_db_instance, kubernetes_namespace).")),
			mcp.WithString("provider", mcp.Description("Restrict to a Terraform provider (aws, kubernetes, datadog, snowflake, etc.).")),
			mcp.WithNumber("limit", mcp.Description("Max results. Default 25, max 200."), mcp.Min(1), mcp.Max(200), mcp.DefaultNumber(25)),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, _ := request.GetArguments()["query"].(string)
			typeFilter, _ := request.GetArguments()["type"].(string)
			providerFilter, _ := request.GetArguments()["provider"].(string)
			limit := 25
			if l, ok := request.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			if query == "" && typeFilter == "" && providerFilter == "" {
				return mcp.NewToolResultError("find_resource requires at least one of: query, type, provider"), nil
			}

			// Use a generous internal cap so type/provider filters still work
			// when the free-text query is empty.
			searchQuery := query
			if searchQuery == "" {
				searchQuery = typeFilter
				if searchQuery == "" {
					searchQuery = providerFilter
				}
			}
			resources, err := store.FindResources(ctx, searchQuery, 1000)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("find_resource failed: %v — try get_context for a broader search", err)), nil
			}

			// Apply structured filters in-process so callers can combine query + type + provider.
			filtered := resources[:0]
			for _, r := range resources {
				if typeFilter != "" && r.Type != typeFilter {
					continue
				}
				if providerFilter != "" && r.Provider != providerFilter {
					continue
				}
				filtered = append(filtered, r)
			}
			truncated := false
			if len(filtered) > limit {
				filtered = filtered[:limit]
				truncated = true
			}

			if len(filtered) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No resources matched (query=%q type=%q provider=%q). Widen filters or try get_context.", query, typeFilter, providerFilter)), nil
			}

			payload, err := json.MarshalIndent(map[string]any{
				"matches":   filtered,
				"truncated": truncated,
			}, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	// list_providers: aggregate per-provider counts. Eliminates the need to
	// grep `versions.tf` files just to answer "what providers does this repo use?".
	if snapshotter, ok := store.(graph.Snapshotter); ok {
		s.AddTool(
			mcp.NewTool(
				"list_providers",
				mcp.WithDescription("Return every Terraform provider in use across the repo, with resource counts and the top resource types per provider. Use this instead of grepping versions.tf or required_providers blocks."),
				mcp.WithTitleAnnotation("List Providers"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
			),
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				snap := snapshotter.Snapshot()
				type entry struct {
					Provider     string         `json:"provider"`
					ResourceCount int           `json:"resource_count"`
					TopTypes     []map[string]any `json:"top_types"`
				}
				perProvider := map[string]map[string]int{}
				for _, r := range snap.Resources {
					if r.Provider == "" || r.Type == "terraform_module" || r.Type == "terraform_convention" {
						continue
					}
					if perProvider[r.Provider] == nil {
						perProvider[r.Provider] = map[string]int{}
					}
					perProvider[r.Provider][r.Type]++
				}
				out := make([]entry, 0, len(perProvider))
				for prov, types := range perProvider {
					typeList := make([]map[string]any, 0, len(types))
					total := 0
					for t, c := range types {
						typeList = append(typeList, map[string]any{"type": t, "count": c})
						total += c
					}
					sort.Slice(typeList, func(i, j int) bool {
						ci, cj := typeList[i]["count"].(int), typeList[j]["count"].(int)
						if ci != cj {
							return ci > cj
						}
						return typeList[i]["type"].(string) < typeList[j]["type"].(string)
					})
					if len(typeList) > 5 {
						typeList = typeList[:5]
					}
					out = append(out, entry{Provider: prov, ResourceCount: total, TopTypes: typeList})
				}
				sort.Slice(out, func(i, j int) bool {
					if out[i].ResourceCount != out[j].ResourceCount {
						return out[i].ResourceCount > out[j].ResourceCount
					}
					return out[i].Provider < out[j].Provider
				})
				payload, _ := json.MarshalIndent(map[string]any{
					"providers":      out,
					"provider_count": len(out),
				}, "", "  ")
				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	s.AddTool(
		mcp.NewTool(
			"get_dependencies",
			mcp.WithDescription("Return the dependency graph for a specific resource: what it depends on and what depends on it. Use the resource ID from find_resource or get_context results."),
			mcp.WithTitleAnnotation("Get Dependencies"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("resource_id", mcp.Required(), mcp.Description("Casper resource ID returned by find_resource or get_context.")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of dependency results to return."), mcp.Min(1), mcp.Max(500), mcp.DefaultNumber(50)),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceID, err := request.RequireString("resource_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := 50
			if l, ok := request.GetArguments()["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}

			dependencies, err := store.GetDependencies(ctx, resourceID)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("get_dependencies failed: %v — verify the resource_id with find_resource first", err)), nil
			}
			if len(dependencies) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No dependencies found for %q. Verify the resource_id with find_resource.", resourceID)), nil
			}

			truncated := false
			if len(dependencies) > limit {
				dependencies = dependencies[:limit]
				truncated = true
			}

			type depResponse struct {
				Dependencies []graph.DependencyResult `json:"dependencies"`
				Truncated    bool                     `json:"truncated,omitempty"`
			}
			payload, err := json.MarshalIndent(depResponse{Dependencies: dependencies, Truncated: truncated}, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	s.AddTool(
		mcp.NewTool(
			"get_module_for",
			mcp.WithDescription("Find Terraform modules in this repository that match an infrastructure intent. Returns module source paths and their arguments."),
			mcp.WithTitleAnnotation("Get Module For Intent"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("intent", mcp.Required(), mcp.Description("Infrastructure intent, such as postgres database, rds, or read replica.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			intent, err := request.RequireString("intent")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			modules, err := store.FindModules(ctx, intent, 10)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("get_module_for failed: %v — try get_context for a combined lookup", err)), nil
			}
			if len(modules) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No Terraform modules found for %q. Try find_similar for individual resource examples.", intent)), nil
			}

			payload, err := json.MarshalIndent(modules, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	s.AddTool(
		mcp.NewTool(
			"get_conventions",
			mcp.WithDescription("Summarize how a specific Terraform resource type is configured across this codebase — common argument patterns, typical values, and naming conventions."),
			mcp.WithTitleAnnotation("Get Conventions"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("resource_type", mcp.Required(), mcp.Description("Terraform resource type, such as aws_db_instance or aws_security_group.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceType, err := request.RequireString("resource_type")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			conventions, err := store.FindConventions(ctx, resourceType, 10)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("get_conventions failed: %v — try get_context for a combined lookup", err)), nil
			}
			if len(conventions) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No conventions found for %q. Try find_similar to see concrete HCL examples.", resourceType)), nil
			}

			payload, err := json.MarshalIndent(conventions, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	s.AddTool(
		mcp.NewTool(
			"find_similar",
			mcp.WithDescription("Find existing Terraform resources in this repository that match a description, returned as HCL examples. Use find_resource when you need to search by exact name or ID."),
			mcp.WithTitleAnnotation("Find Similar Resources"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("description", mcp.Required(), mcp.Description("Natural-language description, such as read replica, postgres database, or security group.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			description, err := request.RequireString("description")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resources, err := store.FindSimilar(ctx, description, 10)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("find_similar failed: %v — try get_context for a combined lookup", err)), nil
			}
			if len(resources) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No similar resources found for %q. Try find_resource with a more specific name or type.", description)), nil
			}

			payload, err := json.MarshalIndent(resources, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	s.AddTool(
		mcp.NewTool(
			"get_context",
			mcp.WithDescription("Returns everything relevant to an infrastructure intent in one call: existing resources, similar HCL examples, matching modules, and conventions. Covers the same ground as find_resource + find_similar + get_module_for + get_conventions combined."),
			mcp.WithTitleAnnotation("Get Context"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("intent", mcp.Required(), mcp.Description("What you are trying to build or understand, e.g. 'postgres read replica', 'S3 bucket with versioning', 'EKS node group'.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			intent, err := request.RequireString("intent")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			type section struct {
				name    string
				results []graph.Resource
				err     error
			}
			sections := make([]section, 4)
			sections[0].name = "existing_resources"
			sections[1].name = "similar_examples"
			sections[2].name = "modules"
			sections[3].name = "conventions"

			var wg sync.WaitGroup
			wg.Add(4)
			go func() { defer wg.Done(); sections[0].results, sections[0].err = store.FindResources(ctx, intent, 5) }()
			go func() { defer wg.Done(); sections[1].results, sections[1].err = store.FindSimilar(ctx, intent, 5) }()
			go func() { defer wg.Done(); sections[2].results, sections[2].err = store.FindModules(ctx, intent, 5) }()
			go func() { defer wg.Done(); sections[3].results, sections[3].err = store.FindConventions(ctx, intent, 5) }()
			wg.Wait()

			// Deduplicate across sections by ID — a resource that scores highly
			// in multiple categories shouldn't be repeated.
			seen := map[string]bool{}
			out := map[string]any{}
			var queryErrors []string
			for _, sec := range sections {
				if sec.err != nil {
					queryErrors = append(queryErrors, fmt.Sprintf("%s: %v", sec.name, sec.err))
					continue
				}
				var deduped []graph.Resource
				for _, r := range sec.results {
					if seen[r.ID] {
						continue
					}
					seen[r.ID] = true
					deduped = append(deduped, r)
				}
				if len(deduped) > 0 {
					out[sec.name] = deduped
				}
			}
			if len(queryErrors) > 0 {
				out["errors"] = queryErrors
			}

			if len(seen) == 0 && len(queryErrors) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No context found for %q. Try a broader or different intent.", intent)), nil
			}

			payload, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	s.AddTool(
		mcp.NewTool(
			"describe_live_state",
			mcp.WithDescription("Query live AWS state for a set of resources and compare against Terraform-managed state to detect drift. Resolves scope from an intent (natural language) or explicit resource IDs, then calls read-only AWS Describe APIs for each resource. Returns terraform_state, live_aws_state, drift fields, and any resources AWS shows that Terraform doesn't track. Coverage: aws_db_instance, aws_rds_cluster, aws_db_subnet_group, aws_security_group, aws_subnet, aws_vpc, aws_instance, aws_s3_bucket (existence/versioning/tags), aws_iam_role, aws_lambda_function, aws_eks_cluster. not_in_terraform: SG rules only in v0.1."),
			mcp.WithTitleAnnotation("Describe Live State"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithString("intent", mcp.Description("Natural-language description of the resources to query, e.g. 'orders database' or 'payments service'. Resolved via the graph using the same logic as find_resource + dependency walk.")),
			mcp.WithArray("resource_ids", mcp.Description("Explicit Casper resource identifiers (type.name or internal ID). Skips graph resolution. At least one of intent or resource_ids is required.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if awsClient == nil {
				return mcp.NewToolResultError(
					"AWS client not configured — add a cloud.aws section to .casper/config.yaml with role_arn and regions, then restart the server",
				), nil
			}

			intent, _ := request.GetArguments()["intent"].(string)
			var resourceIDs []string
			if raw, ok := request.GetArguments()["resource_ids"].([]any); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						resourceIDs = append(resourceIDs, s)
					}
				}
			}

			if intent == "" && len(resourceIDs) == 0 {
				return mcp.NewToolResultError("at least one of intent or resource_ids is required"), nil
			}

			scope, err := awslive.ResolveScope(ctx, store, intent, resourceIDs)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("scope resolution failed: %v", err)), nil
			}
			if len(scope) == 0 {
				msg := fmt.Sprintf("no resources found for intent %q — try find_resource to verify identifiers", intent)
				if len(resourceIDs) > 0 {
					msg = "none of the provided resource_ids matched any Casper resources — verify with find_resource"
				}
				return mcp.NewToolResultText(msg), nil
			}

			scopeIDs := make([]string, len(scope))
			for i, r := range scope {
				scopeIDs[i] = r.Identifier
			}

			result := &awslive.LiveStateResult{ScopeResources: scopeIDs}

			for _, r := range scope {
				awsAttrs, unmanaged, err := awslive.Describe(ctx, awsClient, r)
				if err != nil {
					if errors.Is(err, awslive.ErrNotSupported) {
						result.Errors = append(result.Errors, awslive.ResourceError{
							Resource: r.Identifier,
							Error:    err.Error(),
						})
						continue
					}
					result.Errors = append(result.Errors, awslive.ResourceError{
						Resource: r.Identifier,
						Error:    fmt.Sprintf("AWS describe failed: %v", err),
					})
					continue
				}

				tfState := make(map[string]any)
				for k, v := range r.Attributes {
					tfState[k] = v
				}

				rs := awslive.ResourceState{
					Identifier:     r.Identifier,
					Type:           r.Type,
					TerraformState: tfState,
					LiveAWSState:   awsAttrs,
					Drift:          awslive.DetectDrift(r.Attributes, awsAttrs),
				}
				result.Resources = append(result.Resources, rs)
				result.NotInTerraform = append(result.NotInTerraform, unmanaged...)
			}

			payload, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(string(payload)), nil
		},
	)

	if simulate != nil {
		s.AddTool(
			mcp.NewTool(
				"simulate_impact",
				mcp.WithDescription("Parse proposed Terraform HCL and return a full impact analysis: created/modified resources with argument diffs, blast radius (existing resources affected), broken-reference warnings, similar real examples from this repo, reversibility context (lifecycle flags, dependents, recent commits), and policy violations."),
				mcp.WithTitleAnnotation("Simulate Impact"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithString("code", mcp.Required(), mcp.Description("Proposed Terraform HCL code — one or more resource blocks.")),
			),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				code, err := request.RequireString("code")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}

				result, err := simulate(code)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("simulate_impact failed: %v — check that the HCL is valid Terraform syntax", err)), nil
				}

				payload, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}

				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	// list_state_sources: surface what remote state backends Casper detected
	// and whether the last fetch succeeded. Lets the agent explain to the user
	// when state is missing (e.g. AccessDenied on a specific S3 key) instead
	// of silently producing a graph with gaps.
	if stateSources != nil {
		s.AddTool(
			mcp.NewTool(
				"list_state_sources",
				mcp.WithDescription("List every remote Terraform state backend Casper discovered in this repo's .tf files, with the last fetch status. Use this when:\n- The user asks what state Casper is seeing.\n- A resource the user expects is missing from the graph — check whether its state file failed to load (AccessDenied, NoSuchKey, etc.).\n- You want to confirm the graph reflects remote state, not just declared HCL.\n\nReturns one entry per backend (currently `s3` only) with bucket/key/region, the .tf file declaring it, and `status: \"loaded\"|\"failed\"`. Failed entries include the error string verbatim so you can suggest a fix (typically AWS auth)."),
				mcp.WithTitleAnnotation("List State Sources"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
			),
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				sources := stateSources()
				if len(sources) == 0 {
					return mcp.NewToolResultText("No remote state backends discovered. Casper falls back to local .tfstate files (if any) and code-only graph construction. If you expected state to be loaded, check that the repo has `terraform { backend \"s3\" {} }` blocks in its .tf files."), nil
				}

				loaded, failed := 0, 0
				for _, st := range sources {
					if st.Status == "loaded" {
						loaded++
					} else {
						failed++
					}
				}

				payload, err := json.MarshalIndent(map[string]any{
					"total":   len(sources),
					"loaded":  loaded,
					"failed":  failed,
					"sources": sources,
				}, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	// render_graph: write the interactive HTML graph for the current session's
	// directory. The graph is lazy: nothing is written to disk until this tool
	// (or the watcher, after this tool has been called once) fires. The
	// /casper slash command instructs the agent to call this first.
	if render != nil {
		s.AddTool(
			mcp.NewTool(
				"render_graph",
				mcp.WithDescription("Write the interactive HTML graph (casper/graph.html) for the current Terraform repo. Call this on /casper to materialize the graph the user can open in a browser. The file then auto-updates whenever .tf files change. Returns the absolute path so you can surface it to the user."),
				mcp.WithTitleAnnotation("Render Graph HTML"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
			),
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				path, dir, resCount, edgeCount, err := render(ctx)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("render_graph failed: %v", err)), nil
				}
				payload, _ := json.Marshal(map[string]any{
					"status":         "rendered",
					"path":           path,
					"scanned_dir":    dir,
					"resource_count": resCount,
					"edge_count":     edgeCount,
				})
				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	// dump_graph: only available when the store is a LiveStore (serve mode)
	if snapshotter, ok := store.(graph.Snapshotter); ok {
		s.AddTool(
			mcp.NewTool(
				"dump_graph",
				mcp.WithDescription("Return the COMPLETE infrastructure graph — every resource, every edge, every policy violation. The response is large (often >10k lines); use it only for full-repo audits or when bootstrapping a UI.\n\nDO NOT call this just to filter for one type/provider/name — find_resource is dramatically cheaper and returns the same shape. Don't dump and re-parse with grep/python; pass filters to find_resource instead. Check list_providers if you only need provider counts."),
				mcp.WithTitleAnnotation("Dump Graph"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
			),
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				snap := snapshotter.Snapshot()

				type ResourceEntry struct {
					ID         string             `json:"id"`
					Type       string             `json:"type"`
					Provider   string             `json:"provider,omitempty"`
					Identifier string             `json:"identifier"`
					ModulePath string             `json:"module_path,omitempty"`
					Source     string             `json:"source,omitempty"`
					Attributes map[string]any     `json:"attributes,omitempty"`
					Tags       map[string]any     `json:"tags,omitempty"`
					Violations []policy.Violation `json:"policy_violations,omitempty"`
				}
				type DepEntry struct {
					From string `json:"from"`
					To   string `json:"to"`
					Kind string `json:"kind"`
				}
				type TypeStat struct {
					Type  string `json:"type"`
					Count int    `json:"count"`
				}

				resources := make([]ResourceEntry, 0, len(snap.Resources))
				typeCounts := make(map[string]int)
				for _, r := range snap.Resources {
					typeCounts[r.Type]++
					tags := make(map[string]string)
					for k, v := range r.Tags {
						if s, ok := v.(string); ok {
							tags[k] = s
						}
					}
					var viols []policy.Violation
					if len(policies) > 0 {
						// Code-scanned resources store HCL arg values under
						// attributes.arguments; state-derived ones store them at
						// the top level. Prefer the nested map when present.
						args, _ := r.Attributes["arguments"].(map[string]string)
						if args == nil {
							args = make(map[string]string)
							for k, v := range r.Attributes {
								if s, ok := v.(string); ok {
									args[k] = s
								}
							}
						}
						viols = policy.Check(policies, r.Type, r.Identifier, args, tags)
					}
					resources = append(resources, ResourceEntry{
						ID:         r.ID,
						Type:       r.Type,
						Provider:   r.Provider,
						Identifier: r.Identifier,
						ModulePath: r.ModulePath,
						Source:     r.Source,
						Attributes: r.Attributes,
						Tags:       r.Tags,
						Violations: viols,
					})
				}

				deps := make([]DepEntry, 0, len(snap.Dependencies))
				for _, d := range snap.Dependencies {
					deps = append(deps, DepEntry{From: d.FromResource, To: d.ToResource, Kind: d.Kind})
				}

				typeStats := make([]TypeStat, 0, len(typeCounts))
				for t, c := range typeCounts {
					typeStats = append(typeStats, TypeStat{Type: t, Count: c})
				}

				out := map[string]any{
					"fetched_at":      time.Now().UTC().Format(time.RFC3339),
					"resource_count":  len(resources),
					"dep_count":       len(deps),
					"resources_by_type": typeStats,
					"resources":       resources,
					"dependencies":    deps,
				}
				payload, err := json.MarshalIndent(out, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}
				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	return s
}
