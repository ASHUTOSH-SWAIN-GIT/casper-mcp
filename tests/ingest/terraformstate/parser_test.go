package terraformstate_test

import (
	"path/filepath"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformstate"
)

func TestParseFile(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "terraformstate", "terraform.tfstate")
	result, err := terraformstate.ParseFile(path)
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

func TestParseBytes_RedactsSensitiveAttributes(t *testing.T) {
	// State with an explicit sensitive_attributes entry pointing at "password"
	// and a heuristic-only key ("api_token") to verify both routes redact.
	stateJSON := []byte(`{
		"resources": [{
			"mode": "managed",
			"type": "aws_db_instance",
			"name": "orders",
			"instances": [{
				"attributes": {
					"id": "orders-prod",
					"engine": "postgres",
					"password": "P@ssw0rd123!",
					"api_token": "tok_secret_xyz",
					"public_dns": "orders-prod.abc.us-east-1.rds.amazonaws.com"
				},
				"sensitive_attributes": [
					{"type": "get_attr", "steps": [{"attribute_name": "password"}]}
				]
			}]
		}]
	}`)

	result, err := terraformstate.ParseBytes(stateJSON, "test://state")
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}

	attrs := result.Resources[0].Attributes
	if attrs["password"] != "<redacted>" {
		t.Errorf("password should be redacted (explicit sensitive list), got %v", attrs["password"])
	}
	if attrs["api_token"] != "<redacted>" {
		t.Errorf("api_token should be redacted (heuristic), got %v", attrs["api_token"])
	}
	if attrs["engine"] != "postgres" {
		t.Errorf("non-sensitive engine should pass through, got %v", attrs["engine"])
	}
	if attrs["public_dns"] == "<redacted>" {
		t.Errorf("public_dns is not sensitive; should not be redacted")
	}
}

func TestParseBytes_SourceIsRecorded(t *testing.T) {
	stateJSON := []byte(`{"resources":[{"mode":"managed","type":"aws_vpc","name":"main","instances":[{"attributes":{"id":"vpc-1"}}]}]}`)
	result, err := terraformstate.ParseBytes(stateJSON, "s3://acme-state/prod.tfstate")
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result.Resources))
	}
	if got := result.Resources[0].Source; got != "s3://acme-state/prod.tfstate" {
		t.Errorf("Source: got %q, want s3 URI", got)
	}
}
