package rego

import (
	"context"
	"fmt"
	"sort"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/policy"
)

// Engine compiles a bundle of .rego files once and evaluates them against
// per-resource input on every Check call. The prepared query is reused —
// recompiling per call would be 100x slower per OPA's docs.
//
// Engine satisfies policy.Engine.
type Engine struct {
	query     rego.PreparedEvalQuery
	fileCount int
	files     []string // for logging / Source() context
}

// NewEngine compiles every supplied .rego file as a module and prepares
// the `data.policy.deny` query for fast repeated evaluation. Returns an
// error if any file fails to parse or compile — callers should fail-loud
// on bad policies rather than silently skip them.
func NewEngine(ctx context.Context, files []RegoFile) (*Engine, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no .rego files supplied")
	}

	// Default to RegoV0 because the existing Conftest / terraform-aws-conftest
	// policy ecosystem we want to be compatible with is overwhelmingly v0
	// syntax (`deny[msg] { ... }`). Policies written for v1 still work as
	// long as they declare `import rego.v1` at the top — OPA upgrades them
	// on parse. Forcing v1 here would break every existing customer library.
	opts := []func(*rego.Rego){
		rego.SetRegoVersion(ast.RegoV0),
		rego.Query("data.policy.deny"),
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		opts = append(opts, rego.Module(f.Path, string(f.Bytes)))
		paths = append(paths, f.Path)
	}

	prepared, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("prepare rego policies: %w", err)
	}

	sort.Strings(paths)
	return &Engine{query: prepared, fileCount: len(files), files: paths}, nil
}

// Check evaluates the prepared query against a JSON view of the resource.
// Each item in the resulting `deny` set becomes a Violation. Missing
// attributes in policies don't panic — Rego treats undefined values as
// non-matches, so unrelated policies simply produce 0 violations.
func (e *Engine) Check(resourceType, identifier string, args, tags map[string]string) []policy.Violation {
	if e == nil {
		return nil
	}

	input := map[string]any{
		"type":       resourceType,
		"identifier": identifier,
		"attributes": stringMapToAny(args),
		"tags":       stringMapToAny(tags),
	}

	// Use a context.Background here — Check is called from synchronous tool
	// handlers that don't pass a context through (yet). Evaluation is in-
	// memory and microseconds, so cancellation isn't load-bearing.
	results, err := e.query.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		// Surface eval errors as a single synthetic violation rather than
		// silently swallowing — the agent should see that policy enforcement
		// is broken, not assume the resource is clean.
		return []policy.Violation{{
			PolicyID: "rego-eval-error",
			Resource: identifier,
			Type:     resourceType,
			Message:  "rego policy evaluation failed",
			Details:  err.Error(),
		}}
	}

	var violations []policy.Violation
	for _, r := range results {
		for _, expr := range r.Expressions {
			// `data.policy.deny` evaluates to a list (set) of denial values.
			// Each entry is whatever the policy author put there — usually a
			// string message, sometimes an object with more structure.
			denies, ok := expr.Value.([]any)
			if !ok {
				continue
			}
			for _, deny := range denies {
				violations = append(violations, denyToViolation(deny, resourceType, identifier))
			}
		}
	}
	return violations
}

// Source identifies the engine backend on every violation. Lets agents tell
// rego-sourced denials from yaml-sourced ones when both are mixed in a
// single response.
func (e *Engine) Source() string { return "rego" }

// FileCount is for logging — main.go uses it in the startup line.
func (e *Engine) FileCount() int { return e.fileCount }

// Files returns the absolute paths of every .rego file the engine compiled.
func (e *Engine) Files() []string { return e.files }

// denyToViolation handles both shapes a `deny[msg]` rule can produce:
//   - a string: `deny[msg] { msg := "..." }`
//   - an object: `deny[v] { v := {"msg": "...", "policy_id": "..."} }`
func denyToViolation(deny any, resourceType, identifier string) policy.Violation {
	v := policy.Violation{
		Resource: identifier,
		Type:     resourceType,
	}
	switch d := deny.(type) {
	case string:
		v.Message = d
	case map[string]any:
		if s, ok := d["msg"].(string); ok {
			v.Message = s
		} else if s, ok := d["message"].(string); ok {
			v.Message = s
		}
		if s, ok := d["policy_id"].(string); ok {
			v.PolicyID = s
		}
		if s, ok := d["details"].(string); ok {
			v.Details = s
		}
	default:
		v.Message = fmt.Sprintf("%v", deny)
	}
	if v.Message == "" {
		v.Message = "policy denied"
	}
	return v
}

func stringMapToAny(m map[string]string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
