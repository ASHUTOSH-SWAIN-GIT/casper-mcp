package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
)

type SimulateFunc func(code string) (*graph.ImpactResult, error)

func New(store graph.Querier, simulate SimulateFunc, awsClient *awslive.Client, policies []policy.Policy) *server.MCPServer {
	s := server.NewMCPServer(
		"casper",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithInstructions(`Casper gives you a live, queryable view of this repository's Terraform infrastructure.

Recommended workflow:
1. Call get_context first for any infrastructure task — it returns existing resources, similar examples, matching modules, and conventions in one shot.
2. After drafting Terraform, call simulate_impact before presenting code. The response includes created/modified resources, blast radius, broken-reference warnings, similar real examples, reversibility context, and policy violations.

Individual lookup tools (find_resource, find_similar, get_module_for, get_conventions, get_dependencies) are available when you need a targeted follow-up after get_context.`),
	)

	s.AddTool(
		mcp.NewTool(
			"find_resource",
			mcp.WithDescription("Search for Terraform-managed infrastructure resources by name, type, tag, or attribute. Returns matching resources with their arguments. Use find_similar to get resources as HCL examples."),
			mcp.WithTitleAnnotation("Find Resource"),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query, such as orders-prod or aws_db_instance.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := request.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resources, err := store.FindResources(ctx, query, 10)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("find_resource failed: %v — try get_context for a broader search", err)), nil
			}
			if len(resources) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No resources found for %q. Try get_context with a broader intent, or find_similar for example resources.", query)), nil
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

	// dump_graph: only available when the store is a LiveStore (serve mode)
	if snapshotter, ok := store.(graph.Snapshotter); ok {
		s.AddTool(
			mcp.NewTool(
				"dump_graph",
				mcp.WithDescription("Return the complete infrastructure graph: all resources, all dependency edges, resource counts by type, and policy violations on every resource. Use this to bootstrap a client-side graph view or for full-repo analysis. For targeted queries use find_resource or get_context instead."),
				mcp.WithTitleAnnotation("Dump Graph"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
			),
			func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				snap := snapshotter.Snapshot()

				type ResourceEntry struct {
					ID         string         `json:"id"`
					Type       string         `json:"type"`
					Identifier string         `json:"identifier"`
					ModulePath string         `json:"module_path,omitempty"`
					Source     string         `json:"source,omitempty"`
					Attributes map[string]any `json:"attributes,omitempty"`
					Tags       map[string]any `json:"tags,omitempty"`
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
						args := make(map[string]string)
						for k, v := range r.Attributes {
							if s, ok := v.(string); ok {
								args[k] = s
							}
						}
						viols = policy.Check(policies, r.Type, r.Identifier, args, tags)
					}
					resources = append(resources, ResourceEntry{
						ID:         r.ID,
						Type:       r.Type,
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
