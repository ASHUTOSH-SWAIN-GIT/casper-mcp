CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE resources (
  id TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  type TEXT NOT NULL,
  identifier TEXT NOT NULL,
  attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
  tags JSONB NOT NULL DEFAULT '{}'::jsonb,
  module_path TEXT,
  managed_by TEXT NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX resources_identifier_idx ON resources USING gin (identifier gin_trgm_ops);
CREATE INDEX resources_type_idx ON resources (type);
CREATE INDEX resources_tags_idx ON resources USING gin (tags);

CREATE TABLE dependencies (
  from_resource TEXT NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
  to_resource TEXT NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  source TEXT NOT NULL,
  PRIMARY KEY (from_resource, to_resource, kind, source)
);

CREATE INDEX dependencies_to_resource_idx ON dependencies (to_resource);
