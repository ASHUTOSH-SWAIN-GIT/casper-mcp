package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

// WorkflowRule is one entry in the workflow_rules YAML list.
type WorkflowRule struct {
	ID            string        `yaml:"id"`
	When          RuleCondition `yaml:"when"`
	Decision      string        `yaml:"decision"`                  // allow|require_approval|require_security_review|block
	RequiredSteps []string      `yaml:"required_steps,omitempty"` // overrides defaults when set
	Reason        string        `yaml:"reason,omitempty"`
}

// RuleCondition holds the match predicates for a workflow rule.
// All non-empty fields must match for the rule to fire.
type RuleCondition struct {
	Env                string     `yaml:"env,omitempty"`
	Operation          StringList `yaml:"operation,omitempty"` // string or []string
	ResourceTypeFamily string     `yaml:"resource_type_family,omitempty"`
	ResourceType       string     `yaml:"resource_type,omitempty"`
}

// StringList unmarshals a YAML value that is either a single string or a list.
type StringList []string

func (s *StringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		*s = StringList{value.Value}
		return nil
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*s = items
		return nil
	}
	return fmt.Errorf("workflow: cannot unmarshal %v as string or list", value.Tag)
}

type workflowFile struct {
	WorkflowRules []WorkflowRule `yaml:"workflow_rules"`
}

// Load reads workflow_rules from .casper/policies.yaml in dir.
// Returns an empty slice (not an error) when the file or section is absent.
func Load(dir string) ([]WorkflowRule, error) {
	path := filepath.Join(dir, ".casper", "policies.yaml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read workflow rules: %w", err)
	}

	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow rules: %w", err)
	}
	return wf.WorkflowRules, nil
}

// ResourceInput is the context for a single resource being evaluated.
type ResourceInput struct {
	Identifier string
	Type       string
	Operation  string // create | modify | destroy
	Tags       map[string]string
	ModulePath string
	Source     string
}

// Evaluate applies all rules to each resource and returns the aggregated decision.
// First match per resource wins. The strictest decision across all resources wins overall.
func Evaluate(rules []WorkflowRule, resources []ResourceInput) *graph.WorkflowDecision {
	if len(rules) == 0 {
		return nil
	}

	type resourceResult struct {
		rule     WorkflowRule
		reason   string
		decision int // 0=allow 1=require_approval 2=require_security_review 3=block
	}

	decisionRank := map[string]int{
		"allow":                   0,
		"require_approval":        1,
		"require_security_review": 2,
		"block":                   3,
	}

	best := -1
	var matchedRules []graph.MatchedRule
	seen := map[string]bool{}

	for _, res := range resources {
		env := detectEnv(res)
		family := resourceFamily(res.Type)

		for _, rule := range rules {
			if !matchesCondition(rule.When, env, res.Operation, family, res.Type) {
				continue
			}
			rank := decisionRank[rule.Decision]
			if rank > best {
				best = rank
			}
			if !seen[rule.ID] {
				seen[rule.ID] = true
				reason := buildReason(rule.When, env, res.Operation, family)
				matchedRules = append(matchedRules, graph.MatchedRule{
					ID:     rule.ID,
					Reason: reason,
				})
			}
			break // first match wins per resource
		}
	}

	if best < 0 {
		// No rules matched — explicit allow with no steps
		return &graph.WorkflowDecision{
			Decision:      "allow",
			RequiredSteps: []string{},
		}
	}

	decisionName := []string{"allow", "require_approval", "require_security_review", "block"}[best]

	// Collect required_steps from the matched rules (first occurrence of each decision wins)
	var steps []string
	seenStep := map[string]bool{}
	for _, r := range matchedRules {
		for _, rule := range rules {
			if rule.ID != r.ID {
				continue
			}
			src := rule.RequiredSteps
			if len(src) == 0 {
				src = defaultSteps(rule.Decision)
			}
			for _, s := range src {
				if !seenStep[s] {
					seenStep[s] = true
					steps = append(steps, s)
				}
			}
		}
	}
	if steps == nil {
		steps = []string{}
	}

	wd := &graph.WorkflowDecision{
		Decision:      decisionName,
		MatchedRules:  matchedRules,
		RequiredSteps: steps,
		Blocked:       decisionName == "block",
	}
	if wd.Blocked && len(matchedRules) > 0 {
		wd.BlockedReason = matchedRules[0].Reason
	}
	return wd
}

func matchesCondition(cond RuleCondition, env, operation, family, resourceType string) bool {
	if cond.Env != "" && env != cond.Env {
		return false
	}
	if len(cond.Operation) > 0 && !slices.Contains([]string(cond.Operation), operation) {
		return false
	}
	if cond.ResourceTypeFamily != "" && family != cond.ResourceTypeFamily {
		return false
	}
	if cond.ResourceType != "" && resourceType != cond.ResourceType {
		return false
	}
	return true
}

func buildReason(cond RuleCondition, env, operation, family string) string {
	var parts []string
	if cond.Env != "" {
		parts = append(parts, "env="+env)
	}
	if operation != "" {
		parts = append(parts, "operation="+operation)
	}
	if cond.ResourceTypeFamily != "" {
		parts = append(parts, "family="+family)
	}
	if cond.ResourceType != "" {
		parts = append(parts, "type="+cond.ResourceType)
	}
	return strings.Join(parts, ", ")
}

func defaultSteps(decision string) []string {
	switch decision {
	case "require_approval":
		return []string{"get_team_lead_approval"}
	case "require_security_review":
		return []string{"security_review", "get_team_lead_approval"}
	default:
		return []string{}
	}
}

// detectEnv infers the environment from tags, module path, source path, and identifier.
// Strictness order: prod > staging > dev. Fails closed to "prod" if nothing detected.
func detectEnv(res ResourceInput) string {
	best := ""
	rank := map[string]int{"dev": 1, "staging": 2, "prod": 3}

	check := func(s string) {
		s = normalize(s)
		if r, ok := rank[s]; ok {
			if rank[best] < r {
				best = s
			}
		}
	}

	for _, key := range []string{"env", "environment"} {
		if v, ok := res.Tags[key]; ok {
			check(v)
		}
	}
	for _, path := range []string{res.ModulePath, res.Source, res.Identifier} {
		for _, word := range strings.FieldsFunc(path, func(r rune) bool {
			return r == '/' || r == '\\' || r == '.' || r == '_' || r == '-'
		}) {
			check(word)
		}
	}

	if best == "" {
		return "prod" // fail closed
	}
	return best
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "production":
		return "prod"
	case "stage":
		return "staging"
	case "development":
		return "dev"
	}
	return s
}

var familyMap = map[string]string{
	"aws_db_instance":                      "database",
	"aws_rds_cluster":                      "database",
	"aws_rds_cluster_instance":             "database",
	"aws_dynamodb_table":                   "database",
	"aws_elasticache_cluster":              "database",
	"aws_elasticache_replication_group":    "database",
	"aws_redshift_cluster":                 "database",
	"aws_iam_role":                         "iam",
	"aws_iam_policy":                       "iam",
	"aws_iam_user":                         "iam",
	"aws_iam_role_policy_attachment":       "iam",
	"aws_security_group":                   "network_security",
	"aws_vpc":                              "network_security",
	"aws_network_acl":                      "network_security",
	"aws_wafv2_web_acl":                    "network_security",
	"aws_instance":                         "compute",
	"aws_eks_cluster":                      "compute",
	"aws_eks_node_group":                   "compute",
	"aws_lambda_function":                  "compute",
	"aws_ecs_service":                      "compute",
	"aws_s3_bucket":                        "storage",
	"aws_ebs_volume":                       "storage",
	"aws_efs_file_system":                  "storage",
}

func resourceFamily(resourceType string) string {
	if f, ok := familyMap[resourceType]; ok {
		return f
	}
	return ""
}
