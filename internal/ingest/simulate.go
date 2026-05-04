package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"os/exec"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/ingest/terraformcode"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/workflow"
)

// SimulateImpact parses proposedCode as Terraform HCL, diffs it against the
// current snapshot, and returns which resources would be created or modified,
// what the downstream blast radius is, broken-reference warnings, and similar
// examples from the repo for each proposed resource type.
func SimulateImpact(current graph.GraphSnapshot, querier graph.Querier, policies []policy.Policy, workflowRules []workflow.WorkflowRule, proposedCode string) (*graph.ImpactResult, error) {
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
						"%s: argument %q references %q which is not in the current graph",
						prop.Identifier, argKey, ref,
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

	// Reversibility context: per-resource facts for the agent to reason about rollback
	revCtx := buildReversibilityContext(proposed, currentByIdent, currentByID, dependentOf, tmpDir)

	// Policy violations: check created and modified resources against org policies
	var policyViolations []graph.PolicyViolation
	for _, prop := range proposed {
		args, _ := prop.Attributes["arguments"].(map[string]string)
		tags := flattenTags(prop.Tags)
		for _, v := range policy.Check(policies, prop.Type, prop.Identifier, args, tags) {
			policyViolations = append(policyViolations, graph.PolicyViolation{
				PolicyID: v.PolicyID,
				Resource: v.Resource,
				Type:     v.Type,
				Message:  v.Message,
				Details:  v.Details,
			})
		}
	}

	// Workflow decision: advisory routing based on env, operation, and resource family
	var wfDecision *graph.WorkflowDecision
	if len(workflowRules) > 0 {
		var inputs []workflow.ResourceInput
		for _, prop := range proposed {
			op := "create"
			cur, exists := currentByIdent[prop.Identifier]
			if exists {
				op = "modify"
			}
			tags := flattenTags(prop.Tags)
			modulePath := ""
			if exists {
				modulePath = cur.ModulePath
			}
			inputs = append(inputs, workflow.ResourceInput{
				Identifier: prop.Identifier,
				Type:       prop.Type,
				Operation:  op,
				Tags:       tags,
				ModulePath: modulePath,
				Source:     prop.Source,
			})
		}
		wfDecision = workflow.Evaluate(workflowRules, inputs)
	}

	return &graph.ImpactResult{
		Summary:              summary,
		Created:              created,
		Modified:             modified,
		BlastRadius:          blast,
		Warnings:             warnings,
		SimilarExamples:      similarExamples,
		ReversibilityContext: revCtx,
		PolicyViolations:     policyViolations,
		WorkflowDecision:     wfDecision,
	}, nil
}

func buildReversibilityContext(
	proposed []graph.Resource,
	currentByIdent map[string]graph.Resource,
	currentByID map[string]graph.Resource,
	dependentOf map[string][]string,
	tmpDir string,
) *graph.ReversibilityContext {
	// Parse the raw HCL file for lifecycle block extraction
	hclBody, hclSrc := parseHCLBody(filepath.Join(tmpDir, "proposed.tf"))

	var contexts []graph.ResourceContext

	for _, prop := range proposed {
		propArgs, _ := prop.Attributes["arguments"].(map[string]string)

		rc := graph.ResourceContext{
			Identifier:     prop.Identifier,
			Type:           prop.Type,
			ProposedArgs:   propArgs,
			LifecycleFlags: extractLifecycleFlags(hclBody, hclSrc, prop.Type, resourceNameFromIdent(prop.Identifier)),
		}

		// deletion_protection from proposed args
		if propArgs["deletion_protection"] == "true" {
			rc.LifecycleFlags.DeletionProtection = true
		}

		if cur, exists := currentByIdent[prop.Identifier]; exists {
			rc.Operation = "modify"
			curArgs, _ := cur.Attributes["arguments"].(map[string]string)
			rc.CurrentArgs = curArgs

			diff := diffArguments(cur, prop)
			if diff != nil {
				rc.ChangedArgs = diff.Changed
				rc.AddedArgs = diff.Added
				rc.RemovedArgs = diff.Removed
			}

			// Dependents: who references this resource in the current graph
			for _, depID := range dependentOf[cur.ID] {
				if r, ok := currentByID[depID]; ok {
					rc.Dependents = append(rc.Dependents, r.Identifier)
				}
			}

			rc.RecentCommits = gitHistoryForResource(cur.Source, prop.Type, resourceNameFromIdent(prop.Identifier))
		} else {
			rc.Operation = "create"
		}

		// DependsOn: what this proposed resource references
		seenRef := map[string]bool{}
		for _, expr := range propArgs {
			for _, ref := range extractResourceRefs(expr) {
				if !seenRef[ref] {
					seenRef[ref] = true
					rc.DependsOn = append(rc.DependsOn, ref)
				}
			}
		}
		sort.Strings(rc.DependsOn)
		sort.Strings(rc.Dependents)

		contexts = append(contexts, rc)
	}

	sort.Slice(contexts, func(i, j int) bool {
		return contexts[i].Identifier < contexts[j].Identifier
	})

	return &graph.ReversibilityContext{Resources: contexts}
}

// resourceNameFromIdent extracts the resource name from "type.name" identifier.
func resourceNameFromIdent(ident string) string {
	if _, after, ok := strings.Cut(ident, "."); ok {
		return after
	}
	return ident
}

// parseHCLBody parses a .tf file and returns the root body and raw source bytes.
// Both are nil/empty on error.
func parseHCLBody(path string) (*hclsyntax.Body, []byte) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	f, diags := hclsyntax.ParseConfig(src, path, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, nil
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		return nil, nil
	}
	return body, src
}

// extractLifecycleFlags reads the lifecycle block for a specific resource block.
// src is the raw HCL source bytes, used to extract ignore_changes values.
func extractLifecycleFlags(body *hclsyntax.Body, src []byte, resourceType, resourceName string) graph.LifecycleFlags {
	if body == nil {
		return graph.LifecycleFlags{}
	}
	for _, block := range body.Blocks {
		if block.Type != "resource" || len(block.Labels) < 2 {
			continue
		}
		if block.Labels[0] != resourceType || block.Labels[1] != resourceName {
			continue
		}
		for _, lb := range block.Body.Blocks {
			if lb.Type != "lifecycle" {
				continue
			}
			var flags graph.LifecycleFlags
			if attr, ok := lb.Body.Attributes["prevent_destroy"]; ok {
				val, diags := attr.Expr.Value(nil)
				if !diags.HasErrors() && val.Type() == cty.Bool {
					flags.PreventDestroy = val.True()
				}
			}
			if attr, ok := lb.Body.Attributes["create_before_destroy"]; ok {
				val, diags := attr.Expr.Value(nil)
				if !diags.HasErrors() && val.Type() == cty.Bool {
					flags.CreateBeforeDestroy = val.True()
				}
			}
			if attr, ok := lb.Body.Attributes["ignore_changes"]; ok {
				if len(src) > 0 {
					r := attr.Expr.Range()
					if r.Start.Byte >= 0 && r.End.Byte <= len(src) {
						raw := strings.TrimSpace(string(src[r.Start.Byte:r.End.Byte]))
						// raw looks like: [engine, instance_class] or ["engine"]
						raw = strings.Trim(raw, "[]")
						for _, part := range strings.Split(raw, ",") {
							part = strings.TrimSpace(part)
							part = strings.Trim(part, `"`)
							if part != "" && part != "all" {
								flags.IgnoreChanges = append(flags.IgnoreChanges, part)
							} else if part == "all" {
								flags.IgnoreChanges = []string{"all"}
								break
							}
						}
					}
				}
			}
			return flags
		}
	}
	return graph.LifecycleFlags{}
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
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
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

// gitHistoryForResource returns the last 3 commits that touched the specific
// resource block using git pickaxe (-S). Falls back to recent dir-level commits
// if the pickaxe search returns nothing (e.g. resource predates git history).
func gitHistoryForResource(dir, resourceType, resourceName string) []graph.GitCommit {
	if dir == "" {
		return nil
	}

	format := "--format=%H|%s|%an|%ad"
	search := fmt.Sprintf(`resource "%s" "%s"`, resourceType, resourceName)

	run := func(extraArgs ...string) []graph.GitCommit {
		args := append([]string{"-C", dir, "log", "--date=short", "-3", format}, extraArgs...)
		out, err := exec.Command("git", args...).Output()
		if err != nil || len(out) == 0 {
			return nil
		}
		var commits []graph.GitCommit
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) != 4 {
				continue
			}
			commits = append(commits, graph.GitCommit{
				Hash:    parts[0][:min(7, len(parts[0]))],
				Message: parts[1],
				Author:  parts[2],
				Date:    parts[3],
			})
		}
		return commits
	}

	// Pickaxe: commits that added/removed this exact resource block
	if commits := run("-S", search); len(commits) > 0 {
		return commits
	}
	// Fallback: recent commits touching any .tf file in the dir
	return run("--", "*.tf")
}

// flattenTags converts Resource.Tags (map[string]any) to map[string]string for
// policy evaluation. Non-string tag values are converted with fmt.Sprintf.
func flattenTags(tags map[string]any) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		switch s := v.(type) {
		case string:
			out[k] = s
		default:
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}
