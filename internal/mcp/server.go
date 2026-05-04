package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

type SimulateFunc func(code string) (*graph.ImpactResult, error)

func New(store graph.Querier, simulate SimulateFunc) *server.MCPServer {
	s := server.NewMCPServer(
		"casper",
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithInstructions("Use Casper to query infrastructure resources before writing Terraform."),
	)

	s.AddTool(
		mcp.NewTool(
			"find_resource",
			mcp.WithDescription("Find Terraform-managed infrastructure resources by name, type, tag, or attribute."),
			mcp.WithString("query", mcp.Required(), mcp.Description("Search query, such as orders-prod or aws_db_instance.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := request.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resources, err := store.FindResources(ctx, query, 10)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resources) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No resources found for %q.", query)), nil
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
			mcp.WithDescription("Get resources that this resource depends on, plus resources that depend on it."),
			mcp.WithString("resource_id", mcp.Required(), mcp.Description("Casper resource ID returned by find_resource.")),
			mcp.WithNumber("limit", mcp.Description("Maximum number of dependency results to return. Defaults to 50.")),
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
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(dependencies) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No dependencies found for %q.", resourceID)), nil
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
			mcp.WithDescription("Find Terraform modules that match an infrastructure intent."),
			mcp.WithString("intent", mcp.Required(), mcp.Description("Infrastructure intent, such as postgres database, rds, or read replica.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			intent, err := request.RequireString("intent")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			modules, err := store.FindModules(ctx, intent, 10)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(modules) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No Terraform modules found for %q.", intent)), nil
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
			mcp.WithDescription("Summarize Terraform code conventions for a given resource type."),
			mcp.WithString("resource_type", mcp.Required(), mcp.Description("Terraform resource type, such as aws_db_instance or aws_security_group.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceType, err := request.RequireString("resource_type")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			conventions, err := store.FindConventions(ctx, resourceType, 10)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(conventions) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No Terraform conventions found for %q.", resourceType)), nil
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
			mcp.WithDescription("Find similar Terraform resources or modules that can be used as implementation examples."),
			mcp.WithString("description", mcp.Required(), mcp.Description("Natural-language description, such as read replica, postgres database, or security group.")),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			description, err := request.RequireString("description")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			resources, err := store.FindSimilar(ctx, description, 10)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(resources) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No similar resources found for %q.", description)), nil
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
			mcp.WithDescription("Get everything Casper knows that is relevant to an infrastructure intent — existing resources, similar examples, matching modules, and conventions — in a single call. Use this instead of calling find_resource, find_similar, get_module_for, and get_conventions separately."),
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
				return mcp.NewToolResultText(fmt.Sprintf("No context found for %q.", intent)), nil
			}

			payload, err := json.MarshalIndent(out, "", "  ")
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
				mcp.WithDescription("Parse proposed Terraform HCL and show what would change in the infrastructure graph: which resources get created or modified, and what currently-deployed resources are in the blast radius."),
				mcp.WithString("code", mcp.Required(), mcp.Description("Proposed Terraform HCL code — one or more resource blocks.")),
			),
			func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				code, err := request.RequireString("code")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}

				result, err := simulate(code)
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}

				payload, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(err.Error()), nil
				}

				return mcp.NewToolResultText(string(payload)), nil
			},
		)
	}

	return s
}
