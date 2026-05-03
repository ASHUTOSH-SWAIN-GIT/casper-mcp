package graph

import (
	"context"
	"sort"
	"strings"
)

// MemStore is a zero-dependency, in-memory implementation of Querier.
// Build one from a GraphSnapshot produced by ingest.Scan — no postgres needed.
type MemStore struct {
	resources []Resource
	deps      []Dependency
	byID      map[string]Resource
}

func NewMemStore(snapshot GraphSnapshot) *MemStore {
	byID := make(map[string]Resource, len(snapshot.Resources))
	for _, r := range snapshot.Resources {
		byID[r.ID] = r
	}
	return &MemStore{resources: snapshot.Resources, deps: snapshot.Dependencies, byID: byID}
}

func (m *MemStore) FindResources(_ context.Context, query string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		r Resource
		s int
	}
	var results []scored
	for _, r := range m.resources {
		s := findScore(r, q)
		if s > 0 {
			results = append(results, scored{r, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].s != results[j].s {
			return results[i].s > results[j].s
		}
		return results[i].r.Identifier < results[j].r.Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]Resource, len(results))
	for i, sr := range results {
		out[i] = sr.r
	}
	return out, nil
}

func (m *MemStore) GetDependencies(_ context.Context, resourceID string) ([]DependencyResult, error) {
	var results []DependencyResult
	for _, d := range m.deps {
		if d.FromResource == resourceID {
			if r, ok := m.byID[d.ToResource]; ok {
				results = append(results, DependencyResult{
					Direction: "dependency", Kind: d.Kind, Source: d.Source,
					Resource: r, Dependency: d,
				})
			}
		}
		if d.ToResource == resourceID {
			if r, ok := m.byID[d.FromResource]; ok {
				results = append(results, DependencyResult{
					Direction: "dependent", Kind: d.Kind, Source: d.Source,
					Resource: r, Dependency: d,
				})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Direction != results[j].Direction {
			return results[i].Direction < results[j].Direction
		}
		return results[i].Resource.Identifier < results[j].Resource.Identifier
	})
	return results, nil
}

func (m *MemStore) FindModules(_ context.Context, intent string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(intent))
	// Group by module path, return first representative resource per matching module
	seen := map[string]bool{}
	var results []Resource
	for _, r := range m.resources {
		mp := strings.ToLower(r.ModulePath)
		if mp == "" || seen[mp] {
			continue
		}
		if strings.Contains(mp, q) || strings.Contains(strings.ToLower(r.Type), q) {
			seen[mp] = true
			results = append(results, r)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ModulePath < results[j].ModulePath
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *MemStore) FindConventions(_ context.Context, resourceType string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(resourceType))
	var results []Resource
	for _, r := range m.resources {
		if strings.ToLower(r.Type) == q || strings.Contains(strings.ToLower(r.Type), q) {
			results = append(results, r)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Identifier < results[j].Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *MemStore) FindSimilar(_ context.Context, description string, limit int) ([]Resource, error) {
	tokens := tokenizeQuery(description)
	if len(tokens) == 0 {
		return nil, nil
	}
	type scored struct {
		r Resource
		s int
	}
	var results []scored
	for _, r := range m.resources {
		s := similarityScore(r, description, tokens)
		if s > 0 {
			results = append(results, scored{r, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].s != results[j].s {
			return results[i].s > results[j].s
		}
		return results[i].r.Identifier < results[j].r.Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]Resource, len(results))
	for i, sr := range results {
		out[i] = sr.r
	}
	return out, nil
}

func findScore(r Resource, q string) int {
	if strings.ToLower(r.ID) == q {
		return 100
	}
	ident := strings.ToLower(r.Identifier)
	if ident == q {
		return 90
	}
	typ := strings.ToLower(r.Type)
	attrText := strings.ToLower(marshalForSearch(r.Attributes))
	tagText := strings.ToLower(marshalForSearch(r.Tags))
	score := 0
	if strings.Contains(ident, q) {
		score += 70
	}
	if typ == q {
		score += 60
	} else if strings.Contains(typ, q) {
		score += 40
	}
	if strings.Contains(attrText, q) {
		score += 20
	}
	if strings.Contains(tagText, q) {
		score += 10
	}
	return score
}
