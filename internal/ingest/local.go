package ingest

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/config"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformstate"
)

type Summary struct {
	Files        int
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

				summary.Files++
				summary.Resources += len(result.Resources)
				summary.Dependencies += len(result.Dependencies)
			}
		}
	}

	return summary, nil
}
