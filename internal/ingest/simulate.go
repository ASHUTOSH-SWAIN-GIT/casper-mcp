package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
)

// SimulateImpact parses proposedCode as Terraform HCL, diffs it against the
// current snapshot, and returns which resources would be created or modified,
// what the downstream blast radius is, broken-reference warnings, and similar
// examples from the repo for each proposed resource type.
func SimulateImpact(current graph.GraphSnapshot, querier graph.Querier, proposedCode string) (*graph.ImpactResult, error) {
	// Write proposed code to a temp dir so the HCL parser can read it
	tmpDir, err := os.MkdirTemp("", "casper-sim-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "proposed.tf"), []byte(proposedCode), 0o644); err != nil {
		return nil, fmt.Errorf("write proposed code: %w", err)
	}

	proposed, _, err := terraformcode.ParseDirResources(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("parse proposed code: %w", err)
	}
	if len(proposed) == 0 {
		return nil, fmt.Errorf("no resource blocks found in proposed code")
	}

	// Build current-graph indexes
	currentByIdent := make(map[string]graph.Resource, len(current.Resources))
	currentByID := make(map[string]graph.Resource, len(current.Resources))
	for _, r := range current.Resources {
		currentByIdent[r.Identifier] = r
		currentByID[r.ID] = r
	}

	// dependentOf[id] = list of resource IDs that reference id
	// (FromResource depends on ToResource, so ToResource's dependents are FromResources)
	dependentOf := make(map[string][]string)
	for _, d := range current.Dependencies {
		dependentOf[d.ToResource] = append(dependentOf[d.ToResource], d.FromResource)
	}

	// Diff proposed vs current
	var created, modified []graph.ResourceDiff
	affectedCurrentIDs := map[string]string{} // currentID → identifier

	for _, prop := range proposed {
		cur, exists := currentByIdent[prop.Identifier]
		if !exists {
			args, _ := prop.Attributes["arguments"].(map[string]string)
			created = append(created, graph.ResourceDiff{
				Identifier: prop.Identifier,
				Type:       prop.Type,
				Arguments:  args,
			})
		} else {
			diff := diffArguments(cur, prop)
			if diff != nil {
				diff.Identifier = prop.Identifier
				diff.Type = prop.Type
				modified = append(modified, *diff)
				affectedCurrentIDs[cur.ID] = cur.Identifier
			}
		}
	}

	// Blast radius
	seen := map[string]bool{}
	var blast []graph.BlastItem

	// Downstream: who currently references a modified resource
	for affectedID, affectedIdent := range affectedCurrentIDs {
		for _, depID := range dependentOf[affectedID] {
			if seen[depID] {
				continue
			}
			seen[depID] = true
			if r, ok := currentByID[depID]; ok {
				blast = append(blast, graph.BlastItem{
					Identifier:   r.Identifier,
					Type:         r.Type,
					Relationship: "references " + affectedIdent + " (modified)",
				})
			}
		}
	}

	// Upstream: what do the newly created resources reference in the current graph
	for _, prop := range proposed {
		if _, exists := currentByIdent[prop.Identifier]; exists {
			continue // only care about new resources here
		}
		args, _ := prop.Attributes["arguments"].(map[string]string)
		for _, expr := range args {
			for ident, r := range currentByIdent {
				key := "upstream:" + ident
				if seen[key] {
					continue
				}
				// Match "type.name." or "type.name[" — actual attribute/index access
				if strings.Contains(expr, ident+".") || strings.Contains(expr, ident+"[") {
					seen[key] = true
					blast = append(blast, graph.BlastItem{
						Identifier:   r.Identifier,
						Type:         r.Type,
						Relationship: prop.Identifier + " will reference this",
					})
				}
			}
		}
	}

	sort.Slice(blast, func(i, j int) bool {
		return blast[i].Identifier < blast[j].Identifier
	})

	// Broken-reference warnings: proposed args reference type.name that isn't in current graph
	var warnings []string
	for _, prop := range proposed {
		args, _ := prop.Attributes["arguments"].(map[string]string)
		for argKey, expr := range args {
			for _, ref := range extractResourceRefs(expr) {
				if _, ok := currentByIdent[ref]; !ok {
					warnings = append(warnings, fmt.Sprintf(
						"%s.%s: argument %q references %q which is not in the current graph",
						argKey, prop.Identifier, argKey, ref,
					))
				}
			}
		}
	}
	sort.Strings(warnings)

	// Similar examples: for each proposed resource type, find matching examples in repo
	var similarExamples map[string][]graph.SimilarExample
	if querier != nil {
		ctx := context.Background()
		seenType := map[string]bool{}
		for _, prop := range proposed {
			if seenType[prop.Type] {
				continue
			}
			seenType[prop.Type] = true
			similar, err := querier.FindSimilar(ctx, prop.Type, 3)
			if err != nil || len(similar) == 0 {
				continue
			}
			if similarExamples == nil {
				similarExamples = make(map[string][]graph.SimilarExample)
			}
			for _, s := range similar {
				args, _ := s.Attributes["arguments"].(map[string]string)
				ex := graph.SimilarExample{
					Identifier: s.Identifier,
					ModulePath: s.ModulePath,
					Arguments:  args,
				}
				similarExamples[prop.Type] = append(similarExamples[prop.Type], ex)
			}
		}
	}

	summary := fmt.Sprintf(
		"%d resource(s) to create, %d to modify, %d in blast radius",
		len(created), len(modified), len(blast),
	)

	return &graph.ImpactResult{
		Summary:         summary,
		Created:         created,
		Modified:        modified,
		BlastRadius:     blast,
		Warnings:        warnings,
		SimilarExamples: similarExamples,
	}, nil
}

// extractResourceRefs pulls "type.name" references out of an HCL expression string.
// It looks for tokens matching the pattern word.word that appear in resource references.
func extractResourceRefs(expr string) []string {
	var refs []string
	// Split on common delimiters and look for word.word patterns
	parts := strings.FieldsFunc(expr, func(r rune) bool {
		return r == '"' || r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '(' || r == ')' || r == '[' || r == ']'
	})
	for _, part := range parts {
		// A resource reference looks like "type.name" or "type.name.attribute"
		// We want just the first two segments
		segments := strings.SplitN(part, ".", 3)
		if len(segments) >= 2 && isIdentifier(segments[0]) && isIdentifier(segments[1]) {
			// Skip known non-resource prefixes
			switch segments[0] {
			case "var", "local", "data", "module", "path", "each", "count", "self":
				continue
			}
			refs = append(refs, segments[0]+"."+segments[1])
		}
	}
	return refs
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func diffArguments(cur, prop graph.Resource) *graph.ResourceDiff {
	curArgs, _ := cur.Attributes["arguments"].(map[string]string)
	propArgs, _ := prop.Attributes["arguments"].(map[string]string)

	added := map[string]string{}
	changed := map[string]graph.ArgDiff{}
	var removed []string

	for k, v := range propArgs {
		if curV, ok := curArgs[k]; !ok {
			added[k] = v
		} else if curV != v {
			changed[k] = graph.ArgDiff{Before: curV, After: v}
		}
	}
	for k := range curArgs {
		if _, ok := propArgs[k]; !ok {
			removed = append(removed, k)
		}
	}

	if len(added) == 0 && len(changed) == 0 && len(removed) == 0 {
		return nil // no change
	}
	return &graph.ResourceDiff{Added: added, Changed: changed, Removed: removed}
}
