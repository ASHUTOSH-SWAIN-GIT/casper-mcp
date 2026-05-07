package awslive_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/awslive"
)

func TestLoadConfig_MissingFile(t *testing.T) {
	_, ok, err := awslive.LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("expected no error for missing config, got %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when file is absent")
	}
}

func TestLoadConfig_NoCloudSection(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.Mkdir(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
database:
  url: postgres://localhost/casper
states:
  - type: local
    paths: [./terraform.tfstate]
`
	if err := os.WriteFile(filepath.Join(casperDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, err := awslive.LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false when cloud.aws is absent")
	}
}

func TestLoadConfig_WithCloudSection(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.Mkdir(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
cloud:
  aws:
    role_arn: arn:aws:iam::123456789012:role/casper-readonly
    regions: [ap-south-1, us-east-1]
`
	if err := os.WriteFile(filepath.Join(casperDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := awslive.LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if cfg.RoleARN != "arn:aws:iam::123456789012:role/casper-readonly" {
		t.Errorf("unexpected role_arn: %q", cfg.RoleARN)
	}
	if len(cfg.Regions) != 2 || cfg.Regions[0] != "ap-south-1" {
		t.Errorf("unexpected regions: %v", cfg.Regions)
	}
}

func TestLoadConfig_DefaultsToUSEast1WhenNoRegions(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.Mkdir(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `
cloud:
  aws:
    role_arn: arn:aws:iam::123456789012:role/casper-readonly
`
	if err := os.WriteFile(filepath.Join(casperDir, "config.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := awslive.LoadConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(cfg.Regions) != 1 || cfg.Regions[0] != "us-east-1" {
		t.Errorf("expected default region us-east-1, got %v", cfg.Regions)
	}
}

func TestLoadConfig_ParseError(t *testing.T) {
	dir := t.TempDir()
	casperDir := filepath.Join(dir, ".casper")
	if err := os.Mkdir(casperDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(casperDir, "config.yaml"), []byte(":\tinvalid:\tyaml\t{{{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := awslive.LoadConfig(dir)
	if err == nil {
		t.Fatal("expected parse error for invalid YAML")
	}
}
