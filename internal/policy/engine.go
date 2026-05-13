package policy

// Engine evaluates org policies against a single resource. Implementations:
//   - YAMLEngine wraps the built-in YAML rule format (.casper/policies.yaml).
//   - rego.Engine (separate package) wraps the OPA evaluator.
//
// Both call sites in the ingest + MCP layer take Engine by interface, so
// either backend produces violations of the same shape.
type Engine interface {
	// Check evaluates every loaded policy against the given resource and
	// returns the violations. Returns nil/empty when nothing fires.
	Check(resourceType, identifier string, args, tags map[string]string) []Violation

	// Source identifies which backend produced these violations.
	// "yaml" or "rego" — surfaced on every Violation for traceability.
	Source() string
}

// YAMLEngine wraps the existing Check function so the YAML rule path
// satisfies the Engine interface unchanged. The legacy Check is still
// the source of truth — this is just the adapter.
type YAMLEngine struct {
	policies []Policy
}

// NewYAMLEngine returns an Engine that evaluates the given YAML policies.
// Pass nil/empty to disable yaml policy enforcement entirely.
func NewYAMLEngine(policies []Policy) *YAMLEngine {
	return &YAMLEngine{policies: policies}
}

// Policies returns the loaded policies (read-only). Useful for tests and
// for surfacing "this many policies are active" in startup logs.
func (e *YAMLEngine) Policies() []Policy { return e.policies }

func (e *YAMLEngine) Check(resourceType, identifier string, args, tags map[string]string) []Violation {
	if e == nil || len(e.policies) == 0 {
		return nil
	}
	return Check(e.policies, resourceType, identifier, args, tags)
}

func (e *YAMLEngine) Source() string { return "yaml" }
