package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

func New(store graph.Querier) *server.MCPServer {
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
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			resourceID, err := request.RequireString("resource_id")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			dependencies, err := store.GetDependencies(ctx, resourceID)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			if len(dependencies) == 0 {
				return mcp.NewToolResultText(fmt.Sprintf("No dependencies found for %q.", resourceID)), nil
			}

			payload, err := json.MarshalIndent(dependencies, "", "  ")
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

	return s
}
