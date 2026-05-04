package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Policy is a single rule set that applies to one resource type (or "*" for all).
type Policy struct {
	ID       string `yaml:"id"`
	Resource string `yaml:"resource"` // Terraform resource type or "*"
	Rules    []Rule `yaml:"rules"`
	Message  string `yaml:"message"`
}

// Rule describes a single constraint within a policy.
// Exactly one of Arg or Tag should be set.
type Rule struct {
	Arg          string `yaml:"arg,omitempty"`           // argument name to check
	Tag          string `yaml:"tag,omitempty"`           // tag key to check
	Required     bool   `yaml:"required,omitempty"`      // field/tag must be present and non-empty
	MustEqual    string `yaml:"must_equal,omitempty"`    // field must equal this value
	MustNotEqual string `yaml:"must_not_equal,omitempty"` // field must not equal this value
	MinValue     *int   `yaml:"min_value,omitempty"`     // numeric field must be >= this
}

type policyFile struct {
	Policies []Policy `yaml:"policies"`
}

// Load reads .casper/policies.yaml from dir.
// Returns an empty slice (not an error) if the file doesn't exist.
func Load(dir string) ([]Policy, error) {
	path := filepath.Join(dir, ".casper", "policies.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read policies: %w", err)
	}

	var pf policyFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("parse policies: %w", err)
	}
	return pf.Policies, nil
}

// Violation describes a single policy rule that was not satisfied.
type Violation struct {
	PolicyID string
	Resource string // identifier (type.name)
	Type     string // resource type
	Message  string // policy-level message
	Details  string // specific rule failure
}

// Check evaluates all policies against a single resource's proposed arguments and tags.
// resourceType is e.g. "aws_db_instance", identifier is e.g. "aws_db_instance.orders".
// proposedArgs is the flat map of HCL arguments; proposedTags is the resource tag map.
func Check(policies []Policy, resourceType, identifier string, proposedArgs map[string]string, proposedTags map[string]string) []Violation {
	var violations []Violation
	for _, p := range policies {
		if p.Resource != "*" && p.Resource != resourceType {
			continue
		}
		for _, rule := range p.Rules {
			if detail := evalRule(rule, proposedArgs, proposedTags); detail != "" {
				violations = append(violations, Violation{
					PolicyID: p.ID,
					Resource: identifier,
					Type:     resourceType,
					Message:  p.Message,
					Details:  detail,
				})
			}
		}
	}
	return violations
}

func evalRule(r Rule, args map[string]string, tags map[string]string) string {
	if r.Arg != "" {
		val, exists := args[r.Arg]
		if r.Required && (!exists || val == "") {
			return fmt.Sprintf("argument %q is required but not set", r.Arg)
		}
		if r.MustEqual != "" && val != r.MustEqual {
			if !exists {
				return fmt.Sprintf("argument %q must be %q (not set)", r.Arg, r.MustEqual)
			}
			return fmt.Sprintf("argument %q must be %q (got %q)", r.Arg, r.MustEqual, val)
		}
		if r.MustNotEqual != "" && val == r.MustNotEqual {
			return fmt.Sprintf("argument %q must not be %q", r.Arg, r.MustNotEqual)
		}
		if r.MinValue != nil {
			n, err := strconv.Atoi(val)
			if err != nil || n < *r.MinValue {
				return fmt.Sprintf("argument %q must be >= %d (got %q)", r.Arg, *r.MinValue, val)
			}
		}
	}
	if r.Tag != "" {
		val, exists := tags[r.Tag]
		if r.Required && (!exists || val == "") {
			return fmt.Sprintf("tag %q is required but not set", r.Tag)
		}
		if r.MustEqual != "" && val != r.MustEqual {
			if !exists {
				return fmt.Sprintf("tag %q must be %q (not set)", r.Tag, r.MustEqual)
			}
			return fmt.Sprintf("tag %q must be %q (got %q)", r.Tag, r.MustEqual, val)
		}
		if r.MustNotEqual != "" && val == r.MustNotEqual {
			return fmt.Sprintf("tag %q must not be %q", r.Tag, r.MustNotEqual)
		}
	}
	return ""
}
