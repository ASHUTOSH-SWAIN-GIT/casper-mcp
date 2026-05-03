package terraformstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

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
	IndexKey     any            `json:"index_key"`
	Attributes   map[string]any `json:"attributes"`
	Dependencies []string       `json:"dependencies"`
}

type Result struct {
	Resources    []graph.Resource
	Dependencies []graph.Dependency
}

func ParseFile(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read terraform state: %w", err)
	}

	var state rawState
	if err := json.Unmarshal(data, &state); err != nil {
		return Result{}, fmt.Errorf("parse terraform state: %w", err)
	}

	source := filepath.Clean(path)
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

func copyMap(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
