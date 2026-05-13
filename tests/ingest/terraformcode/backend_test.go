package terraformcode_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
)

func fixturePath(parts ...string) string {
	base := []string{"..", "..", "testdata", "backend"}
	return filepath.Join(append(base, parts...)...)
}

func TestFindS3Backends_Single(t *testing.T) {
	backends, err := terraformcode.FindS3Backends(fixturePath("single"))
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(backends))
	}
	b := backends[0]
	if b.Bucket != "tofu-backend-429032495558" {
		t.Errorf("bucket: got %q", b.Bucket)
	}
	if b.Key != "base-new.tfstate" {
		t.Errorf("key: got %q", b.Key)
	}
	if b.Region != "ap-south-1" {
		t.Errorf("region: got %q", b.Region)
	}
}

func TestFindS3Backends_MultiModule(t *testing.T) {
	backends, err := terraformcode.FindS3Backends(fixturePath("multi"))
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 3 {
		t.Fatalf("expected 3 backends, got %d: %+v", len(backends), backends)
	}

	keys := map[string]terraformcode.S3Backend{}
	for _, b := range backends {
		keys[b.Key] = b
	}
	for _, want := range []string{"service-a.tfstate", "service-b.tfstate", "service-c.tfstate"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("expected backend with key %q in result, got keys %v", want, keys)
		}
	}
}

func TestFindS3Backends_RegionOptional(t *testing.T) {
	backends, err := terraformcode.FindS3Backends(fixturePath("multi", "c"))
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(backends))
	}
	if backends[0].Region != "" {
		t.Errorf("expected empty region for backend without region declaration, got %q", backends[0].Region)
	}
	if backends[0].Key != "service-c.tfstate" {
		t.Errorf("key: got %q", backends[0].Key)
	}
}

func TestFindS3Backends_NoBackend(t *testing.T) {
	backends, err := terraformcode.FindS3Backends(fixturePath("none"))
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 0 {
		t.Errorf("expected 0 backends for tree without backend blocks, got %d", len(backends))
	}
}

func TestFindS3Backends_IgnoresNonS3(t *testing.T) {
	backends, err := terraformcode.FindS3Backends(fixturePath("non_s3"))
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 0 {
		t.Errorf("expected 0 backends for non-s3 backend (local), got %d", len(backends))
	}
}

func TestFindS3Backends_DedupesByBucketAndKey(t *testing.T) {
	// Same bucket+key declared in two files under the same tree should produce one entry.
	dir := t.TempDir()
	content := `terraform {
  backend "s3" {
    bucket = "shared"
    key    = "stack.tfstate"
    region = "us-east-1"
  }
}`
	for _, name := range []string{"a.tf", "b.tf"} {
		if err := writeFile(filepath.Join(dir, name), content); err != nil {
			t.Fatal(err)
		}
	}
	backends, err := terraformcode.FindS3Backends(dir)
	if err != nil {
		t.Fatalf("FindS3Backends: %v", err)
	}
	if len(backends) != 1 {
		t.Errorf("expected dedup to produce 1 entry, got %d", len(backends))
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
