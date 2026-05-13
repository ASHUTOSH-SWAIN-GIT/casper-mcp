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

// Discover returns every .rego file under root. Every .rego file Casper
// finds becomes a loaded policy — there is no precedence between
// directories. `.casper/policies/` is just a *recommended* location for
// teams that want a clean convention, not a special one.
//
// Skips conventional noise directories (.git, .terraform, vendor, etc).
func Discover(root string) ([]RegoFile, error) {
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
