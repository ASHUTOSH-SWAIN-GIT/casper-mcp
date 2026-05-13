package rego_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	regopkg "github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy/rego"
)

func TestDiscover_FindsFiles(t *testing.T) {
	// The shared testdata/rego/ holds 3 .rego files.
	dir := filepath.Join("..", "..", "testdata", "rego")
	files, err := regopkg.Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 rego files, got %d", len(files))
	}
	for _, f := range files {
		if len(f.Bytes) == 0 {
			t.Errorf("%s: empty bytes", f.Path)
		}
		if !filepath.IsAbs(f.Path) {
			t.Errorf("%s: path should be absolute", f.Path)
		}
	}
}

func TestDiscover_RecursiveWalk(t *testing.T) {
	root := t.TempDir()
	// Nest a few .rego files at varying depths.
	files := map[string]string{
		"top.rego":               "package policy\ndeny[m] { m := \"top\" }",
		"a/inner.rego":           "package policy\ndeny[m] { m := \"a/inner\" }",
		"a/b/deeper.rego":        "package policy\ndeny[m] { m := \"a/b/deeper\" }",
		"a/b/c/deepest.rego":     "package policy\ndeny[m] { m := \"a/b/c/deepest\" }",
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := regopkg.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != len(files) {
		t.Fatalf("expected %d files, got %d", len(files), len(got))
	}
}

func TestDiscover_SkipsNoiseDirs(t *testing.T) {
	root := t.TempDir()
	noise := []string{".git", ".terraform", "node_modules", ".terragrunt-cache", "vendor", "testdata"}

	if err := os.WriteFile(filepath.Join(root, "real.rego"), []byte("package policy"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range noise {
		full := filepath.Join(root, dir, "should_skip.rego")
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package policy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := regopkg.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (real.rego only), got %d: %+v", len(files), files)
	}
	if filepath.Base(files[0].Path) != "real.rego" {
		t.Errorf("expected real.rego, got %s", files[0].Path)
	}
}

func TestDiscover_IgnoresNonRego(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main.tf", "policies.yaml", "README.md", "rego.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := regopkg.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 .rego files, got %d", len(files))
	}
}

func TestDiscover_EmptyDir(t *testing.T) {
	files, err := regopkg.Discover(t.TempDir())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files for empty dir, got %d", len(files))
	}
}

func TestDiscover_CasperPolicyDirIsExclusive(t *testing.T) {
	// When .casper/policies/ exists with rego files, repo-wide rego files
	// elsewhere should NOT be picked up. This is the explicit "Casper
	// policies live here" path that wins over the recursive walk.
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, ".casper", "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".casper", "policies", "in_casper.rego"),
		[]byte("package policy\ndeny[m] { m := \"casper\" }"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Put a "competing" rego file at the repo root that should be ignored
	// when the convention dir is active.
	if err := os.MkdirAll(filepath.Join(root, "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "policy", "outside.rego"),
		[]byte("package policy\ndeny[m] { m := \"outside\" }"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := regopkg.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (only .casper/policies/), got %d: %+v", len(files), files)
	}
	if !strings.Contains(files[0].Path, ".casper/policies/in_casper.rego") {
		t.Errorf("expected in_casper.rego, got %s", files[0].Path)
	}

	if !regopkg.IsCasperPolicyDir(root) {
		t.Error("IsCasperPolicyDir: expected true when .casper/policies/ has rego files")
	}
}

func TestDiscover_EmptyCasperPolicyDirFallsBack(t *testing.T) {
	// An empty .casper/policies/ directory should be treated as a placeholder.
	// Casper falls back to recursive repo walk rather than serving zero
	// policies.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".casper", "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "policy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "policy", "x.rego"),
		[]byte("package policy"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := regopkg.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected fallback to find 1 file, got %d", len(files))
	}

	if regopkg.IsCasperPolicyDir(root) {
		t.Error("IsCasperPolicyDir: expected false for empty .casper/policies/")
	}
}

func TestIsCasperPolicyDir_Missing(t *testing.T) {
	if regopkg.IsCasperPolicyDir(t.TempDir()) {
		t.Error("expected false for repo without .casper/policies/")
	}
}
