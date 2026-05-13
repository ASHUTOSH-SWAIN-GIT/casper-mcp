package terraformcode

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// S3Backend describes a `terraform { backend "s3" { ... } }` block found in
// the scanned tree. One per declaration — deduplicated by bucket+key across
// the full discovery pass so the same backend referenced from multiple .tf
// files only produces one entry.
type S3Backend struct {
	Bucket string
	Key    string
	Region string // may be empty if the backend block doesn't declare one
	Source string // .tf file the block was declared in (relative to discovery root)
}

// FindS3Backends walks every `.tf` file under root (skipping vendored / cache
// directories) and returns every S3 backend block it finds.
//
// Discovery is best-effort: a single unparseable file logs but doesn't fail
// the whole scan, matching the rest of the ingest pipeline.
func FindS3Backends(root string) ([]S3Backend, error) {
	parser := hclparse.NewParser()
	seen := map[string]struct{}{} // bucket + "\x00" + key
	var out []S3Backend

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == ".terraform" || name == "node_modules" || name == ".terragrunt-cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".tf") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		file, diags := parser.ParseHCL(src, path)
		if diags.HasErrors() {
			return nil
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			return nil
		}

		for _, block := range body.Blocks {
			if block.Type != "terraform" {
				continue
			}
			for _, nested := range block.Body.Blocks {
				if nested.Type != "backend" || len(nested.Labels) == 0 || nested.Labels[0] != "s3" {
					continue
				}
				b := extractS3Backend(nested.Body, src)
				if b.Bucket == "" || b.Key == "" {
					continue
				}
				dedup := b.Bucket + "\x00" + b.Key
				if _, dup := seen[dedup]; dup {
					continue
				}
				seen[dedup] = struct{}{}
				b.Source = path
				out = append(out, b)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s for backend blocks: %w", root, err)
	}
	return out, nil
}

func extractS3Backend(body *hclsyntax.Body, src []byte) S3Backend {
	var b S3Backend
	for name, attr := range body.Attributes {
		val := literalString(attr.Expr, src)
		if val == "" {
			continue
		}
		switch name {
		case "bucket":
			b.Bucket = val
		case "key":
			b.Key = val
		case "region":
			b.Region = val
		}
	}
	return b
}

// literalString returns the value of an HCL attribute if it's a plain string
// literal. Returns "" for references, interpolations, or anything else —
// which is fine: Terraform itself disallows most variable interpolation in
// backend blocks, so real-world values are virtually always literals.
func literalString(expr hclsyntax.Expression, src []byte) string {
	tmpl, ok := expr.(*hclsyntax.TemplateExpr)
	if !ok || !tmpl.IsStringLiteral() {
		return ""
	}
	r := tmpl.Range()
	if r.Start.Byte < 0 || r.End.Byte > len(src) {
		return ""
	}
	raw := strings.TrimSpace(string(src[r.Start.Byte:r.End.Byte]))
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		return raw[1 : len(raw)-1]
	}
	return raw
}
