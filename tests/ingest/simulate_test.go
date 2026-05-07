package ingest_test

import (
	"strings"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/workflow"
)

func testSnapshot() graph.GraphSnapshot {
	return graph.GraphSnapshot{
		Resources: []graph.Resource{
			{
				ID:         "r1",
				Source:     "/tmp",
				Type:       "aws_db_instance",
				Identifier: "aws_db_instance.orders",
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
				Source:     "/tmp",
				Type:       "aws_vpc",
				Identifier: "aws_vpc.main",
				Attributes: map[string]any{
					"arguments": map[string]string{"cidr_block": "10.0.0.0/16"},
				},
			},
			{
				ID:         "r3",
				Source:     "/tmp",
				Type:       "aws_security_group",
				Identifier: "aws_security_group.app",
				Attributes: map[string]any{
					"arguments": map[string]string{
						"vpc_id":      "aws_vpc.main.id",
						"description": "app servers",
					},
				},
			},
		},
		Dependencies: []graph.Dependency{
			{FromResource: "r3", ToResource: "r2", Kind: "reference"},
		},
	}
}

func noQuerier() graph.Querier             { return graph.NewMemStore(graph.GraphSnapshot{}) }
func noPolicies() []policy.Policy          { return nil }
func noWorkflowRules() []workflow.WorkflowRule { return nil }

func TestSimulateImpact_Create(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/orders"
  retention_in_days = 30
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("expected 1 created resource, got %d", len(result.Created))
	}
	if result.Created[0].Identifier != "aws_cloudwatch_log_group.ecs" {
		t.Errorf("unexpected identifier: %s", result.Created[0].Identifier)
	}
	if len(result.Modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(result.Modified))
	}
	if len(result.BlastRadius) != 0 {
		t.Errorf("expected 0 blast radius, got %d", len(result.BlastRadius))
	}
}

func TestSimulateImpact_CreateReversibilityOperation(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_cloudwatch_log_group" "ecs" {
  name = "/ecs/orders"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReversibilityContext == nil {
		t.Fatal("expected reversibility context")
	}
	var found bool
	for _, rc := range result.ReversibilityContext.Resources {
		if rc.Identifier == "aws_cloudwatch_log_group.ecs" {
			found = true
			if rc.Operation != "create" {
				t.Errorf("expected operation=create, got %s", rc.Operation)
			}
		}
	}
	if !found {
		t.Error("resource not found in reversibility context")
	}
}

func TestSimulateImpact_Modify(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_db_instance" "orders" {
  engine         = "postgres"
  instance_class = "db.t3.large"
  deletion_protection = "true"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Modified) != 1 {
		t.Fatalf("expected 1 modified resource, got %d", len(result.Modified))
	}
	diff := result.Modified[0]
	if diff.Identifier != "aws_db_instance.orders" {
		t.Errorf("unexpected identifier: %s", diff.Identifier)
	}
	if _, ok := diff.Changed["instance_class"]; !ok {
		t.Error("expected instance_class in Changed")
	}
	before := diff.Changed["instance_class"].Before
	after := diff.Changed["instance_class"].After
	if before != "db.t3.medium" && before != `"db.t3.medium"` {
		t.Errorf("unexpected Before value: %s", before)
	}
	if after != "db.t3.large" && after != `"db.t3.large"` {
		t.Errorf("unexpected After value: %s", after)
	}
}

func TestSimulateImpact_ModifyReversibilityContext(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_db_instance" "orders" {
  engine         = "postgres"
  instance_class = "db.t3.large"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReversibilityContext == nil {
		t.Fatal("expected reversibility context")
	}
	var rc *graph.ResourceContext
	for i, r := range result.ReversibilityContext.Resources {
		if r.Identifier == "aws_db_instance.orders" {
			rc = &result.ReversibilityContext.Resources[i]
		}
	}
	if rc == nil {
		t.Fatal("aws_db_instance.orders not in reversibility context")
	}
	if rc.Operation != "modify" {
		t.Errorf("expected operation=modify, got %s", rc.Operation)
	}
	if rc.CurrentArgs["instance_class"] != "db.t3.medium" {
		t.Errorf("expected current instance_class=db.t3.medium, got %s", rc.CurrentArgs["instance_class"])
	}
}

func TestSimulateImpact_BlastRadius_Downstream(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_vpc" "main" {
  cidr_block = "10.1.0.0/16"
}`)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range result.BlastRadius {
		if b.Identifier == "aws_security_group.app" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_security_group.app in blast radius (it references aws_vpc.main)")
	}
}

func TestSimulateImpact_BlastRadius_NewResourceUpstream(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_security_group" "new_sg" {
  vpc_id      = aws_vpc.main.id
  description = "new sg"
}`)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, b := range result.BlastRadius {
		if b.Identifier == "aws_vpc.main" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_vpc.main in blast radius as upstream reference")
	}
}

func TestSimulateImpact_BrokenReference(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_security_group" "orphan" {
  vpc_id      = aws_vpc.nonexistent.id
  description = "orphaned"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected at least one warning for broken reference to aws_vpc.nonexistent")
	}
}

func TestSimulateImpact_NoWarnForValidRef(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_security_group" "valid" {
  vpc_id      = aws_vpc.main.id
  description = "valid reference"
}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "aws_vpc.main") {
			t.Errorf("unexpected warning for valid reference: %s", w)
		}
	}
}

func TestSimulateImpact_LifecycleFlags_PreventDestroy(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_db_instance" "orders" {
  engine         = "postgres"
  instance_class = "db.t3.medium"
  lifecycle {
    prevent_destroy = true
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReversibilityContext == nil {
		t.Fatal("expected reversibility context")
	}
	for _, rc := range result.ReversibilityContext.Resources {
		if rc.Identifier == "aws_db_instance.orders" {
			if !rc.LifecycleFlags.PreventDestroy {
				t.Error("expected PreventDestroy=true")
			}
			return
		}
	}
	t.Error("aws_db_instance.orders not found in reversibility context")
}

func TestSimulateImpact_LifecycleFlags_CreateBeforeDestroy(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), noPolicies(), noWorkflowRules(), `
resource "aws_db_instance" "orders" {
  engine         = "postgres"
  instance_class = "db.t3.medium"
  lifecycle {
    create_before_destroy = true
  }
}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, rc := range result.ReversibilityContext.Resources {
		if rc.Identifier == "aws_db_instance.orders" {
			if !rc.LifecycleFlags.CreateBeforeDestroy {
				t.Error("expected CreateBeforeDestroy=true")
			}
			return
		}
	}
	t.Error("aws_db_instance.orders not found in reversibility context")
}

func TestSimulateImpact_PolicyViolation(t *testing.T) {
	snapshot := testSnapshot()
	policies := []policy.Policy{{
		ID:       "rds-deletion-protection",
		Resource: "aws_db_instance",
		Rules:    []policy.Rule{{Arg: "deletion_protection", MustEqual: "true"}},
		Message:  "must have deletion_protection",
	}}

	result, err := ingest.SimulateImpact(snapshot, noQuerier(), policies, noWorkflowRules(), `
resource "aws_db_instance" "new_db" {
  engine         = "postgres"
  instance_class = "db.t3.small"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PolicyViolations) == 0 {
		t.Fatal("expected policy violations")
	}
	v := result.PolicyViolations[0]
	if v.PolicyID != "rds-deletion-protection" {
		t.Errorf("unexpected PolicyID: %s", v.PolicyID)
	}
	if v.Resource != "aws_db_instance.new_db" {
		t.Errorf("unexpected Resource: %s", v.Resource)
	}
}

func TestSimulateImpact_PolicyPass(t *testing.T) {
	snapshot := testSnapshot()
	policies := []policy.Policy{{
		ID:       "rds-deletion-protection",
		Resource: "aws_db_instance",
		Rules:    []policy.Rule{{Arg: "deletion_protection", MustEqual: "true"}},
		Message:  "must have deletion_protection",
	}}

	result, err := ingest.SimulateImpact(snapshot, noQuerier(), policies, noWorkflowRules(), `
resource "aws_db_instance" "new_db" {
  engine              = "postgres"
  instance_class      = "db.t3.small"
  deletion_protection = true
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PolicyViolations) != 0 {
		t.Errorf("expected 0 violations, got %d: %v", len(result.PolicyViolations), result.PolicyViolations)
	}
}

func TestSimulateImpact_NoPolicies(t *testing.T) {
	snapshot := testSnapshot()
	result, err := ingest.SimulateImpact(snapshot, noQuerier(), nil, noWorkflowRules(), `
resource "aws_db_instance" "new_db" {
  engine = "postgres"
}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.PolicyViolations) != 0 {
		t.Errorf("expected 0 violations with no policies, got %d", len(result.PolicyViolations))
	}
}

func TestSimulateImpact_NoResourceBlocks(t *testing.T) {
	_, err := ingest.SimulateImpact(testSnapshot(), noQuerier(), noPolicies(), noWorkflowRules(), `
variable "region" {
  default = "us-east-1"
}`)
	if err == nil {
		t.Error("expected error for HCL with no resource blocks")
	}
}

func TestSimulateImpact_InvalidHCL(t *testing.T) {
	_, err := ingest.SimulateImpact(testSnapshot(), noQuerier(), noPolicies(), noWorkflowRules(), `this is not valid hcl {{{`)
	if err == nil {
		t.Error("expected error for invalid HCL")
	}
}

func TestExtractResourceRefs(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		wantRefs []string
	}{
		{"simple reference", "aws_vpc.main.id", []string{"aws_vpc.main"}},
		{"skips var prefix", "var.region", []string{}},
		{"skips local prefix", "local.subnet_ids", []string{}},
		{"skips data prefix", "data.aws_ami.ubuntu.id", []string{}},
		{"multiple refs in string", "aws_vpc.main.id aws_subnet.private.id", []string{"aws_vpc.main", "aws_subnet.private"}},
		{"no ref in plain string", "us-east-1", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ingest.ExtractResourceRefs(tt.expr)
			if len(got) != len(tt.wantRefs) {
				t.Errorf("got %v, want %v", got, tt.wantRefs)
				return
			}
			gotSet := map[string]bool{}
			for _, r := range got {
				gotSet[r] = true
			}
			for _, want := range tt.wantRefs {
				if !gotSet[want] {
					t.Errorf("missing expected ref %q in %v", want, got)
				}
			}
		})
	}
}

func TestDiffArguments(t *testing.T) {
	cur := graph.Resource{
		Attributes: map[string]any{
			"arguments": map[string]string{
				"engine":         "postgres",
				"instance_class": "db.t3.medium",
				"old_arg":        "old_value",
			},
		},
	}
	prop := graph.Resource{
		Attributes: map[string]any{
			"arguments": map[string]string{
				"engine":         "postgres",
				"instance_class": "db.t3.large",
				"new_arg":        "new_value",
			},
		},
	}

	diff := ingest.DiffArguments(cur, prop)
	if diff == nil {
		t.Fatal("expected non-nil diff")
	}
	if _, ok := diff.Changed["instance_class"]; !ok {
		t.Error("expected instance_class in Changed")
	}
	if diff.Changed["instance_class"].Before != "db.t3.medium" {
		t.Errorf("Before: got %q", diff.Changed["instance_class"].Before)
	}
	if diff.Changed["instance_class"].After != "db.t3.large" {
		t.Errorf("After: got %q", diff.Changed["instance_class"].After)
	}
	if _, ok := diff.Added["new_arg"]; !ok {
		t.Error("expected new_arg in Added")
	}
	var foundRemoved bool
	for _, k := range diff.Removed {
		if k == "old_arg" {
			foundRemoved = true
		}
	}
	if !foundRemoved {
		t.Error("expected old_arg in Removed")
	}
	if _, ok := diff.Changed["engine"]; ok {
		t.Error("engine should not be in Changed (unchanged)")
	}
}

func TestDiffArguments_NoChange(t *testing.T) {
	r := graph.Resource{
		Attributes: map[string]any{
			"arguments": map[string]string{"engine": "postgres"},
		},
	}
	diff := ingest.DiffArguments(r, r)
	if diff != nil {
		t.Error("expected nil diff for identical resources")
	}
}
