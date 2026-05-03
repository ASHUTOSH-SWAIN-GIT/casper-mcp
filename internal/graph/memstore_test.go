package graph

import (
	"context"
	"testing"
)

// testSnapshot builds a small synthetic GraphSnapshot for all MemStore tests.
func testSnapshot() GraphSnapshot {
	resources := []Resource{
		{
			ID:         "r1",
			Type:       "aws_db_instance",
			Identifier: "aws_db_instance.orders",
			ModulePath: "modules/rds",
			Attributes: map[string]any{
				"arguments": map[string]string{
					"engine":              "postgres",
					"instance_class":      "db.t3.medium",
					"deletion_protection": "true",
				},
			},
		},
		{
			ID:         "r2",
			Type:       "aws_db_instance",
			Identifier: "aws_db_instance.replica",
			ModulePath: "modules/rds",
			Attributes: map[string]any{
				"arguments": map[string]string{
					"replicate_source_db": "orders",
					"instance_class":      "db.t3.small",
				},
			},
		},
		{
			ID:         "r3",
			Type:       "aws_vpc",
			Identifier: "aws_vpc.main",
			ModulePath: "modules/vpc",
			Attributes: map[string]any{
				"arguments": map[string]string{"cidr_block": "10.0.0.0/16"},
			},
		},
		{
			ID:         "r4",
			Type:       "aws_security_group",
			Identifier: "aws_security_group.app",
			ModulePath: "modules/vpc",
			Attributes: map[string]any{
				"arguments": map[string]string{
					"vpc_id":      "aws_vpc.main",
					"description": "app servers",
				},
			},
		},
		{
			ID:         "r5",
			Type:       "aws_lambda_function",
			Identifier: "aws_lambda_function.processor",
			ModulePath: "modules/lambda",
		},
	}

	deps := []Dependency{
		{FromResource: "r4", ToResource: "r3", Kind: "reference"},  // sg depends on vpc
		{FromResource: "r2", ToResource: "r1", Kind: "reference"},  // replica depends on primary
	}

	return GraphSnapshot{Resources: resources, Dependencies: deps}
}

func TestFindResources_ExactIdentifier(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindResources(ctx, "aws_db_instance.orders", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if results[0].Identifier != "aws_db_instance.orders" {
		t.Errorf("expected orders, got %s", results[0].Identifier)
	}
}

func TestFindResources_TypeMatch(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindResources(ctx, "aws_db_instance", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 db instances, got %d", len(results))
	}
}

func TestFindResources_SubstringMatch(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindResources(ctx, "orders", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result for 'orders'")
	}
}

func TestFindResources_NoMatch(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindResources(ctx, "nonexistent_resource_xyz", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFindResources_LimitRespected(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindResources(ctx, "aws", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestGetDependencies_Downstream(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	// r3 (vpc) is referenced by r4 (sg) — r4 is a dependent of r3
	deps, err := store.GetDependencies(ctx, "r3")
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, d := range deps {
		if d.Resource.Identifier == "aws_security_group.app" && d.Direction == "dependent" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_security_group.app as a dependent of aws_vpc.main")
	}
}

func TestGetDependencies_Upstream(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	// r4 (sg) depends on r3 (vpc)
	deps, err := store.GetDependencies(ctx, "r4")
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, d := range deps {
		if d.Resource.Identifier == "aws_vpc.main" && d.Direction == "dependency" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_vpc.main as a dependency of aws_security_group.app")
	}
}

func TestGetDependencies_NoDeps(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	deps, err := store.GetDependencies(ctx, "r5") // lambda has no edges
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 deps, got %d", len(deps))
	}
}

func TestFindModules_Match(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindModules(ctx, "rds", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected at least one module matching 'rds'")
	}
	for _, r := range results {
		if r.ModulePath == "" {
			t.Error("module result should have a ModulePath")
		}
	}
}

func TestFindConventions_ExactType(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindConventions(ctx, "aws_db_instance", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 aws_db_instance resources, got %d", len(results))
	}
	for _, r := range results {
		if r.Type != "aws_db_instance" {
			t.Errorf("expected aws_db_instance, got %s", r.Type)
		}
	}
}

func TestFindSimilar_TypeMatch(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindSimilar(ctx, "aws_db_instance", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected results for aws_db_instance")
	}
}

func TestFindSimilar_SynonymExpansion(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	// "rds" should expand to include aws_db_instance synonyms
	results, err := store.FindSimilar(ctx, "rds", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected results for 'rds' via synonym expansion")
	}
	for _, r := range results {
		if r.Type != "aws_db_instance" {
			t.Errorf("expected aws_db_instance results, got %s", r.Type)
		}
	}
}

func TestFindSimilar_ReplicaByArgKey(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	// "replica" should match resources with replicate_source_db argument
	results, err := store.FindSimilar(ctx, "replica", 10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, r := range results {
		if r.Identifier == "aws_db_instance.replica" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_db_instance.replica in results for 'replica' query")
	}
}

func TestFindSimilar_NoMatch(t *testing.T) {
	store := NewMemStore(testSnapshot())
	ctx := context.Background()

	results, err := store.FindSimilar(ctx, "kubernetes_pod", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for unrelated query, got %d", len(results))
	}
}
