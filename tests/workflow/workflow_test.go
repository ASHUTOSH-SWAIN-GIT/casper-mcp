package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/workflow"
)

func TestDetectEnv_FromTag(t *testing.T) {
	res := workflow.ResourceInput{Tags: map[string]string{"env": "production"}}
	if got := workflow.DetectEnv(res); got != "prod" {
		t.Errorf("expected prod, got %s", got)
	}
}

func TestDetectEnv_FromModulePath(t *testing.T) {
	res := workflow.ResourceInput{ModulePath: "/infra/modules/prod/rds"}
	if got := workflow.DetectEnv(res); got != "prod" {
		t.Errorf("expected prod, got %s", got)
	}
}

func TestDetectEnv_StagingBeatingDev(t *testing.T) {
	res := workflow.ResourceInput{
		Tags:       map[string]string{"env": "dev"},
		ModulePath: "/infra/staging/rds",
	}
	if got := workflow.DetectEnv(res); got != "staging" {
		t.Errorf("expected staging, got %s", got)
	}
}

func TestDetectEnv_FailClosed(t *testing.T) {
	res := workflow.ResourceInput{Identifier: "aws_db_instance.orders"}
	if got := workflow.DetectEnv(res); got != "prod" {
		t.Errorf("expected prod (fail-closed), got %s", got)
	}
}

func TestResourceFamily(t *testing.T) {
	cases := map[string]string{
		"aws_db_instance":    "database",
		"aws_rds_cluster":    "database",
		"aws_iam_role":       "iam",
		"aws_security_group": "network_security",
		"aws_eks_cluster":    "compute",
		"aws_s3_bucket":      "storage",
		"aws_unknown_type":   "",
	}
	for rtype, want := range cases {
		if got := workflow.ResourceFamily(rtype); got != want {
			t.Errorf("ResourceFamily(%q) = %q, want %q", rtype, got, want)
		}
	}
}

func TestEvaluate_NoRules(t *testing.T) {
	result := workflow.Evaluate(nil, []workflow.ResourceInput{{Identifier: "aws_db_instance.orders", Type: "aws_db_instance", Operation: "destroy"}})
	if result != nil {
		t.Errorf("expected nil for no rules, got %+v", result)
	}
}

func TestEvaluate_NoMatch(t *testing.T) {
	rules := []workflow.WorkflowRule{
		{
			ID:       "prod-db-destroy",
			When:     workflow.RuleCondition{Env: "prod", Operation: workflow.StringList{"destroy"}, ResourceTypeFamily: "database"},
			Decision: "block",
		},
	}
	res := workflow.ResourceInput{
		Identifier: "aws_db_instance.orders",
		Type:       "aws_db_instance",
		Operation:  "destroy",
		Tags:       map[string]string{"env": "staging"},
	}
	result := workflow.Evaluate(rules, []workflow.ResourceInput{res})
	if result.Decision != "allow" {
		t.Errorf("expected allow for non-matching, got %s", result.Decision)
	}
}

func TestEvaluate_Block(t *testing.T) {
	rules := []workflow.WorkflowRule{
		{
			ID:       "prod-db-destroy",
			When:     workflow.RuleCondition{Env: "prod", Operation: workflow.StringList{"destroy"}, ResourceTypeFamily: "database"},
			Decision: "block",
			Reason:   "database destroy requires manual ticket",
		},
	}
	res := workflow.ResourceInput{
		Identifier: "aws_db_instance.orders",
		Type:       "aws_db_instance",
		Operation:  "destroy",
		Tags:       map[string]string{"env": "prod"},
	}
	result := workflow.Evaluate(rules, []workflow.ResourceInput{res})
	if result.Decision != "block" {
		t.Errorf("expected block, got %s", result.Decision)
	}
	if !result.Blocked {
		t.Error("expected Blocked=true")
	}
	if len(result.MatchedRules) != 1 || result.MatchedRules[0].ID != "prod-db-destroy" {
		t.Errorf("unexpected matched rules: %+v", result.MatchedRules)
	}
}

func TestEvaluate_RequireApproval(t *testing.T) {
	rules := []workflow.WorkflowRule{
		{
			ID:       "prod-changes",
			When:     workflow.RuleCondition{Env: "prod", Operation: workflow.StringList{"create", "modify", "destroy"}},
			Decision: "require_approval",
		},
	}
	res := workflow.ResourceInput{
		Identifier: "aws_s3_bucket.assets",
		Type:       "aws_s3_bucket",
		Operation:  "create",
		Tags:       map[string]string{"env": "prod"},
	}
	result := workflow.Evaluate(rules, []workflow.ResourceInput{res})
	if result.Decision != "require_approval" {
		t.Errorf("expected require_approval, got %s", result.Decision)
	}
	if len(result.RequiredSteps) == 0 {
		t.Error("expected default steps for require_approval")
	}
}

func TestEvaluate_StricterDecisionWins(t *testing.T) {
	rules := []workflow.WorkflowRule{
		{ID: "r2", When: workflow.RuleCondition{ResourceTypeFamily: "database", Operation: workflow.StringList{"destroy"}}, Decision: "block"},
		{ID: "r1", When: workflow.RuleCondition{Env: "prod"}, Decision: "require_approval"},
	}
	resources := []workflow.ResourceInput{
		{Identifier: "aws_s3_bucket.assets", Type: "aws_s3_bucket", Operation: "create", Tags: map[string]string{"env": "prod"}},
		{Identifier: "aws_db_instance.orders", Type: "aws_db_instance", Operation: "destroy"},
	}
	result := workflow.Evaluate(rules, resources)
	if result.Decision != "block" {
		t.Errorf("expected block (strictest), got %s", result.Decision)
	}
}

func TestEvaluate_CustomRequiredSteps(t *testing.T) {
	rules := []workflow.WorkflowRule{
		{
			ID:            "security-review",
			When:          workflow.RuleCondition{ResourceTypeFamily: "iam"},
			Decision:      "require_security_review",
			RequiredSteps: []string{"iam_review_ticket", "security_lead_approval"},
		},
	}
	res := workflow.ResourceInput{Type: "aws_iam_role", Operation: "create"}
	result := workflow.Evaluate(rules, []workflow.ResourceInput{res})
	if result.Decision != "require_security_review" {
		t.Errorf("expected require_security_review, got %s", result.Decision)
	}
	if len(result.RequiredSteps) != 2 || result.RequiredSteps[0] != "iam_review_ticket" {
		t.Errorf("unexpected steps: %v", result.RequiredSteps)
	}
}

func TestLoad_AbsentFile(t *testing.T) {
	rules, err := workflow.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Errorf("expected empty rules for absent file, got %d", len(rules))
	}
}

func TestLoad_YAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".casper"), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
workflow_rules:
  - id: prod-db-destroy
    when:
      env: prod
      operation: destroy
      resource_type_family: database
    decision: block
  - id: prod-changes
    when:
      env: prod
      operation: [create, modify, destroy]
    decision: require_approval
`
	if err := os.WriteFile(filepath.Join(dir, ".casper", "policies.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	rules, err := workflow.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].ID != "prod-db-destroy" {
		t.Errorf("unexpected rule id: %s", rules[0].ID)
	}
	if rules[1].When.Operation[0] != "create" {
		t.Errorf("expected create as first operation, got %s", rules[1].When.Operation[0])
	}
}
