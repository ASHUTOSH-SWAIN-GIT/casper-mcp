package graph

// ImpactResult is the output of SimulateImpact — what would change in the
// graph if the proposed Terraform were applied.
type ImpactResult struct {
	Summary        string                       `json:"summary"`
	Created        []ResourceDiff               `json:"created,omitempty"`
	Modified       []ResourceDiff               `json:"modified,omitempty"`
	BlastRadius    []BlastItem                  `json:"blast_radius,omitempty"`
	Warnings             []string                     `json:"warnings,omitempty"`
	SimilarExamples      map[string][]SimilarExample  `json:"similar_examples,omitempty"`
	ReversibilityContext *ReversibilityContext         `json:"reversibility_context,omitempty"`
}

// SimilarExample is a concise view of an existing resource used as a reference.
type SimilarExample struct {
	Identifier string            `json:"identifier"`
	ModulePath string            `json:"module_path,omitempty"`
	Arguments  map[string]string `json:"arguments,omitempty"`
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

// ReversibilityContext holds per-resource context the agent uses to reason about
// whether each proposed change can be rolled back.
type ReversibilityContext struct {
	Resources []ResourceContext `json:"resources"`
}

// ResourceContext surfaces the raw facts about a single resource change so the
// agent can assess rollback risk without any hardcoded classification.
type ResourceContext struct {
	Identifier     string             `json:"identifier"`
	Type           string             `json:"type"`
	Operation      string             `json:"operation"`              // create | modify | destroy
	CurrentArgs    map[string]string  `json:"current_args,omitempty"` // state before change
	ProposedArgs   map[string]string  `json:"proposed_args,omitempty"`
	ChangedArgs    map[string]ArgDiff `json:"changed_args,omitempty"`
	AddedArgs      map[string]string  `json:"added_args,omitempty"`
	RemovedArgs    []string           `json:"removed_args,omitempty"`
	LifecycleFlags LifecycleFlags     `json:"lifecycle_flags"`
	Dependents     []string           `json:"dependents,omitempty"`      // identifiers that reference this resource
	DependsOn      []string           `json:"depends_on,omitempty"`      // identifiers this resource references
	RecentCommits  []GitCommit        `json:"recent_commits,omitempty"`  // last commits that touched this resource block
}

// LifecycleFlags captures Terraform lifecycle settings and resource-level
// protection flags that affect how safely a change can be applied or reversed.
type LifecycleFlags struct {
	PreventDestroy      bool `json:"prevent_destroy"`
	CreateBeforeDestroy bool `json:"create_before_destroy"`
	DeletionProtection  bool `json:"deletion_protection"` // from resource args, e.g. RDS
}

// GitCommit is a single entry from git log for a resource.
type GitCommit struct {
	Hash    string `json:"hash"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}
