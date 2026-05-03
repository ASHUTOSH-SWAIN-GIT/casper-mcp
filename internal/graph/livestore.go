package graph

import (
	"context"
	"sync"
)

// LiveStore is a thread-safe Querier whose underlying MemStore can be
// hot-swapped via Reload. The MCP server holds a stable pointer to the
// LiveStore; the inner MemStore is replaced atomically when files change.
type LiveStore struct {
	mu       sync.RWMutex
	inner    *MemStore
	snapshot GraphSnapshot
}

func NewLiveStore(snapshot GraphSnapshot) *LiveStore {
	return &LiveStore{
		inner:    NewMemStore(snapshot),
		snapshot: snapshot,
	}
}

// Reload replaces the inner store with a fresh snapshot. Safe to call from
// a background goroutine while the MCP server is serving requests.
func (l *LiveStore) Reload(snapshot GraphSnapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inner = NewMemStore(snapshot)
	l.snapshot = snapshot
}

// Snapshot returns the current GraphSnapshot (used by the simulate closure).
func (l *LiveStore) Snapshot() GraphSnapshot {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.snapshot
}

func (l *LiveStore) FindResources(ctx context.Context, query string, limit int) ([]Resource, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inner.FindResources(ctx, query, limit)
}

func (l *LiveStore) GetDependencies(ctx context.Context, resourceID string) ([]DependencyResult, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inner.GetDependencies(ctx, resourceID)
}

func (l *LiveStore) FindModules(ctx context.Context, intent string, limit int) ([]Resource, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inner.FindModules(ctx, intent, limit)
}

func (l *LiveStore) FindConventions(ctx context.Context, resourceType string, limit int) ([]Resource, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inner.FindConventions(ctx, resourceType, limit)
}

func (l *LiveStore) FindSimilar(ctx context.Context, description string, limit int) ([]Resource, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.inner.FindSimilar(ctx, description, limit)
}
