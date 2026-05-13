package rego_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	regopkg "github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy/rego"
)

func loadFixture(t *testing.T, names ...string) []regopkg.RegoFile {
	t.Helper()
	out := make([]regopkg.RegoFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join("..", "..", "testdata", "rego", name)
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, regopkg.RegoFile{Path: abs, Bytes: data})
	}
	return out
}

func TestEngine_DenyFires(t *testing.T) {
	files := loadFixture(t, "simple_s3.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	args := map[string]string{"acl": "public-read"}
	violations := engine.Check("aws_s3_bucket", "aws_s3_bucket.public", args, nil)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if !strings.Contains(violations[0].Message, "public-read") {
		t.Errorf("message: got %q", violations[0].Message)
	}
	if violations[0].Resource != "aws_s3_bucket.public" {
		t.Errorf("resource: got %q", violations[0].Resource)
	}
	if violations[0].Type != "aws_s3_bucket" {
		t.Errorf("type: got %q", violations[0].Type)
	}
}

func TestEngine_DenyDoesNotFireOnUnrelated(t *testing.T) {
	files := loadFixture(t, "simple_s3.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Private bucket — should not fire.
	v1 := engine.Check("aws_s3_bucket", "aws_s3_bucket.private", map[string]string{"acl": "private"}, nil)
	if len(v1) != 0 {
		t.Errorf("expected 0 violations for private bucket, got %d", len(v1))
	}

	// Completely different resource type — should not fire.
	v2 := engine.Check("aws_db_instance", "aws_db_instance.orders", map[string]string{}, nil)
	if len(v2) != 0 {
		t.Errorf("expected 0 violations for non-s3 resource, got %d", len(v2))
	}
}

func TestEngine_MultipleFiles(t *testing.T) {
	files := loadFixture(t, "simple_s3.rego", "rds_deletion.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Both policies should fire on their respective resources.
	v1 := engine.Check("aws_s3_bucket", "aws_s3_bucket.public", map[string]string{"acl": "public-read"}, nil)
	if len(v1) != 1 {
		t.Errorf("s3 policy: expected 1 violation, got %d", len(v1))
	}

	v2 := engine.Check("aws_db_instance", "aws_db_instance.orders", map[string]string{"deletion_protection": "false"}, nil)
	if len(v2) != 1 {
		t.Errorf("rds policy: expected 1 violation, got %d", len(v2))
	}
}

func TestEngine_MultipleRulesInOneFile(t *testing.T) {
	files := loadFixture(t, "multi_rule.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Both rules fire: missing owner tag + EOL runtime.
	violations := engine.Check(
		"aws_lambda_function",
		"aws_lambda_function.legacy",
		map[string]string{"runtime": "nodejs12.x"},
		nil, // no tags → missing owner
	)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations (missing owner + EOL runtime), got %d: %v", len(violations), violations)
	}
}

func TestEngine_MissingAttributeDoesNotPanic(t *testing.T) {
	files := loadFixture(t, "simple_s3.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// s3 bucket without an acl arg at all — policy queries input.attributes.acl,
	// which is undefined. Rego should treat the rule as not matching → 0 violations,
	// no panic.
	violations := engine.Check("aws_s3_bucket", "aws_s3_bucket.no_acl", map[string]string{}, nil)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for missing attribute, got %d", len(violations))
	}
}

func TestEngine_InvalidRegoFails(t *testing.T) {
	files := []regopkg.RegoFile{{
		Path:  "broken.rego",
		Bytes: []byte("this is not valid rego {{{ ::: }}}"),
	}}
	_, err := regopkg.NewEngine(context.Background(), files)
	if err == nil {
		t.Fatal("expected NewEngine to fail on invalid rego, got nil")
	}
}

func TestEngine_EmptyFileList(t *testing.T) {
	_, err := regopkg.NewEngine(context.Background(), nil)
	if err == nil {
		t.Error("expected NewEngine to fail with no files supplied")
	}
}

func TestEngine_Source(t *testing.T) {
	files := loadFixture(t, "simple_s3.rego")
	engine, err := regopkg.NewEngine(context.Background(), files)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if engine.Source() != "rego" {
		t.Errorf("Source: got %q, want rego", engine.Source())
	}
}
