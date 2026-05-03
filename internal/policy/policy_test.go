package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func intPtr(n int) *int { return &n }

// --- Check() unit tests ---

func TestCheck_MustEqual(t *testing.T) {
	policies := []Policy{{
		ID:       "rds-deletion-protection",
		Resource: "aws_db_instance",
		Rules:    []Rule{{Arg: "deletion_protection", MustEqual: "true"}},
		Message:  "must have deletion_protection",
	}}

	tests := []struct {
		name        string
		args        map[string]string
		wantViolate bool
	}{
		{"passes when correct", map[string]string{"deletion_protection": "true"}, false},
		{"fails when wrong value", map[string]string{"deletion_protection": "false"}, true},
		{"fails when not set", map[string]string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Check(policies, "aws_db_instance", "aws_db_instance.orders", tt.args)
			if (len(v) > 0) != tt.wantViolate {
				t.Errorf("wantViolate=%v got %d violations", tt.wantViolate, len(v))
			}
		})
	}
}

func TestCheck_MustNotEqual(t *testing.T) {
	policies := []Policy{{
		ID:       "no-public-s3",
		Resource: "aws_s3_bucket",
		Rules:    []Rule{{Arg: "acl", MustNotEqual: "public-read"}},
		Message:  "no public buckets",
	}}

	tests := []struct {
		name        string
		args        map[string]string
		wantViolate bool
	}{
		{"passes when private", map[string]string{"acl": "private"}, false},
		{"passes when not set", map[string]string{}, false},
		{"fails when public-read", map[string]string{"acl": "public-read"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Check(policies, "aws_s3_bucket", "aws_s3_bucket.data", tt.args)
			if (len(v) > 0) != tt.wantViolate {
				t.Errorf("wantViolate=%v got %d violations", tt.wantViolate, len(v))
			}
		})
	}
}

func TestCheck_Required(t *testing.T) {
	policies := []Policy{{
		ID:       "sg-description",
		Resource: "aws_security_group",
		Rules:    []Rule{{Arg: "description", Required: true}},
		Message:  "security groups need description",
	}}

	tests := []struct {
		name        string
		args        map[string]string
		wantViolate bool
	}{
		{"passes when set", map[string]string{"description": "app servers"}, false},
		{"fails when missing", map[string]string{}, true},
		{"fails when empty", map[string]string{"description": ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Check(policies, "aws_security_group", "aws_security_group.app", tt.args)
			if (len(v) > 0) != tt.wantViolate {
				t.Errorf("wantViolate=%v got %d violations", tt.wantViolate, len(v))
			}
		})
	}
}

func TestCheck_MinValue(t *testing.T) {
	policies := []Policy{{
		ID:       "rds-backup",
		Resource: "aws_db_instance",
		Rules:    []Rule{{Arg: "backup_retention_period", MinValue: intPtr(7)}},
		Message:  "need 7 days retention",
	}}

	tests := []struct {
		name        string
		args        map[string]string
		wantViolate bool
	}{
		{"passes when exactly min", map[string]string{"backup_retention_period": "7"}, false},
		{"passes when above min", map[string]string{"backup_retention_period": "30"}, false},
		{"fails when below min", map[string]string{"backup_retention_period": "3"}, true},
		{"fails when zero", map[string]string{"backup_retention_period": "0"}, true},
		{"fails when not set", map[string]string{}, true},
		{"fails when not a number", map[string]string{"backup_retention_period": "weekly"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Check(policies, "aws_db_instance", "aws_db_instance.orders", tt.args)
			if (len(v) > 0) != tt.wantViolate {
				t.Errorf("wantViolate=%v got %d violations", tt.wantViolate, len(v))
			}
		})
	}
}

func TestCheck_WildcardResource(t *testing.T) {
	policies := []Policy{{
		ID:       "require-owner-tag",
		Resource: "*",
		Rules:    []Rule{{Arg: "owner", Required: true}},
		Message:  "all resources need an owner",
	}}

	// Should apply to any resource type
	for _, rtype := range []string{"aws_db_instance", "aws_s3_bucket", "aws_lambda_function"} {
		v := Check(policies, rtype, rtype+".test", map[string]string{})
		if len(v) == 0 {
			t.Errorf("expected violation for resource type %s", rtype)
		}
	}
}

func TestCheck_ResourceTypeMismatch(t *testing.T) {
	policies := []Policy{{
		ID:       "rds-only",
		Resource: "aws_db_instance",
		Rules:    []Rule{{Arg: "deletion_protection", MustEqual: "true"}},
		Message:  "rds policy",
	}}

	// Should NOT apply to a different resource type
	v := Check(policies, "aws_s3_bucket", "aws_s3_bucket.data", map[string]string{})
	if len(v) > 0 {
		t.Errorf("policy should not apply to aws_s3_bucket, got %d violations", len(v))
	}
}

func TestCheck_MultipleRules_AllMustPass(t *testing.T) {
	policies := []Policy{{
		ID:       "rds-full",
		Resource: "aws_db_instance",
		Rules: []Rule{
			{Arg: "deletion_protection", MustEqual: "true"},
			{Arg: "backup_retention_period", MinValue: intPtr(7)},
			{Arg: "storage_encrypted", MustEqual: "true"},
		},
		Message: "rds requirements",
	}}

	// All three fail
	v := Check(policies, "aws_db_instance", "aws_db_instance.orders", map[string]string{})
	if len(v) != 3 {
		t.Errorf("expected 3 violations, got %d", len(v))
	}

	// Two fail
	v = Check(policies, "aws_db_instance", "aws_db_instance.orders", map[string]string{
		"deletion_protection": "true",
	})
	if len(v) != 2 {
		t.Errorf("expected 2 violations, got %d", len(v))
	}

	// All pass
	v = Check(policies, "aws_db_instance", "aws_db_instance.orders", map[string]string{
		"deletion_protection":    "true",
		"backup_retention_period": "7",
		"storage_encrypted":      "true",
	})
	if len(v) != 0 {
		t.Errorf("expected 0 violations, got %d", len(v))
	}
}

func TestCheck_ViolationFields(t *testing.T) {
	policies := []Policy{{
		ID:       "test-policy",
		Resource: "aws_db_instance",
		Rules:    []Rule{{Arg: "deletion_protection", MustEqual: "true"}},
		Message:  "must protect",
	}}

	v := Check(policies, "aws_db_instance", "aws_db_instance.orders", map[string]string{})
	if len(v) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(v))
	}
	if v[0].PolicyID != "test-policy" {
		t.Errorf("PolicyID: got %q", v[0].PolicyID)
	}
	if v[0].Resource != "aws_db_instance.orders" {
		t.Errorf("Resource: got %q", v[0].Resource)
	}
	if v[0].Type != "aws_db_instance" {
		t.Errorf("Type: got %q", v[0].Type)
	}
	if v[0].Message != "must protect" {
		t.Errorf("Message: got %q", v[0].Message)
	}
	if v[0].Details == "" {
		t.Errorf("Details should not be empty")
	}
}

// --- Load() unit tests ---

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	policies, err := Load(dir)
	if err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
	if len(policies) != 0 {
		t.Errorf("expected empty slice, got %d policies", len(policies))
	}
}

func TestLoad_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.MkdirAll(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}

	yaml := `
policies:
  - id: test-policy
    resource: aws_db_instance
    rules:
      - arg: deletion_protection
        must_equal: "true"
    message: "test message"
`
	if err := os.WriteFile(filepath.Join(casperDir, "policies.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	policies, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(policies))
	}
	if policies[0].ID != "test-policy" {
		t.Errorf("ID: got %q", policies[0].ID)
	}
	if policies[0].Resource != "aws_db_instance" {
		t.Errorf("Resource: got %q", policies[0].Resource)
	}
	if len(policies[0].Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(policies[0].Rules))
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.MkdirAll(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casperDir, "policies.yaml"), []byte("{{invalid yaml{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}
