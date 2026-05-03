package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func (s *Store) UpsertResources(ctx context.Context, resources []Resource) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, resource := range resources {
		attrs, err := json.Marshal(resource.Attributes)
		if err != nil {
			return fmt.Errorf("marshal attributes for %s: %w", resource.ID, err)
		}
		tags, err := json.Marshal(resource.Tags)
		if err != nil {
			return fmt.Errorf("marshal tags for %s: %w", resource.ID, err)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO resources (
				id, source, type, identifier, attributes, tags,
				module_path, managed_by, last_seen
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
			ON CONFLICT (id) DO UPDATE SET
				source = EXCLUDED.source,
				type = EXCLUDED.type,
				identifier = EXCLUDED.identifier,
				attributes = EXCLUDED.attributes,
				tags = EXCLUDED.tags,
				module_path = EXCLUDED.module_path,
				managed_by = EXCLUDED.managed_by,
				last_seen = now()
		`, resource.ID, resource.Source, resource.Type, resource.Identifier, attrs, tags, nullableString(resource.ModulePath), resource.ManagedBy)
		if err != nil {
			return fmt.Errorf("upsert resource %s: %w", resource.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Store) ReplaceDependencies(ctx context.Context, source string, dependencies []Dependency) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM dependencies WHERE source = $1`, source); err != nil {
		return fmt.Errorf("delete dependencies for %s: %w", source, err)
	}

	for _, dependency := range dependencies {
		_, err := tx.Exec(ctx, `
			INSERT INTO dependencies (from_resource, to_resource, kind, source)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT DO NOTHING
		`, dependency.FromResource, dependency.ToResource, dependency.Kind, dependency.Source)
		if err != nil {
			return fmt.Errorf("insert dependency %s -> %s: %w", dependency.FromResource, dependency.ToResource, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Store) FindResources(ctx context.Context, query string, limit int) ([]Resource, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, source, type, identifier, attributes, tags, COALESCE(module_path, ''), managed_by, last_seen
		FROM resources
		WHERE identifier ILIKE '%' || $1 || '%'
			OR type ILIKE '%' || $1 || '%'
			OR attributes ->> 'id' ILIKE '%' || $1 || '%'
			OR attributes ->> 'name' ILIKE '%' || $1 || '%'
			OR attributes ->> 'identifier' ILIKE '%' || $1 || '%'
			OR attributes::text ILIKE '%' || $1 || '%'
			OR tags::text ILIKE '%' || $1 || '%'
		ORDER BY
			CASE
				WHEN id = $1 THEN 0
				WHEN identifier = $1 THEN 1
				WHEN attributes ->> 'id' = $1 THEN 2
				WHEN attributes ->> 'identifier' = $1 THEN 3
				WHEN attributes ->> 'name' = $1 THEN 4
				WHEN identifier ILIKE '%' || $1 || '%' THEN 5
				WHEN type = $1 THEN 6
				ELSE 7
			END,
			identifier
		LIMIT $2
	`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query resources: %w", err)
	}
	defer rows.Close()

	var resources []Resource
	for rows.Next() {
		var resource Resource
		var attrs, tags []byte
		if err := rows.Scan(
			&resource.ID,
			&resource.Source,
			&resource.Type,
			&resource.Identifier,
			&attrs,
			&tags,
			&resource.ModulePath,
			&resource.ManagedBy,
			&resource.LastSeen,
		); err != nil {
			return nil, fmt.Errorf("scan resource: %w", err)
		}
		if err := json.Unmarshal(attrs, &resource.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes for %s: %w", resource.ID, err)
		}
		if err := json.Unmarshal(tags, &resource.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags for %s: %w", resource.ID, err)
		}
		resources = append(resources, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resources: %w", err)
	}
	return resources, nil
}

func (s *Store) FindModules(ctx context.Context, intent string, limit int) ([]Resource, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, source, type, identifier, attributes, tags, COALESCE(module_path, ''), managed_by, last_seen
		FROM resources
		WHERE type = 'terraform_module'
			AND managed_by = 'terraform_code'
			AND (
				identifier ILIKE '%' || $1 || '%'
				OR attributes::text ILIKE '%' || $1 || '%'
			)
		ORDER BY
			CASE
				WHEN identifier = $1 THEN 0
				WHEN identifier ILIKE '%' || $1 || '%' THEN 1
				WHEN attributes ->> 'path' ILIKE '%' || $1 || '%' THEN 2
				ELSE 3
			END,
			identifier
		LIMIT $2
	`, intent, limit)
	if err != nil {
		return nil, fmt.Errorf("query modules: %w", err)
	}
	defer rows.Close()

	var modules []Resource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		modules = append(modules, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate modules: %w", err)
	}
	return modules, nil
}

func (s *Store) FindConventions(ctx context.Context, resourceType string, limit int) ([]Resource, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, source, type, identifier, attributes, tags, COALESCE(module_path, ''), managed_by, last_seen
		FROM resources
		WHERE type = 'terraform_convention'
			AND managed_by = 'terraform_code'
			AND (
				identifier ILIKE '%' || $1 || '%'
				OR attributes ->> 'resource_type' = $1
				OR attributes::text ILIKE '%' || $1 || '%'
			)
		ORDER BY
			CASE
				WHEN attributes ->> 'resource_type' = $1 THEN 0
				WHEN identifier ILIKE $1 || '@%' THEN 1
				WHEN identifier ILIKE '%' || $1 || '%' THEN 2
				ELSE 3
			END,
			identifier
		LIMIT $2
	`, resourceType, limit)
	if err != nil {
		return nil, fmt.Errorf("query conventions: %w", err)
	}
	defer rows.Close()

	var conventions []Resource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		conventions = append(conventions, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conventions: %w", err)
	}
	return conventions, nil
}

func (s *Store) FindSimilar(ctx context.Context, description string, limit int) ([]Resource, error) {
	if limit <= 0 {
		limit = 10
	}

	tokens := tokenizeQuery(description)
	if len(tokens) == 0 {
		return nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, source, type, identifier, attributes, tags, COALESCE(module_path, ''), managed_by, last_seen
		FROM resources
		WHERE type <> 'terraform_convention'
			AND (
				identifier ILIKE ANY($1)
				OR type ILIKE ANY($1)
				OR attributes::text ILIKE ANY($1)
				OR tags::text ILIKE ANY($1)
			)
	`, ilikePatterns(tokens))
	if err != nil {
		return nil, fmt.Errorf("query similar resources: %w", err)
	}
	defer rows.Close()

	type scoredResource struct {
		resource Resource
		score    int
	}

	var scored []scoredResource
	for rows.Next() {
		resource, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		score := similarityScore(resource, description, tokens)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredResource{resource: resource, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate similar resources: %w", err)
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].resource.Identifier < scored[j].resource.Identifier
		}
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	results := make([]Resource, 0, len(scored))
	for _, item := range scored {
		results = append(results, item.resource)
	}
	return results, nil
}

func (s *Store) LoadGraphSnapshot(ctx context.Context) (GraphSnapshot, error) {
	resourceRows, err := s.pool.Query(ctx, `
		SELECT id, source, type, identifier, attributes, tags, COALESCE(module_path, ''), managed_by, last_seen
		FROM resources
		ORDER BY
			CASE
				WHEN type = 'terraform_module' THEN 0
				WHEN type = 'terraform_convention' THEN 2
				ELSE 1
			END,
			identifier
	`)
	if err != nil {
		return GraphSnapshot{}, fmt.Errorf("query resources snapshot: %w", err)
	}
	defer resourceRows.Close()

	var snapshot GraphSnapshot
	for resourceRows.Next() {
		resource, err := scanResource(resourceRows)
		if err != nil {
			return GraphSnapshot{}, err
		}
		snapshot.Resources = append(snapshot.Resources, resource)
	}
	if err := resourceRows.Err(); err != nil {
		return GraphSnapshot{}, fmt.Errorf("iterate resources snapshot: %w", err)
	}

	dependencyRows, err := s.pool.Query(ctx, `
		SELECT from_resource, to_resource, kind, source
		FROM dependencies
		ORDER BY from_resource, to_resource, kind
	`)
	if err != nil {
		return GraphSnapshot{}, fmt.Errorf("query dependencies snapshot: %w", err)
	}
	defer dependencyRows.Close()

	for dependencyRows.Next() {
		var dependency Dependency
		if err := dependencyRows.Scan(
			&dependency.FromResource,
			&dependency.ToResource,
			&dependency.Kind,
			&dependency.Source,
		); err != nil {
			return GraphSnapshot{}, fmt.Errorf("scan dependency snapshot: %w", err)
		}
		snapshot.Dependencies = append(snapshot.Dependencies, dependency)
	}
	if err := dependencyRows.Err(); err != nil {
		return GraphSnapshot{}, fmt.Errorf("iterate dependencies snapshot: %w", err)
	}

	return snapshot, nil
}

func (s *Store) GetDependencies(ctx context.Context, resourceID string) ([]DependencyResult, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			'dependency' AS direction,
			d.kind,
			d.source,
			r.id,
			r.source,
			r.type,
			r.identifier,
			r.attributes,
			r.tags,
			COALESCE(r.module_path, ''),
			r.managed_by,
			r.last_seen,
			d.from_resource,
			d.to_resource
		FROM dependencies d
		JOIN resources r ON r.id = d.to_resource
		WHERE d.from_resource = $1

		UNION ALL

		SELECT
			'dependent' AS direction,
			d.kind,
			d.source,
			r.id,
			r.source,
			r.type,
			r.identifier,
			r.attributes,
			r.tags,
			COALESCE(r.module_path, ''),
			r.managed_by,
			r.last_seen,
			d.from_resource,
			d.to_resource
		FROM dependencies d
		JOIN resources r ON r.id = d.from_resource
		WHERE d.to_resource = $1

		ORDER BY direction, identifier
	`, resourceID)
	if err != nil {
		return nil, fmt.Errorf("query dependencies: %w", err)
	}
	defer rows.Close()

	var results []DependencyResult
	for rows.Next() {
		var result DependencyResult
		var attrs, tags []byte
		if err := rows.Scan(
			&result.Direction,
			&result.Kind,
			&result.Source,
			&result.Resource.ID,
			&result.Resource.Source,
			&result.Resource.Type,
			&result.Resource.Identifier,
			&attrs,
			&tags,
			&result.Resource.ModulePath,
			&result.Resource.ManagedBy,
			&result.Resource.LastSeen,
			&result.Dependency.FromResource,
			&result.Dependency.ToResource,
		); err != nil {
			return nil, fmt.Errorf("scan dependency: %w", err)
		}
		result.Dependency.Kind = result.Kind
		result.Dependency.Source = result.Source
		if err := json.Unmarshal(attrs, &result.Resource.Attributes); err != nil {
			return nil, fmt.Errorf("unmarshal attributes for %s: %w", result.Resource.ID, err)
		}
		if err := json.Unmarshal(tags, &result.Resource.Tags); err != nil {
			return nil, fmt.Errorf("unmarshal tags for %s: %w", result.Resource.ID, err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dependencies: %w", err)
	}
	return results, nil
}

type resourceScanner interface {
	Scan(dest ...any) error
}

func scanResource(scanner resourceScanner) (Resource, error) {
	var resource Resource
	var attrs, tags []byte
	if err := scanner.Scan(
		&resource.ID,
		&resource.Source,
		&resource.Type,
		&resource.Identifier,
		&attrs,
		&tags,
		&resource.ModulePath,
		&resource.ManagedBy,
		&resource.LastSeen,
	); err != nil {
		return Resource{}, fmt.Errorf("scan resource: %w", err)
	}
	if err := json.Unmarshal(attrs, &resource.Attributes); err != nil {
		return Resource{}, fmt.Errorf("unmarshal attributes for %s: %w", resource.ID, err)
	}
	if err := json.Unmarshal(tags, &resource.Tags); err != nil {
		return Resource{}, fmt.Errorf("unmarshal tags for %s: %w", resource.ID, err)
	}
	return resource, nil
}

func tokenizeQuery(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	seen := map[string]struct{}{}
	var tokens []string
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		tokens = append(tokens, part)
	}
	return tokens
}

func ilikePatterns(tokens []string) []string {
	patterns := make([]string, 0, len(tokens))
	for _, token := range tokens {
		patterns = append(patterns, "%"+token+"%")
	}
	return patterns
}

func similarityScore(resource Resource, query string, tokens []string) int {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	lowerIdentifier := strings.ToLower(resource.Identifier)
	lowerType := strings.ToLower(resource.Type)
	attributesText := strings.ToLower(marshalForSearch(resource.Attributes))
	tagsText := strings.ToLower(marshalForSearch(resource.Tags))
	modulePath := strings.ToLower(resource.ModulePath)

	score := 0
	switch {
	case lowerIdentifier == lowerQuery:
		score += 120
	case strings.Contains(lowerIdentifier, lowerQuery) && lowerQuery != "":
		score += 70
	}

	if valueEquals(resource.Attributes, lowerQuery, "id", "identifier", "name", "path") {
		score += 90
	}

	if strings.Contains(lowerType, lowerQuery) && lowerQuery != "" {
		score += 60
	}
	if strings.Contains(attributesText, lowerQuery) && lowerQuery != "" {
		score += 40
	}
	if strings.Contains(tagsText, lowerQuery) && lowerQuery != "" {
		score += 20
	}

	for _, token := range tokens {
		if strings.Contains(lowerIdentifier, token) {
			score += 20
		}
		if strings.Contains(lowerType, token) {
			score += 16
		}
		if strings.Contains(modulePath, token) {
			score += 12
		}
		if strings.Contains(attributesText, token) {
			score += 10
		}
		if strings.Contains(tagsText, token) {
			score += 4
		}
	}

	if resource.Type == "terraform_module" {
		score += 8
	}

	return score
}

func valueEquals(values map[string]any, query string, keys ...string) bool {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprint(raw), query) {
			return true
		}
	}
	return false
}

func marshalForSearch(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
