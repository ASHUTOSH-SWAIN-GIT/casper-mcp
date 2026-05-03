package terraformcode

import "testing"

func TestParseDir(t *testing.T) {
	resources, err := ParseDir("testdata/modules/postgres")
	if err != nil {
		t.Fatalf("ParseDir() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
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

	convention := resources[1]
	if convention.Type != "terraform_convention" {
		t.Fatalf("expected terraform_convention, got %q", convention.Type)
	}
	if convention.Attributes["resource_type"] != "aws_db_instance" {
		t.Fatalf("expected resource_type aws_db_instance, got %v", convention.Attributes["resource_type"])
	}
	namingSignals, ok := convention.Attributes["naming_signals"].([]string)
	if !ok {
		t.Fatalf("naming_signals has unexpected type %T", convention.Attributes["naming_signals"])
	}
	if len(namingSignals) == 0 || namingSignals[0] != "name" {
		t.Fatalf("expected naming signal name, got %v", namingSignals)
	}
}
