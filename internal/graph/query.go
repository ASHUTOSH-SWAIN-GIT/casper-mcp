package graph

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// TokenizeQuery splits a free-text query into lowercase alphanumeric tokens.
// Used by MemStore for scoring; exposed so tests in tests/graph can exercise
// it directly.
func TokenizeQuery(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	seen := map[string]struct{}{}
	var tokens []string
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		tokens = append(tokens, part)
	}
	return tokens
}

// SimilarityScore ranks a resource against a query and its tokens. Higher is
// a better match. Used by MemStore.FindSimilar.
func SimilarityScore(resource Resource, query string, tokens []string) int {
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	lowerIdentifier := strings.ToLower(resource.Identifier)
	lowerType := strings.ToLower(resource.Type)
	attributesText := strings.ToLower(marshalForSearch(resource.Attributes))
	tagsText := strings.ToLower(marshalForSearch(resource.Tags))
	modulePath := strings.ToLower(resource.ModulePath)

	score := 0
	switch {
	case lowerIdentifier == lowerQuery:
		score += 120
	case strings.Contains(lowerIdentifier, lowerQuery) && lowerQuery != "":
		score += 70
	}

	if valueEquals(resource.Attributes, lowerQuery, "id", "identifier", "name", "path") {
		score += 90
	}

	if strings.Contains(lowerType, lowerQuery) && lowerQuery != "" {
		score += 60
	}
	if strings.Contains(attributesText, lowerQuery) && lowerQuery != "" {
		score += 40
	}
	if strings.Contains(tagsText, lowerQuery) && lowerQuery != "" {
		score += 20
	}

	for _, token := range tokens {
		if strings.Contains(lowerIdentifier, token) {
			score += 20
		}
		if strings.Contains(lowerType, token) {
			score += 16
		}
		if strings.Contains(modulePath, token) {
			score += 12
		}
		if strings.Contains(attributesText, token) {
			score += 10
		}
		if strings.Contains(tagsText, token) {
			score += 4
		}
	}

	if resource.Type == "terraform_module" {
		score += 8
	}

	return score
}

func valueEquals(values map[string]any, query string, keys ...string) bool {
	for _, key := range keys {
		raw, ok := values[key]
		if !ok {
			continue
		}
		if strings.EqualFold(fmt.Sprint(raw), query) {
			return true
		}
	}
	return false
}

func marshalForSearch(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
