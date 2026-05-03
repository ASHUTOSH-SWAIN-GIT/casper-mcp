package terraformcode

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

type resourceBlockDetail struct {
	Type      string
	Name      string
	Arguments map[string]string
}

func ParseDir(path string) ([]graph.Resource, error) {
	dir := filepath.Clean(path)
	if !tfconfig.IsModuleDir(dir) {
		return nil, nil
	}

	module, diagnostics := tfconfig.LoadModule(dir)
	if diagnostics.HasErrors() {
		return nil, fmt.Errorf("parse terraform module %s: %s", dir, diagnostics.Error())
	}

	resourceDetails, err := parseResourceBlockDetails(dir)
	if err != nil {
		return nil, err
	}
	resourceDetailIndex := indexResourceDetails(resourceDetails)

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
			"managed_resources":  managedResources(module, resourceDetailIndex),
			"data_resources":     dataResources(module),
			"module_calls":       moduleCalls(module),
		},
		Tags:       map[string]any{},
		ModulePath: identifier,
		ManagedBy:  "terraform_code",
	}

	resources := []graph.Resource{resource}
	resources = append(resources, conventionResources(identifier, module, resourceDetails)...)
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

func managedResources(module *tfconfig.Module, detailIndex map[string]resourceBlockDetail) []map[string]any {
	keys := sortedKeys(module.ManagedResources)
	result := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		resource := module.ManagedResources[key]
		item := map[string]any{
			"type": resource.Type,
			"name": resource.Name,
		}
		if detail, ok := detailIndex[resourceDetailKey(resource.Type, resource.Name)]; ok {
			item["argument_names"] = sortedArgumentNames(detail.Arguments)
			item["argument_examples"] = copyArgumentMap(detail.Arguments)
		}
		result = append(result, item)
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

func conventionResources(modulePath string, module *tfconfig.Module, resourceDetails []resourceBlockDetail) []graph.Resource {
	byType := map[string][]*tfconfig.Resource{}
	for _, resource := range module.ManagedResources {
		byType[resource.Type] = append(byType[resource.Type], resource)
	}
	detailByType := groupResourceDetailsByType(resourceDetails)

	resourceTypes := sortedKeys(byType)
	result := make([]graph.Resource, 0, len(resourceTypes))
	variableNames := sortedKeys(module.Variables)

	for _, resourceType := range resourceTypes {
		managed := byType[resourceType]
		details := detailByType[resourceType]
		identifier := fmt.Sprintf("%s@%s", resourceType, modulePath)
		result = append(result, graph.Resource{
			ID:         conventionID(resourceType, modulePath),
			Source:     module.Path,
			Type:       "terraform_convention",
			Identifier: identifier,
			Attributes: map[string]any{
				"resource_type":       resourceType,
				"module_path":         modulePath,
				"resource_names":      managedResourceNames(managed),
				"common_inputs":       variableNames,
				"argument_names":      argumentNames(details),
				"common_arguments":    commonArguments(details),
				"argument_examples":   argumentExamples(details),
				"literal_arguments":   literalArguments(details),
				"reference_arguments": referenceArguments(details),
				"naming_signals":      filterSignals(variableNames, "name", "identifier", "prefix", "suffix"),
				"tag_signals":         filterSignals(variableNames, "tag"),
				"network_signals":     filterSignals(variableNames, "vpc", "subnet", "security_group", "sg"),
				"behavior_signals":    filterSignals(variableNames, "apply", "deletion", "public", "multi_az", "replica"),
				"provider_signals":    requiredProviders(module),
				"module_examples":     []string{modulePath},
				"supports_tagging":    hasAny(variableNames, "tag"),
				"supports_network":    hasAny(variableNames, "vpc", "subnet", "security_group", "sg"),
				"supports_naming":     hasAny(variableNames, "name", "identifier", "prefix", "suffix"),
				"supports_replica":    hasAny(variableNames, "replica"),
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

func parseResourceBlockDetails(dir string) ([]resourceBlockDetail, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, fmt.Errorf("glob terraform files in %s: %w", dir, err)
	}

	parser := hclparse.NewParser()
	var details []resourceBlockDetail

	for _, match := range matches {
		src, err := os.ReadFile(match)
		if err != nil {
			return nil, fmt.Errorf("read terraform file %s: %w", match, err)
		}
		file, diags := parser.ParseHCL(src, match)
		if diags.HasErrors() {
			return nil, fmt.Errorf("parse terraform file %s: %s", match, diags.Error())
		}

		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}

		for _, block := range body.Blocks {
			if block.Type != "resource" || len(block.Labels) < 2 {
				continue
			}

			arguments := map[string]string{}
			for name, attribute := range block.Body.Attributes {
				exprRange := attribute.Expr.Range()
				if exprRange.Start.Byte < 0 || exprRange.End.Byte > len(src) || exprRange.Start.Byte >= exprRange.End.Byte {
					continue
				}
				arguments[name] = strings.TrimSpace(string(src[exprRange.Start.Byte:exprRange.End.Byte]))
			}

			details = append(details, resourceBlockDetail{
				Type:      block.Labels[0],
				Name:      block.Labels[1],
				Arguments: arguments,
			})
		}
	}

	sort.Slice(details, func(i, j int) bool {
		if details[i].Type == details[j].Type {
			return details[i].Name < details[j].Name
		}
		return details[i].Type < details[j].Type
	})

	return details, nil
}

func indexResourceDetails(details []resourceBlockDetail) map[string]resourceBlockDetail {
	index := make(map[string]resourceBlockDetail, len(details))
	for _, detail := range details {
		index[resourceDetailKey(detail.Type, detail.Name)] = detail
	}
	return index
}

func groupResourceDetailsByType(details []resourceBlockDetail) map[string][]resourceBlockDetail {
	grouped := map[string][]resourceBlockDetail{}
	for _, detail := range details {
		grouped[detail.Type] = append(grouped[detail.Type], detail)
	}
	return grouped
}

func resourceDetailKey(resourceType, name string) string {
	return resourceType + "." + name
}

func sortedArgumentNames(arguments map[string]string) []string {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func copyArgumentMap(arguments map[string]string) map[string]string {
	result := make(map[string]string, len(arguments))
	for key, value := range arguments {
		result[key] = value
	}
	return result
}

func argumentNames(details []resourceBlockDetail) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, detail := range details {
		for name := range detail.Arguments {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func commonArguments(details []resourceBlockDetail) []string {
	if len(details) == 0 {
		return nil
	}

	counts := map[string]int{}
	for _, detail := range details {
		for name := range detail.Arguments {
			counts[name]++
		}
	}

	var names []string
	for name, count := range counts {
		if count == len(details) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func argumentExamples(details []resourceBlockDetail) map[string][]string {
	examples := map[string][]string{}
	for _, detail := range details {
		for name, expr := range detail.Arguments {
			examples[name] = appendUniqueString(examples[name], expr)
		}
	}
	sortExpressionMap(examples)
	return examples
}

func literalArguments(details []resourceBlockDetail) map[string][]string {
	literals := map[string][]string{}
	for _, detail := range details {
		for name, expr := range detail.Arguments {
			if isLiteralExpression(expr) {
				literals[name] = appendUniqueString(literals[name], expr)
			}
		}
	}
	sortExpressionMap(literals)
	return literals
}

func referenceArguments(details []resourceBlockDetail) map[string][]string {
	references := map[string][]string{}
	for _, detail := range details {
		for name, expr := range detail.Arguments {
			if isReferenceExpression(expr) {
				references[name] = appendUniqueString(references[name], expr)
			}
		}
	}
	sortExpressionMap(references)
	return references
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortExpressionMap(values map[string][]string) {
	for key := range values {
		sort.Strings(values[key])
	}
}

func isLiteralExpression(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"") {
		return true
	}
	switch expr {
	case "true", "false", "null":
		return true
	}
	for _, r := range expr {
		if !unicode.IsDigit(r) && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func isReferenceExpression(expr string) bool {
	expr = strings.TrimSpace(expr)
	return strings.Contains(expr, "var.") || strings.Contains(expr, ".")
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
