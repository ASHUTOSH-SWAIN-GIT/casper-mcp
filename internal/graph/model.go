package graph

import "time"

type Resource struct {
	ID         string
	Source     string
	Type       string
	Identifier string
	Attributes map[string]any
	Tags       map[string]any
	ModulePath string
	ManagedBy  string
	LastSeen   time.Time
}

type Dependency struct {
	FromResource string
	ToResource   string
	Kind         string
	Source       string
}
