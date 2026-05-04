package awslive

import (
	"context"
	"fmt"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

const maxScopeResources = 20

// metaTypes are Terraform graph meta-entries that aren't real AWS resources.
var metaTypes = map[string]bool{
	"terraform_module":     true,
	"terraform_convention": true,
}

// ResolveScope determines the set of graph.Resources to describe.
//
// If resourceIDs is non-empty, each entry is looked up via FindResources (accepts
// both internal IDs and type.name identifiers) and the first match is used.
//
// If intent is provided, FindResources is called and 1-hop outbound dependencies
// (the resources the target depends on: SGs, subnets, IAM roles) are included.
//
// Returns an error if the resolved set exceeds maxScopeResources.
func ResolveScope(
	ctx context.Context,
	store graph.Querier,
	intent string,
	resourceIDs []string,
) ([]graph.Resource, error) {
	seen := map[string]bool{}
	var result []graph.Resource

	add := func(r graph.Resource) {
		if seen[r.ID] || metaTypes[r.Type] {
			return
		}
		seen[r.ID] = true
		result = append(result, r)
	}

	if len(resourceIDs) > 0 {
		for _, id := range resourceIDs {
			matches, err := store.FindResources(ctx, id, 1)
			if err != nil {
				return nil, fmt.Errorf("resolve %q: %w", id, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("resource %q not found in Casper graph — verify with find_resource", id)
			}
			add(matches[0])
		}
	} else {
		matches, err := store.FindResources(ctx, intent, 10)
		if err != nil {
			return nil, fmt.Errorf("resolve intent %q: %w", intent, err)
		}
		for _, r := range matches {
			add(r)
		}
		// Walk 1-hop outbound dependencies (things the target depends on).
		for _, r := range matches {
			deps, err := store.GetDependencies(ctx, r.ID)
			if err != nil {
				continue
			}
			for _, d := range deps {
				if d.Direction == "dependency" {
					add(d.Resource)
				}
			}
		}
	}

	if len(result) > maxScopeResources {
		if len(resourceIDs) > 0 {
			return nil, fmt.Errorf(
				"%d resource_ids resolved to %d resources (max %d) — reduce the list",
				len(resourceIDs), len(result), maxScopeResources,
			)
		}
		return nil, fmt.Errorf(
			"intent %q resolved to %d resources (max %d) — use resource_ids to narrow scope",
			intent, len(result), maxScopeResources,
		)
	}

	return result, nil
}
