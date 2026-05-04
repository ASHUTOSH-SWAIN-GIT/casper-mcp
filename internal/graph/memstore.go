package graph

import (
	"context"
	"sort"
	"strings"
)

// MemStore is a zero-dependency, in-memory implementation of Querier.
// Build one from a GraphSnapshot produced by ingest.Scan — no postgres needed.
type MemStore struct {
	resources []Resource
	deps      []Dependency
	byID      map[string]Resource
}

func NewMemStore(snapshot GraphSnapshot) *MemStore {
	byID := make(map[string]Resource, len(snapshot.Resources))
	for _, r := range snapshot.Resources {
		byID[r.ID] = r
	}
	return &MemStore{resources: snapshot.Resources, deps: snapshot.Dependencies, byID: byID}
}

func (m *MemStore) FindResources(_ context.Context, query string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		r Resource
		s int
	}
	var results []scored
	for _, r := range m.resources {
		s := findScore(r, q)
		if s > 0 {
			results = append(results, scored{r, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].s != results[j].s {
			return results[i].s > results[j].s
		}
		return results[i].r.Identifier < results[j].r.Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]Resource, len(results))
	for i, sr := range results {
		out[i] = sr.r
	}
	return out, nil
}

func (m *MemStore) GetDependencies(_ context.Context, resourceID string) ([]DependencyResult, error) {
	var results []DependencyResult
	for _, d := range m.deps {
		if d.FromResource == resourceID {
			if r, ok := m.byID[d.ToResource]; ok {
				results = append(results, DependencyResult{
					Direction: "dependency", Kind: d.Kind, Source: d.Source,
					Resource: r, Dependency: d,
				})
			}
		}
		if d.ToResource == resourceID {
			if r, ok := m.byID[d.FromResource]; ok {
				results = append(results, DependencyResult{
					Direction: "dependent", Kind: d.Kind, Source: d.Source,
					Resource: r, Dependency: d,
				})
			}
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Direction != results[j].Direction {
			return results[i].Direction < results[j].Direction
		}
		return results[i].Resource.Identifier < results[j].Resource.Identifier
	})
	return results, nil
}

func (m *MemStore) FindModules(_ context.Context, intent string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(intent))
	// Group by module path, return first representative resource per matching module
	seen := map[string]bool{}
	var results []Resource
	for _, r := range m.resources {
		mp := strings.ToLower(r.ModulePath)
		if mp == "" || seen[mp] {
			continue
		}
		if strings.Contains(mp, q) || strings.Contains(strings.ToLower(r.Type), q) {
			seen[mp] = true
			results = append(results, r)
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ModulePath < results[j].ModulePath
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (m *MemStore) FindConventions(_ context.Context, resourceType string, limit int) ([]Resource, error) {
	q := strings.ToLower(strings.TrimSpace(resourceType))
	type scored struct {
		r Resource
		s int
	}
	var results []scored
	for _, r := range m.resources {
		if strings.ToLower(r.Type) == q || strings.Contains(strings.ToLower(r.Type), q) {
			// More arguments = more informative as a convention example
			args, _ := r.Attributes["arguments"].(map[string]string)
			results = append(results, scored{r, len(args)})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].s != results[j].s {
			return results[i].s > results[j].s
		}
		return results[i].r.Identifier < results[j].r.Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]Resource, len(results))
	for i, sr := range results {
		out[i] = sr.r
	}
	return out, nil
}

func (m *MemStore) FindSimilar(_ context.Context, description string, limit int) ([]Resource, error) {
	tokens := expandTokens(tokenizeQuery(description))
	if len(tokens) == 0 {
		return nil, nil
	}
	type scored struct {
		r Resource
		s int
	}
	var results []scored
	for _, r := range m.resources {
		s := similarScore(r, tokens)
		if s > 0 {
			results = append(results, scored{r, s})
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].s != results[j].s {
			return results[i].s > results[j].s
		}
		return results[i].r.Identifier < results[j].r.Identifier
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	out := make([]Resource, len(results))
	for i, sr := range results {
		out[i] = sr.r
	}
	return out, nil
}

// similarScore scores a resource against a set of tokens.
// It checks type, identifier, and — most importantly — actual HCL argument
// keys and values so agents get real working examples to copy from.
func similarScore(r Resource, tokens []string) int {
	ident := strings.ToLower(r.Identifier)
	typ := strings.ToLower(r.Type)

	// Build a flat text blob from the actual HCL arguments
	argKeys, argVals := argumentTexts(r)

	score := 0
	for _, t := range tokens {
		if strings.Contains(typ, t) {
			score += 50
		}
		if strings.Contains(ident, t) {
			score += 40
		}
		// Argument key match (e.g. token "replica" matches key "replicate_source_db")
		for _, k := range argKeys {
			if strings.Contains(k, t) {
				score += 30
				break
			}
		}
		// Argument value match (e.g. token "postgres" matches value "postgres14")
		for _, v := range argVals {
			if strings.Contains(v, t) {
				score += 20
				break
			}
		}
		if strings.Contains(strings.ToLower(r.ModulePath), t) {
			score += 10
		}
	}
	return score
}

func argumentTexts(r Resource) (keys, vals []string) {
	args, ok := r.Attributes["arguments"].(map[string]string)
	if !ok {
		return
	}
	for k, v := range args {
		keys = append(keys, strings.ToLower(k))
		vals = append(vals, strings.ToLower(v))
	}
	return
}

// expandTokens adds Terraform-specific synonyms so natural language queries
// match the actual argument names and resource types in HCL.
func expandTokens(tokens []string) []string {
	synonyms := map[string][]string{
		"replica":      {"replicate_source_db", "replication_source_identifier", "replica"},
		"rds":          {"aws_db_instance", "aws_rds_cluster", "db_instance"},
		"database":     {"aws_db_instance", "aws_rds_cluster", "db_instance", "engine"},
		"postgres":     {"postgres", "postgresql"},
		"mysql":        {"mysql"},
		"subnet":       {"aws_subnet", "subnet_id", "subnet_ids"},
		"vpc":          {"aws_vpc", "vpc_id"},
		"sg":           {"aws_security_group", "security_group_ids"},
		"security":     {"aws_security_group", "security_group"},
		"lb":           {"aws_lb", "aws_alb", "load_balancer"},
		"loadbalancer": {"aws_lb", "aws_alb"},
		"iam":          {"aws_iam_role", "aws_iam_policy", "iam_role"},
		"role":         {"aws_iam_role", "iam_role_arn"},
		"bucket":       {"aws_s3_bucket", "bucket"},
		"s3":           {"aws_s3_bucket"},
		"eks":          {"aws_eks_cluster", "aws_eks_node_group"},
		"lambda":       {"aws_lambda_function"},
		"ec2":          {"aws_instance", "instance_type"},
		"instance":     {"aws_instance", "aws_db_instance", "instance_type", "instance_class"},
		"cache":        {"aws_elasticache_cluster", "aws_elasticache_replication_group"},
		"redis":        {"aws_elasticache_replication_group", "redis"},
		"queue":        {"aws_sqs_queue"},
		"sqs":          {"aws_sqs_queue"},
		"sns":          {"aws_sns_topic"},
		"route53":      {"aws_route53_record", "aws_route53_zone"},
		"dns":          {"aws_route53_record", "aws_route53_zone"},
		"cert":         {"aws_acm_certificate"},
		"tls":          {"aws_acm_certificate"},
		"kms":          {"aws_kms_key"},
		"secret":       {"aws_secretsmanager_secret"},
		"log":           {"aws_cloudwatch_log_group", "cloudwatch_logs"},
		"cloudwatch":    {"aws_cloudwatch_metric_alarm", "aws_cloudwatch_log_group"},
		"dynamodb":      {"aws_dynamodb_table"},
		"table":         {"aws_dynamodb_table"},
		"apigw":         {"aws_api_gateway_rest_api", "aws_apigatewayv2_api"},
		"apigateway":    {"aws_api_gateway_rest_api", "aws_apigatewayv2_api"},
		"api":           {"aws_api_gateway_rest_api", "aws_apigatewayv2_api"},
		"eventbridge":   {"aws_cloudwatch_event_rule", "aws_cloudwatch_event_target"},
		"events":        {"aws_cloudwatch_event_rule", "aws_cloudwatch_event_target"},
		"kinesis":       {"aws_kinesis_stream", "aws_kinesis_firehose_delivery_stream"},
		"stream":        {"aws_kinesis_stream", "aws_kinesis_firehose_delivery_stream"},
		"redshift":      {"aws_redshift_cluster"},
		"glue":          {"aws_glue_job", "aws_glue_crawler"},
		"cognito":       {"aws_cognito_user_pool", "aws_cognito_identity_pool"},
		"waf":           {"aws_wafv2_web_acl", "aws_waf_web_acl"},
		"sfn":           {"aws_sfn_state_machine"},
		"stepfunction":  {"aws_sfn_state_machine"},
		"statemachine":  {"aws_sfn_state_machine"},
	}

	seen := map[string]bool{}
	expanded := make([]string, 0, len(tokens)*2)
	for _, t := range tokens {
		if !seen[t] {
			seen[t] = true
			expanded = append(expanded, t)
		}
		for _, syn := range synonyms[t] {
			if !seen[syn] {
				seen[syn] = true
				expanded = append(expanded, syn)
			}
		}
	}
	return expanded
}

func findScore(r Resource, q string) int {
	if strings.ToLower(r.ID) == q {
		return 100
	}
	ident := strings.ToLower(r.Identifier)
	if ident == q {
		return 90
	}
	typ := strings.ToLower(r.Type)
	attrText := strings.ToLower(marshalForSearch(r.Attributes))
	tagText := strings.ToLower(marshalForSearch(r.Tags))
	score := 0
	if strings.Contains(ident, q) {
		score += 70
	}
	if typ == q {
		score += 60
	} else if strings.Contains(typ, q) {
		score += 40
	}
	if strings.Contains(attrText, q) {
		score += 20
	}
	if strings.Contains(tagText, q) {
		score += 10
	}
	return score
}
