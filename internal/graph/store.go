package graph

import (
	"context"
	"encoding/json"
	"fmt"

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
			OR attributes::text ILIKE '%' || $1 || '%'
			OR tags::text ILIKE '%' || $1 || '%'
		ORDER BY
			CASE WHEN identifier = $1 THEN 0 ELSE 1 END,
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

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
