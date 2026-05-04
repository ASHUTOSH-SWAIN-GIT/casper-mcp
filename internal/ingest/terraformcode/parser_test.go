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
	argumentExamples, ok := managedResources[0]["argument_examples"].(map[string]string)
	if !ok {
		t.Fatalf("argument_examples has unexpected type %T", managedResources[0]["argument_examples"])
	}
	if argumentExamples["identifier"] != "var.name" {
		t.Fatalf("expected identifier example var.name, got %v", argumentExamples["identifier"])
	}
	if argumentExamples["engine"] != "postgres" {
		t.Fatalf("expected engine example \"postgres\", got %v", argumentExamples["engine"])
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
	commonArguments, ok := convention.Attributes["common_arguments"].([]string)
	if !ok {
		t.Fatalf("common_arguments has unexpected type %T", convention.Attributes["common_arguments"])
	}
	if len(commonArguments) != 3 {
		t.Fatalf("expected 3 common arguments, got %v", commonArguments)
	}
	argumentExamplesByName, ok := convention.Attributes["argument_examples"].(map[string][]string)
	if !ok {
		t.Fatalf("argument_examples has unexpected type %T", convention.Attributes["argument_examples"])
	}
	if len(argumentExamplesByName["identifier"]) != 1 || argumentExamplesByName["identifier"][0] != "var.name" {
		t.Fatalf("expected identifier convention example var.name, got %v", argumentExamplesByName["identifier"])
	}
	literalArguments, ok := convention.Attributes["literal_arguments"].(map[string][]string)
	if !ok {
		t.Fatalf("literal_arguments has unexpected type %T", convention.Attributes["literal_arguments"])
	}
	if len(literalArguments["engine"]) != 1 || literalArguments["engine"][0] != "postgres" {
		t.Fatalf("expected engine literal argument \"postgres\", got %v", literalArguments["engine"])
	}
}
