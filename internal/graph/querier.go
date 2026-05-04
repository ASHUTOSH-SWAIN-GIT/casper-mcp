package graph

import "context"

// Querier is the query interface used by the MCP server.
// Both Store (postgres) and MemStore (in-memory) implement it.
type Querier interface {
	FindResources(ctx context.Context, query string, limit int) ([]Resource, error)
	GetDependencies(ctx context.Context, resourceID string) ([]DependencyResult, error)
	FindModules(ctx context.Context, intent string, limit int) ([]Resource, error)
	FindConventions(ctx context.Context, resourceType string, limit int) ([]Resource, error)
	FindSimilar(ctx context.Context, description string, limit int) ([]Resource, error)
}

// Snapshotter is optionally implemented by stores that hold an in-memory
// snapshot (e.g. LiveStore). dump_graph uses this to return the full graph.
type Snapshotter interface {
	Snapshot() GraphSnapshot
}
