package awslive_test

import (
	"context"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

func makeSnapshot() graph.GraphSnapshot {
	return graph.GraphSnapshot{
		Resources: []graph.Resource{
			{ID: "r1", Type: "aws_db_instance", Identifier: "aws_db_instance.orders_main"},
			{ID: "r2", Type: "aws_security_group", Identifier: "aws_security_group.orders_db"},
			{ID: "r3", Type: "aws_subnet", Identifier: "aws_subnet.private_a"},
			{ID: "r4", Type: "terraform_module", Identifier: "module.orders"},
			{ID: "r5", Type: "aws_db_instance", Identifier: "aws_db_instance.payments_main"},
		},
		Dependencies: []graph.Dependency{
			{FromResource: "r1", ToResource: "r2", Kind: "reference"},
			{FromResource: "r1", ToResource: "r3", Kind: "reference"},
		},
	}
}

func TestResolveScope_ByResourceID(t *testing.T) {
	store := graph.NewMemStore(makeSnapshot())
	scope, err := awslive.ResolveScope(context.Background(), store, "", []string{"aws_db_instance.orders_main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 1 || scope[0].Identifier != "aws_db_instance.orders_main" {
		t.Fatalf("expected orders_main only, got %+v", scope)
	}
}

func TestResolveScope_ByIntent_IncludesOnehopDeps(t *testing.T) {
	store := graph.NewMemStore(makeSnapshot())
	scope, err := awslive.ResolveScope(context.Background(), store, "orders_main", nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range scope {
		ids[r.Identifier] = true
	}
	if !ids["aws_db_instance.orders_main"] {
		t.Error("missing orders_main in scope")
	}
	if !ids["aws_security_group.orders_db"] {
		t.Error("missing orders_db sg in scope")
	}
	if !ids["aws_subnet.private_a"] {
		t.Error("missing private_a subnet in scope")
	}
}

func TestResolveScope_ExcludesMetaTypes(t *testing.T) {
	store := graph.NewMemStore(makeSnapshot())
	scope, err := awslive.ResolveScope(context.Background(), store, "orders", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range scope {
		if r.Type == "terraform_module" {
			t.Errorf("meta type %q must not appear in scope", r.Identifier)
		}
	}
}

func TestResolveScope_ErrorOnUnknownResourceID(t *testing.T) {
	store := graph.NewMemStore(makeSnapshot())
	_, err := awslive.ResolveScope(context.Background(), store, "", []string{"aws_db_instance.nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown resource ID")
	}
}

func TestResolveScope_CapAt20(t *testing.T) {
	var resources []graph.Resource
	var ids []string
	for i := 0; i < 25; i++ {
		id := "aws_instance.app_" + string(rune('a'+i))
		resources = append(resources, graph.Resource{
			ID:         id,
			Type:       "aws_instance",
			Identifier: id,
		})
		ids = append(ids, id)
	}
	store := graph.NewMemStore(graph.GraphSnapshot{Resources: resources})
	_, err := awslive.ResolveScope(context.Background(), store, "", ids)
	if err == nil {
		t.Fatal("expected cap error when scope exceeds 20")
	}
}

func TestResolveScope_DeduplicatesResources(t *testing.T) {
	store := graph.NewMemStore(makeSnapshot())
	scope, err := awslive.ResolveScope(context.Background(), store, "",
		[]string{"aws_db_instance.orders_main", "aws_db_instance.orders_main"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scope) != 1 {
		t.Fatalf("expected 1 after dedup, got %d", len(scope))
	}
}
