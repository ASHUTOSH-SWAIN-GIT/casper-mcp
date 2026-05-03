package terraformcode

import "testing"

func TestParseDir(t *testing.T) {
	resources, err := ParseDir("testdata/modules/postgres")
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 module resource, got %d", len(resources))
	}

	module := resources[0]
	if module.Type != "terraform_module" {
		t.Fatalf("expected terraform_module, got %q", module.Type)
	}
	if module.ManagedBy != "terraform_code" {
		t.Fatalf("expected terraform_code, got %q", module.ManagedBy)
	}

	managedResources, ok := module.Attributes["managed_resources"].([]map[string]any)
	if !ok {
		t.Fatalf("managed_resources has unexpected type %T", module.Attributes["managed_resources"])
	}
	if len(managedResources) != 1 {
		t.Fatalf("expected 1 managed resource, got %d", len(managedResources))
	}
	if managedResources[0]["type"] != "aws_db_instance" {
		t.Fatalf("expected aws_db_instance, got %v", managedResources[0]["type"])
	}
}
