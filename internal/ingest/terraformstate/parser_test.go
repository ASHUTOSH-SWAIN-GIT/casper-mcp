package terraformstate

import "testing"

func TestParseFile(t *testing.T) {
	result, err := ParseFile("testdata/terraform.tfstate")
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	if len(result.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(result.Resources))
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(result.Dependencies))
	}

	db := result.Resources[1]
	if db.Type != "aws_db_instance" {
		t.Fatalf("expected aws_db_instance, got %q", db.Type)
	}
	if db.Tags["Team"] != "orders" {
		t.Fatalf("expected Team tag orders, got %v", db.Tags["Team"])
	}
}
