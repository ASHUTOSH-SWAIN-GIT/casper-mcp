package awslive_test

import (
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
)

func TestDetectDrift_NoChanges(t *testing.T) {
	tf := map[string]any{"instance_class": "db.r5.large", "engine": "postgres"}
	aws := map[string]string{"instance_class": "db.r5.large", "engine": "postgres"}
	if d := awslive.DetectDrift(tf, aws); len(d) != 0 {
		t.Fatalf("expected no drift, got %+v", d)
	}
}

func TestDetectDrift_Changed(t *testing.T) {
	tf := map[string]any{"instance_class": "db.r5.large"}
	aws := map[string]string{"instance_class": "db.r5.xlarge"}
	d := awslive.DetectDrift(tf, aws)
	if len(d) != 1 {
		t.Fatalf("expected 1 drift field, got %d", len(d))
	}
	if d[0].Field != "instance_class" || d[0].TerraformValue != "db.r5.large" || d[0].AWSValue != "db.r5.xlarge" {
		t.Errorf("unexpected drift entry: %+v", d[0])
	}
}

func TestDetectDrift_SkipsAWSOnlyFields(t *testing.T) {
	tf := map[string]any{"instance_class": "db.r5.large"}
	aws := map[string]string{"instance_class": "db.r5.large", "arn": "arn:aws:rds:..."}
	if d := awslive.DetectDrift(tf, aws); len(d) != 0 {
		t.Fatalf("expected no drift, got %+v", d)
	}
}

func TestDetectDrift_SkipsEmptyTFValues(t *testing.T) {
	tf := map[string]any{"instance_class": "db.r5.large", "db_name": ""}
	aws := map[string]string{"instance_class": "db.r5.large", "db_name": "orders"}
	if d := awslive.DetectDrift(tf, aws); len(d) != 0 {
		t.Fatalf("expected no drift for empty tf field, got %+v", d)
	}
}

func TestDetectDrift_MultipleDrifts(t *testing.T) {
	tf := map[string]any{
		"instance_class": "db.r5.large",
		"multi_az":       "false",
		"engine_version": "14.3",
	}
	aws := map[string]string{
		"instance_class": "db.r5.xlarge",
		"multi_az":       "true",
		"engine_version": "14.3",
	}
	d := awslive.DetectDrift(tf, aws)
	if len(d) != 2 {
		t.Fatalf("expected 2 drift fields, got %d: %+v", len(d), d)
	}
}
