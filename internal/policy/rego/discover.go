// Package rego discovers and evaluates OPA/Rego policies declared
// alongside Terraform code. It wraps the OPA v1 Go library so the rest of
// Casper sees policy violations through the same Engine interface the YAML
// engine implements — only the input language differs.
package rego

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RegoFile is one discovered .rego policy file, ready to compile.
type RegoFile struct {
	Path  string // absolute path
	Bytes []byte
}

// CasperPolicyDir is the convention location for Casper-specific Rego
// policies, symmetric with .casper/policies.yaml for the YAML engine.
// When this directory exists with .rego files inside, Discover uses only
// those — repo-wide walk is skipped.
const CasperPolicyDir = ".casper/policies"

// skipDir names directories Discover should not descend into. Mirrors the
// existing scan-pipeline skip set so policy discovery agrees with module
// discovery on what counts as "noise."
var skipDir = map[string]struct{}{
	".git":              {},
	".terraform":        {},
	"node_modules":      {},
	".terragrunt-cache": {},
	"vendor":            {},
	// testdata is a Go-stdlib convention for test fixtures. Skipping it
	// means casper-mcp run against this repo (or any repo with rego inside
	// testdata/) doesn't pick up test policies as real ones.
	"testdata": {},
}

// Discover returns every .rego file Casper should treat as an active
// policy. Discovery is ordered:
//
//  1. If <root>/.casper/policies/ exists and contains any .rego files,
//     use only those (the Casper convention path — explicit user intent).
//  2. Otherwise, walk the entire repo for .rego files (zero-config Conftest
//     compatibility — drop policies anywhere).
//
// Both modes skip the conventional noise directories.
func Discover(root string) ([]RegoFile, error) {
	// Convention path first.
	casperDir := filepath.Join(root, CasperPolicyDir)
	if info, err := os.Stat(casperDir); err == nil && info.IsDir() {
		files, err := walkRego(casperDir)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}
		// Empty .casper/policies/ — fall through to repo-wide walk rather
		// than serving zero policies. Treat an empty dir as a placeholder.
	}

	// Fallback: full repo walk.
	return walkRego(root)
}

// IsCasperPolicyDir reports whether root contains a non-empty
// .casper/policies/ directory. Used by the startup logs in main so we can
// tell users which discovery mode fired.
func IsCasperPolicyDir(root string) bool {
	casperDir := filepath.Join(root, CasperPolicyDir)
	info, err := os.Stat(casperDir)
	if err != nil || !info.IsDir() {
		return false
	}
	files, err := walkRego(casperDir)
	return err == nil && len(files) > 0
}

// walkRego is the shared per-directory walker used by both Discover modes.
func walkRego(root string) ([]RegoFile, error) {
	seen := map[string]struct{}{}
	var out []RegoFile

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort: skip unreadable entries
		}
		if entry.IsDir() {
			if _, skip := skipDir[entry.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".rego") {
			return nil
		}

		abs, err := filepath.Abs(path)
		if err != nil {
			return nil
		}
		if _, dup := seen[abs]; dup {
			return nil
		}
		seen[abs] = struct{}{}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // best-effort: skip unreadable files
		}
		out = append(out, RegoFile{Path: abs, Bytes: data})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s for rego files: %w", root, err)
	}
	return out, nil
}
