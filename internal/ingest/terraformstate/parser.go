package terraformstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

// redactedValue is what every sensitive attribute is replaced with before
// the parsed state reaches the graph. Keeps secrets from ever being
// surfaced through dump_graph, find_resource, or any other tool.
const redactedValue = "<redacted>"

type rawState struct {
	Resources []rawResource `json:"resources"`
}

type rawResource struct {
	Module    string        `json:"module"`
	Mode      string        `json:"mode"`
	Type      string        `json:"type"`
	Name      string        `json:"name"`
	Instances []rawInstance `json:"instances"`
}

type rawInstance struct {
	IndexKey            any            `json:"index_key"`
	Attributes          map[string]any `json:"attributes"`
	SensitiveAttributes []any          `json:"sensitive_attributes"`
	Dependencies        []string       `json:"dependencies"`
}

type Result struct {
	Resources    []graph.Resource
	Dependencies []graph.Dependency
}

// ParseFile reads a Terraform state file from disk and parses it. Thin
// wrapper around ParseBytes — pass it any local path.
func ParseFile(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read terraform state: %w", err)
	}
	return ParseBytes(data, filepath.Clean(path))
}

// ParseBytes parses Terraform state JSON. `source` is the logical identifier
// the state came from (a file path, an S3 URI, etc.) — used as the Source
// field on every returned graph.Resource.
func ParseBytes(data []byte, source string) (Result, error) {
	var state rawState
	if err := json.Unmarshal(data, &state); err != nil {
		return Result{}, fmt.Errorf("parse terraform state: %w", err)
	}

	resourcesByAddress := map[string]graph.Resource{}
	resources := collectResources(source, state.Resources)
	for _, resource := range resources {
		resourcesByAddress[resource.Identifier] = resource
	}

	var dependencies []graph.Dependency
	collectDependencies(source, state.Resources, resourcesByAddress, &dependencies)

	return Result{Resources: resources, Dependencies: dependencies}, nil
}

func collectResources(source string, stateResources []rawResource) []graph.Resource {
	var resources []graph.Resource
	for _, stateResource := range stateResources {
		if stateResource.Mode != "managed" {
			continue
		}

		for i, instance := range stateResource.Instances {
			address := resourceAddress(stateResource, instance, i)
			attributes := copyMap(instance.Attributes)
			redactSensitive(attributes, instance.SensitiveAttributes)
			resources = append(resources, graph.Resource{
				ID:         resourceID(source, address),
				Source:     source,
				Type:       stateResource.Type,
				Identifier: address,
				Attributes: attributes,
				Tags:       extractTags(attributes),
				ModulePath: stateResource.Module,
				ManagedBy:  "terraform",
			})
		}
	}

	return resources
}

func collectDependencies(source string, stateResources []rawResource, resourcesByAddress map[string]graph.Resource, dependencies *[]graph.Dependency) {
	for _, stateResource := range stateResources {
		if stateResource.Mode != "managed" {
			continue
		}
		for i, instance := range stateResource.Instances {
			address := resourceAddress(stateResource, instance, i)
			from, ok := resourcesByAddress[address]
			if !ok {
				continue
			}
			for _, dependencyAddress := range instance.Dependencies {
				to, ok := resourcesByAddress[dependencyAddress]
				if !ok {
					continue
				}
				*dependencies = append(*dependencies, graph.Dependency{
					FromResource: from.ID,
					ToResource:   to.ID,
					Kind:         "depends_on",
					Source:       source,
				})
			}
		}
	}
}

func resourceID(source, address string) string {
	sum := sha256.Sum256([]byte(source + ":" + address))
	return "tfstate_" + hex.EncodeToString(sum[:])[:24]
}

func resourceAddress(resource rawResource, instance rawInstance, index int) string {
	address := resource.Type + "." + resource.Name
	if resource.Module != "" {
		address = resource.Module + "." + address
	}

	if instance.IndexKey == nil && len(resource.Instances) == 1 {
		return address
	}

	switch value := instance.IndexKey.(type) {
	case string:
		return fmt.Sprintf("%s[%q]", address, value)
	case float64:
		return fmt.Sprintf("%s[%d]", address, int(value))
	default:
		return fmt.Sprintf("%s[%d]", address, index)
	}
}

func extractTags(attributes map[string]any) map[string]any {
	tags := map[string]any{}
	for _, key := range []string{"tags", "tags_all"} {
		raw, ok := attributes[key]
		if !ok {
			continue
		}
		rawTags, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for tagKey, value := range rawTags {
			tags[tagKey] = value
		}
	}
	return tags
}

// redactSensitive replaces values that Terraform has marked sensitive — both
// via the per-instance `sensitive_attributes` paths and a built-in heuristic
// for common secret-bearing keys — with a placeholder. Mutates attributes
// in place.
func redactSensitive(attributes map[string]any, sensitivePaths []any) {
	// Honor Terraform's own sensitive_attributes list. Each entry is a JSON
	// object describing a path (`steps`). For v1 we redact the *top-level*
	// key named in the first step — covers the overwhelming majority of
	// real state files. Nested-path redaction is a follow-up.
	for _, p := range sensitivePaths {
		obj, ok := p.(map[string]any)
		if !ok {
			continue
		}
		steps, ok := obj["steps"].([]any)
		if !ok || len(steps) == 0 {
			continue
		}
		step, ok := steps[0].(map[string]any)
		if !ok {
			continue
		}
		name, _ := step["attribute_name"].(string)
		if name == "" {
			continue
		}
		if _, present := attributes[name]; present {
			attributes[name] = redactedValue
		}
	}

	// Heuristic safety net for state files that don't ship a sensitive list
	// (older Terraform versions, hand-edited state, etc.).
	for k, v := range attributes {
		if isLikelySecretKey(k) {
			if _, alreadyRedacted := v.(string); alreadyRedacted && v == redactedValue {
				continue
			}
			attributes[k] = redactedValue
		}
	}
}

func isLikelySecretKey(key string) bool {
	k := strings.ToLower(key)
	// Exact matches first.
	switch k {
	case "password", "master_password", "secret", "private_key", "ssh_key", "auth_token":
		return true
	}
	// Suffix patterns: *_password, *_secret, *_secret_key, *_token, *_private_key
	for _, suffix := range []string{"_password", "_secret", "_secret_key", "_token", "_private_key"} {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
