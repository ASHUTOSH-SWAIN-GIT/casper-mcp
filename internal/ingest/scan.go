package ingest

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformstate"
)

// StateSource describes a remote Terraform state file Casper will fetch.
// The agent-facing list_state_sources tool surfaces these so users can see
// what Casper picked up from their `terraform { backend "..." {} }` blocks.
type StateSource struct {
	Type       string // "s3"
	Identity   string // e.g. "s3://bucket/key" — also recorded on each resource's Source field
	Bucket     string // s3-specific
	Key        string // s3-specific
	Region     string // s3-specific (may be empty)
	DeclaredIn string // path of the .tf file that declared the backend
	// Fetch returns the state bytes for this source.
	Fetch func(ctx context.Context) ([]byte, error)
}

// StateSourceStatus is the per-source result of the last fetch attempt.
// Returned alongside the snapshot from ScanWithRemoteStates and surfaced
// to agents via the list_state_sources MCP tool.
type StateSourceStatus struct {
	Type          string `json:"type"`
	Identity      string `json:"identity"`
	Bucket        string `json:"bucket,omitempty"`
	Key           string `json:"key,omitempty"`
	Region        string `json:"region,omitempty"`
	DeclaredIn    string `json:"declared_in,omitempty"`
	Status        string `json:"status"`           // "loaded" | "failed"
	Error         string `json:"error,omitempty"`  // populated when Status == "failed"
	ResourceCount int    `json:"resource_count"`
	EdgeCount     int    `json:"edge_count"`
}

// Scan parses Terraform state and code under dir and returns a GraphSnapshot
// without requiring a database. Equivalent to ScanWithRemoteStates with no
// remote sources — kept for tests and callers that only want local files.
func Scan(dir string) (graph.GraphSnapshot, error) {
	snap, _, err := ScanWithRemoteStates(context.Background(), dir, nil)
	return snap, err
}

// ScanWithRemoteStates is Scan plus remote state fetching. Local .tfstate
// files are read first, then every remote source is fetched in parallel
// (capped at 8 concurrent). All results merge into a single GraphSnapshot.
// The returned statuses describe what was attempted and whether it worked,
// one entry per source — surfaced to agents through list_state_sources.
func ScanWithRemoteStates(ctx context.Context, dir string, sources []StateSource) (graph.GraphSnapshot, []StateSourceStatus, error) {
	var snapshot graph.GraphSnapshot

	stateFiles, _ := doublestar.FilepathGlob(filepath.Join(filepath.Clean(dir), "**/*.tfstate"))
	for _, f := range stateFiles {
		result, err := terraformstate.ParseFile(f)
		if err != nil {
			log.Printf("casper: skipping state file %s: %v", f, err)
			continue
		}
		backfillProvider(result.Resources)
		snapshot.Resources = append(snapshot.Resources, result.Resources...)
		snapshot.Dependencies = append(snapshot.Dependencies, result.Dependencies...)
	}

	// Remote state fetches run in parallel — typically S3 GetObject, which is
	// network-bound and benefits from concurrency. Cap matches the
	// /Users/lowkeydev/code/infrastructure shape (6 backends) with headroom.
	var statuses []StateSourceStatus
	if len(sources) > 0 {
		var remoteResults []terraformstate.Result
		remoteResults, statuses = fetchRemoteStates(ctx, sources)
		for _, r := range remoteResults {
			snapshot.Resources = append(snapshot.Resources, r.Resources...)
			snapshot.Dependencies = append(snapshot.Dependencies, r.Dependencies...)
		}
	}

	moduleDirs, err := findModuleDirs(dir)
	if err != nil {
		return snapshot, statuses, err
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
			log.Printf("casper: skipping module %s: %v", d, err)
			continue
		}
		snapshot.Resources = append(snapshot.Resources, resources...)
		snapshot.Dependencies = append(snapshot.Dependencies, deps...)
	}

	// Resolve cross-module placeholder edges. Each placeholder points at a
	// target module directory; expand it into one real edge per resource that
	// lives inside that directory. Drop placeholders we can't resolve so they
	// don't pollute the graph.
	resourcesByDir := map[string][]string{}
	for _, r := range snapshot.Resources {
		if r.Source == "" || r.Type == "terraform_module" || r.Type == "terraform_convention" {
			continue
		}
		clean := filepath.Clean(r.Source)
		resourcesByDir[clean] = append(resourcesByDir[clean], r.ID)
	}

	resolved := make([]graph.Dependency, 0, len(snapshot.Dependencies))
	for _, dep := range snapshot.Dependencies {
		target, ok := strings.CutPrefix(dep.ToResource, terraformcode.ModuleEdgePlaceholder)
		if !ok {
			resolved = append(resolved, dep)
			continue
		}
		ids := resourcesByDir[filepath.Clean(target)]
		for _, toID := range ids {
			if toID == dep.FromResource {
				continue
			}
			resolved = append(resolved, graph.Dependency{
				FromResource: dep.FromResource,
				ToResource:   toID,
				Kind:         dep.Kind,
				Source:       dep.Source,
			})
		}
	}
	snapshot.Dependencies = resolved

	return snapshot, statuses, nil
}

// backfillProvider sets r.Provider on every resource that doesn't have one,
// derived from the resource type. State-derived resources don't get a
// provider set by the state parser.
func backfillProvider(resources []graph.Resource) {
	for i := range resources {
		if resources[i].Provider == "" {
			resources[i].Provider = terraformcode.ProviderFromType(resources[i].Type)
		}
	}
}

// fetchRemoteStates runs every source's Fetch concurrently (max 8 at a time),
// parses each successful response, and returns both the parsed results and a
// per-source status entry. Per-fetcher errors are logged but never fail the
// whole scan — a missing or permission-denied state file shouldn't take down
// the server.
func fetchRemoteStates(ctx context.Context, sources []StateSource) ([]terraformstate.Result, []StateSourceStatus) {
	const maxConcurrent = 8
	sem := make(chan struct{}, maxConcurrent)
	results := make([]terraformstate.Result, len(sources))
	statuses := make([]StateSourceStatus, len(sources))
	var wg sync.WaitGroup

	for i := range sources {
		src := sources[i]
		statuses[i] = StateSourceStatus{
			Type:       src.Type,
			Identity:   src.Identity,
			Bucket:     src.Bucket,
			Key:        src.Key,
			Region:     src.Region,
			DeclaredIn: src.DeclaredIn,
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, s StateSource) {
			defer wg.Done()
			defer func() { <-sem }()

			data, err := s.Fetch(ctx)
			if err != nil {
				log.Printf("casper: remote state fetch failed (%s): %v", s.Identity, err)
				statuses[idx].Status = "failed"
				statuses[idx].Error = err.Error()
				return
			}
			parsed, err := terraformstate.ParseBytes(data, s.Identity)
			if err != nil {
				log.Printf("casper: remote state parse failed (%s): %v", s.Identity, err)
				statuses[idx].Status = "failed"
				statuses[idx].Error = err.Error()
				return
			}
			backfillProvider(parsed.Resources)
			log.Printf("casper: loaded %d resources, %d edges from %s",
				len(parsed.Resources), len(parsed.Dependencies), s.Identity)
			results[idx] = parsed
			statuses[idx].Status = "loaded"
			statuses[idx].ResourceCount = len(parsed.Resources)
			statuses[idx].EdgeCount = len(parsed.Dependencies)
		}(i, src)
	}
	wg.Wait()
	return results, statuses
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
