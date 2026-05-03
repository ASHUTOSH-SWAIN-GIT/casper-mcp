package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

func New(store *graph.Store) *server.MCPServer {
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

	return s
}
