package graph

import "testing"

func TestTokenizeQuery(t *testing.T) {
	tokens := tokenizeQuery("read replica for orders-prod")
	if len(tokens) != 5 {
		t.Fatalf("expected 5 tokens, got %d", len(tokens))
	}
	if tokens[0] != "read" || tokens[1] != "replica" || tokens[2] != "for" || tokens[3] != "orders" || tokens[4] != "prod" {
		t.Fatalf("unexpected tokens: %v", tokens)
	}
}

func TestSimilarityScore(t *testing.T) {
	query := "orders-prod postgres"
	tokens := tokenizeQuery(query)

	db := Resource{
		Type:       "aws_db_instance",
		Identifier: "aws_db_instance.orders_prod",
		Attributes: map[string]any{
			"id":             "orders-prod",
			"identifier":     "orders-prod",
			"engine":         "postgres",
			"instance_class": "db.r5.large",
		},
		Tags: map[string]any{
			"Team": "orders",
		},
	}

	module := Resource{
		Type:       "terraform_module",
		Identifier: "modules/postgres",
		ModulePath: "modules/postgres",
		Attributes: map[string]any{
			"path": "modules/postgres",
			"managed_resources": []map[string]any{
				{"type": "aws_db_instance"},
			},
		},
	}

	if similarityScore(db, query, tokens) <= similarityScore(module, query, tokens) {
		t.Fatalf("expected db resource to score above module for query %q", query)
	}
}
