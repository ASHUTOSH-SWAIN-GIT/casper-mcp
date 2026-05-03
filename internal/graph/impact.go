package graph

// ImpactResult is the output of SimulateImpact — what would change in the
// graph if the proposed Terraform were applied.
type ImpactResult struct {
	Summary     string         `json:"summary"`
	Created     []ResourceDiff `json:"created"`
	Modified    []ResourceDiff `json:"modified"`
	BlastRadius []BlastItem    `json:"blast_radius"`
}

// ResourceDiff describes a single resource that would be created or modified.
type ResourceDiff struct {
	Identifier string             `json:"identifier"`
	Type       string             `json:"type"`
	Arguments  map[string]string  `json:"arguments,omitempty"`  // created: all args
	Added      map[string]string  `json:"added,omitempty"`       // modified: new args
	Changed    map[string]ArgDiff `json:"changed,omitempty"`     // modified: changed args
	Removed    []string           `json:"removed,omitempty"`     // modified: removed args
}

// ArgDiff shows the before and after value of a changed argument.
type ArgDiff struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// BlastItem is a resource in the current graph that is affected by the change.
type BlastItem struct {
	Identifier   string `json:"identifier"`
	Type         string `json:"type"`
	Relationship string `json:"relationship"`
}
