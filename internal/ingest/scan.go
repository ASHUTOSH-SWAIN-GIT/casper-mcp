package ingest

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformstate"
)

// Scan parses Terraform state and code under dir and returns a GraphSnapshot
// without requiring a database.
func Scan(dir string) (graph.GraphSnapshot, error) {
	var snapshot graph.GraphSnapshot

	stateFiles, _ := doublestar.FilepathGlob(filepath.Join(filepath.Clean(dir), "**/*.tfstate"))
	for _, f := range stateFiles {
		result, err := terraformstate.ParseFile(f)
		if err != nil {
			continue
		}
		snapshot.Resources = append(snapshot.Resources, result.Resources...)
		snapshot.Dependencies = append(snapshot.Dependencies, result.Dependencies...)
	}

	moduleDirs, err := findModuleDirs(dir)
	if err != nil {
		return snapshot, err
	}

	// Include downloaded child modules from any terraform init that has been run.
	// Deduplicates so the same source isn't indexed twice.
	allDirs := append(moduleDirs, childModuleDirs(moduleDirs)...)
	seen := map[string]struct{}{}
	for _, d := range allDirs {
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		resources, deps, err := terraformcode.ParseDirResources(d)
		if err != nil {
			continue
		}
		snapshot.Resources = append(snapshot.Resources, resources...)
		snapshot.Dependencies = append(snapshot.Dependencies, deps...)
	}

	return snapshot, nil
}

// childModuleDirs reads .terraform/modules/modules.json next to each component
// dir and returns the paths of downloaded child modules. Only called when
// terraform init has already been run — silently returns nothing otherwise.
func childModuleDirs(componentDirs []string) []string {
	type moduleEntry struct {
		Key    string `json:"Key"`
		Source string `json:"Source"`
		Dir    string `json:"Dir"`
	}
	type modulesFile struct {
		Modules []moduleEntry `json:"Modules"`
	}

	seenSource := map[string]struct{}{} // deduplicate by source string
	var result []string

	for _, compDir := range componentDirs {
		data, err := os.ReadFile(filepath.Join(compDir, ".terraform", "modules", "modules.json"))
		if err != nil {
			continue // terraform init not run here
		}
		var mf modulesFile
		if err := json.Unmarshal(data, &mf); err != nil {
			continue
		}
		for _, mod := range mf.Modules {
			if mod.Dir == "" || mod.Dir == "." {
				continue
			}
			// Deduplicate: same registry source (e.g. cloudposse/rds/aws 1.1.0)
			// shouldn't be scanned multiple times from different component dirs.
			srcKey := mod.Source
			if srcKey == "" {
				srcKey = mod.Dir
			}
			if _, ok := seenSource[srcKey]; ok {
				continue
			}
			seenSource[srcKey] = struct{}{}

			absDir := filepath.Clean(filepath.Join(compDir, mod.Dir))
			if !tfconfig.IsModuleDir(absDir) {
				continue
			}
			result = append(result, absDir)
		}
	}

	sort.Strings(result)
	return result
}

func findModuleDirs(root string) ([]string, error) {
	seen := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if name == ".git" || name == ".terraform" || name == "node_modules" || name == ".terragrunt-cache" {
			return filepath.SkipDir
		}
		if tfconfig.IsModuleDir(path) {
			seen[filepath.Clean(path)] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// AutoConfig writes a .casper/config.yaml for dir if one does not exist.
// Returns the path written (or existing path if already present).
func AutoConfig(dir, dbURL string) (string, error) {
	cfgDir := filepath.Join(dir, ".casper")
	cfgPath := filepath.Join(cfgDir, "config.yaml")

	if _, err := os.Stat(cfgPath); err == nil {
		return cfgPath, nil
	}

	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return "", err
	}

	stateGlob := filepath.Join(dir, "**/*.tfstate")
	stateFiles, _ := doublestar.FilepathGlob(stateGlob)

	stateSection := ""
	if len(stateFiles) > 0 {
		stateSection = "states:\n  - type: local\n    paths:\n      - \"" + filepath.ToSlash(stateGlob) + "\"\n\n"
	}

	content := "database:\n  url: " + dbURL + "\n\n" +
		stateSection +
		"iac:\n  module_dirs:\n    - \"" + filepath.ToSlash(dir) + "\"\n"

	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return cfgPath, nil
}
