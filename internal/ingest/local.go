package ingest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/config"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformstate"
)

type Summary struct {
	StateFiles   int
	CodeModules  int
	Resources    int
	Dependencies int
}

func Run(ctx context.Context, cfg config.Config, store *graph.Store) (Summary, error) {
	var summary Summary

	for _, state := range cfg.States {
		if state.Type != "local" {
			return Summary{}, fmt.Errorf("state type %q is not supported", state.Type)
		}

		for _, pattern := range state.Paths {
			matches, err := doublestar.FilepathGlob(filepath.Clean(pattern))
			if err != nil {
				return Summary{}, fmt.Errorf("expand state path %q: %w", pattern, err)
			}
			if len(matches) == 0 {
				return Summary{}, fmt.Errorf("state path %q matched no files", pattern)
			}

			for _, match := range matches {
				result, err := terraformstate.ParseFile(match)
				if err != nil {
					return Summary{}, err
				}
				if err := store.UpsertResources(ctx, result.Resources); err != nil {
					return Summary{}, err
				}
				if err := store.ReplaceDependencies(ctx, filepath.Clean(match), result.Dependencies); err != nil {
					return Summary{}, err
				}

				summary.StateFiles++
				summary.Resources += len(result.Resources)
				summary.Dependencies += len(result.Dependencies)
			}
		}
	}

	moduleDirs, err := terraformModuleDirs(cfg.IAC)
	if err != nil {
		return Summary{}, err
	}
	for _, moduleDir := range moduleDirs {
		resources, err := terraformcode.ParseDir(moduleDir)
		if err != nil {
			return Summary{}, err
		}
		if len(resources) == 0 {
			continue
		}
		if err := store.UpsertResources(ctx, resources); err != nil {
			return Summary{}, err
		}

		summary.CodeModules++
		summary.Resources += len(resources)
	}

	return summary, nil
}

func terraformModuleDirs(cfg config.IACConfig) ([]string, error) {
	dirs := map[string]struct{}{}

	for _, pattern := range cfg.Paths {
		matches, err := doublestar.FilepathGlob(filepath.Clean(pattern))
		if err != nil {
			return nil, fmt.Errorf("expand iac path %q: %w", pattern, err)
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.IsDir() {
				dirs[filepath.Clean(match)] = struct{}{}
			} else {
				dirs[filepath.Dir(filepath.Clean(match))] = struct{}{}
			}
		}
	}

	for _, moduleDir := range cfg.ModuleDirs {
		root := filepath.Clean(moduleDir)
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			continue
		}

		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			if entry.Name() == ".terraform" {
				return filepath.SkipDir
			}
			if tfconfig.IsModuleDir(path) {
				dirs[filepath.Clean(path)] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk module dir %q: %w", moduleDir, err)
		}
	}

	result := make([]string, 0, len(dirs))
	for dir := range dirs {
		result = append(result, dir)
	}
	sortStrings(result)
	return result, nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
