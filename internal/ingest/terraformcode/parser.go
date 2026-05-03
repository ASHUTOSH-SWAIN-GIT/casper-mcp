package terraformcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-config-inspect/tfconfig"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

func ParseDir(path string) ([]graph.Resource, error) {
	dir := filepath.Clean(path)
	if !tfconfig.IsModuleDir(dir) {
		return nil, nil
	}

	module, diagnostics := tfconfig.LoadModule(dir)
	if diagnostics.HasErrors() {
		return nil, fmt.Errorf("parse terraform module %s: %s", dir, diagnostics.Error())
	}

	identifier := moduleIdentifier(dir)
	resource := graph.Resource{
		ID:         moduleID(identifier),
		Source:     dir,
		Type:       "terraform_module",
		Identifier: identifier,
		Attributes: map[string]any{
			"path":               identifier,
			"variables":          variables(module),
			"outputs":            outputs(module),
			"required_providers": requiredProviders(module),
			"managed_resources":  managedResources(module),
			"data_resources":     dataResources(module),
			"module_calls":       moduleCalls(module),
		},
		Tags:       map[string]any{},
		ModulePath: identifier,
		ManagedBy:  "terraform_code",
	}

	resources := []graph.Resource{resource}
	resources = append(resources, conventionResources(identifier, module)...)
	return resources, nil
}

func moduleIdentifier(path string) string {
	rel, err := filepath.Rel(".", path)
	if err != nil {
		return path
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func moduleID(identifier string) string {
	sum := sha256.Sum256([]byte("terraform_code:" + identifier))
	return "tfcode_" + hex.EncodeToString(sum[:])[:24]
}

func variables(module *tfconfig.Module) []map[string]any {
	names := sortedKeys(module.Variables)
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		variable := module.Variables[name]
		result = append(result, map[string]any{
			"name":        variable.Name,
			"type":        variable.Type,
			"description": variable.Description,
			"required":    variable.Required,
			"sensitive":   variable.Sensitive,
		})
	}
	return result
}

func outputs(module *tfconfig.Module) []map[string]any {
	names := sortedKeys(module.Outputs)
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		output := module.Outputs[name]
		result = append(result, map[string]any{
			"name":        output.Name,
			"description": output.Description,
			"sensitive":   output.Sensitive,
			"type":        output.Type,
		})
	}
	return result
}

func requiredProviders(module *tfconfig.Module) []map[string]any {
	names := sortedKeys(module.RequiredProviders)
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		provider := module.RequiredProviders[name]
		result = append(result, map[string]any{
			"name":                name,
			"source":              provider.Source,
			"version_constraints": provider.VersionConstraints,
		})
	}
	return result
}

func managedResources(module *tfconfig.Module) []map[string]any {
	keys := sortedKeys(module.ManagedResources)
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		resource := module.ManagedResources[key]
		result = append(result, map[string]any{
			"type": resource.Type,
			"name": resource.Name,
		})
	}
	return result
}

func dataResources(module *tfconfig.Module) []map[string]any {
	keys := sortedKeys(module.DataResources)
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		resource := module.DataResources[key]
		result = append(result, map[string]any{
			"type": resource.Type,
			"name": resource.Name,
		})
	}
	return result
}

func moduleCalls(module *tfconfig.Module) []map[string]any {
	names := sortedKeys(module.ModuleCalls)
	result := make([]map[string]any, 0, len(names))
	for _, name := range names {
		call := module.ModuleCalls[name]
		result = append(result, map[string]any{
			"name":    call.Name,
			"source":  call.Source,
			"version": call.Version,
		})
	}
	return result
}

func conventionResources(modulePath string, module *tfconfig.Module) []graph.Resource {
	byType := map[string][]*tfconfig.Resource{}
	for _, resource := range module.ManagedResources {
		byType[resource.Type] = append(byType[resource.Type], resource)
	}

	resourceTypes := sortedKeys(byType)
	result := make([]graph.Resource, 0, len(resourceTypes))
	variableNames := sortedKeys(module.Variables)

	for _, resourceType := range resourceTypes {
		managed := byType[resourceType]
		identifier := fmt.Sprintf("%s@%s", resourceType, modulePath)
		result = append(result, graph.Resource{
			ID:         conventionID(resourceType, modulePath),
			Source:     module.Path,
			Type:       "terraform_convention",
			Identifier: identifier,
			Attributes: map[string]any{
				"resource_type":    resourceType,
				"module_path":      modulePath,
				"resource_names":   managedResourceNames(managed),
				"common_inputs":    variableNames,
				"naming_signals":   filterSignals(variableNames, "name", "identifier", "prefix", "suffix"),
				"tag_signals":      filterSignals(variableNames, "tag"),
				"network_signals":  filterSignals(variableNames, "vpc", "subnet", "security_group", "sg"),
				"behavior_signals": filterSignals(variableNames, "apply", "deletion", "public", "multi_az", "replica"),
				"provider_signals": requiredProviders(module),
				"module_examples":  []string{modulePath},
				"supports_tagging": hasAny(variableNames, "tag"),
				"supports_network": hasAny(variableNames, "vpc", "subnet", "security_group", "sg"),
				"supports_naming":  hasAny(variableNames, "name", "identifier", "prefix", "suffix"),
				"supports_replica": hasAny(variableNames, "replica"),
			},
			Tags:       map[string]any{},
			ModulePath: modulePath,
			ManagedBy:  "terraform_code",
		})
	}

	return result
}

func conventionID(resourceType, modulePath string) string {
	sum := sha256.Sum256([]byte("terraform_convention:" + resourceType + ":" + modulePath))
	return "tfconv_" + hex.EncodeToString(sum[:])[:24]
}

func managedResourceNames(resources []*tfconfig.Resource) []string {
	names := make([]string, 0, len(resources))
	for _, resource := range resources {
		names = append(names, resource.Name)
	}
	sort.Strings(names)
	return names
}

func filterSignals(values []string, needles ...string) []string {
	var result []string
	for _, value := range values {
		if containsAny(value, needles...) {
			result = append(result, value)
		}
	}
	return result
}

func hasAny(values []string, needles ...string) bool {
	for _, value := range values {
		if containsAny(value, needles...) {
			return true
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	lower := strings.ToLower(value)
	for _, needle := range needles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
