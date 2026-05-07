//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

var (
	binaryPath  string
	fixtureDir  string
	repoRoot    string
)

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e setup failed:", err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func setup() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	repoRoot = filepath.Clean(filepath.Join(wd, "..", ".."))
	fixtureDir = filepath.Join(wd, "testdata")

	if p := os.Getenv("CASPER_MCP_BIN"); p != "" {
		binaryPath = p
		return nil
	}

	tmp, err := os.MkdirTemp("", "casper-e2e-")
	if err != nil {
		return err
	}
	binaryPath = filepath.Join(tmp, "casper-mcp")

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/casper-mcp")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build casper-mcp: %w", err)
	}
	return nil
}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.NewStdioMCPClient(binaryPath, nil, "serve", "--dir", fixtureDir)
	if err != nil {
		t.Fatalf("start client: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "casper-e2e", Version: "0.0.0"}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	return c
}

func callTool(t *testing.T, c *client.Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := mcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := c.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("tool %s returned error: %s", name, textContent(res))
	}
	return res
}

// textContent concatenates all TextContent blocks from a tool result.
func textContent(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// unmarshalJSON parses the text content of a tool response as JSON into v.
func unmarshalJSON(t *testing.T, res *mcp.CallToolResult, v any) {
	t.Helper()
	txt := textContent(res)
	if err := json.Unmarshal([]byte(txt), v); err != nil {
		t.Fatalf("unmarshal tool response: %v\nbody: %s", err, txt)
	}
}

// --- Tests ---

func TestE2E_Initialize(t *testing.T) {
	c := newClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := []string{"get_context", "find_resource", "find_similar", "get_module_for", "get_conventions", "get_dependencies", "simulate_impact", "dump_graph"}
	got := map[string]bool{}
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected tool %q in tool list", name)
		}
	}
}

func TestE2E_FindResource_ByName(t *testing.T) {
	c := newClient(t)
	res := callTool(t, c, "find_resource", map[string]any{"query": "orders_primary"})

	var payload struct {
		Matches []map[string]any `json:"matches"`
	}
	unmarshalJSON(t, res, &payload)
	if len(payload.Matches) == 0 {
		t.Fatal("expected at least one resource matching orders_primary")
	}
	var found bool
	for _, r := range payload.Matches {
		if r["Identifier"] == "aws_db_instance.orders_primary" {
			found = true
		}
	}
	if !found {
		t.Errorf("aws_db_instance.orders_primary not in results: %s", textContent(res))
	}
}

func TestE2E_FindResource_ByType(t *testing.T) {
	c := newClient(t)
	res := callTool(t, c, "find_resource", map[string]any{"type": "aws_db_instance"})

	var payload struct {
		Matches []map[string]any `json:"matches"`
	}
	unmarshalJSON(t, res, &payload)

	identifiers := map[string]bool{}
	for _, r := range payload.Matches {
		if id, ok := r["Identifier"].(string); ok {
			identifiers[id] = true
		}
	}
	for _, want := range []string{"aws_db_instance.orders_primary", "aws_db_instance.orders_replica"} {
		if !identifiers[want] {
			t.Errorf("expected %q in find_resource(type=aws_db_instance) results, got %v", want, identifiers)
		}
	}
}

func TestE2E_GetContext(t *testing.T) {
	c := newClient(t)
	res := callTool(t, c, "get_context", map[string]any{"intent": "postgres read replica"})

	var sections map[string]json.RawMessage
	unmarshalJSON(t, res, &sections)

	// At least one of the lookup sections must fire. similar_examples is the most
	// reliable for natural-language intents because it tokenises + expands synonyms.
	if _, ok := sections["similar_examples"]; !ok {
		t.Errorf("expected similar_examples in get_context response, keys: %v", mapKeys(sections))
	}

	// The replica should appear somewhere — validates the "replica" → replicate_source_db synonym.
	body := textContent(res)
	if !strings.Contains(body, "aws_db_instance.orders_replica") {
		t.Errorf("expected orders_replica in get_context response, got:\n%s", body)
	}
}

func TestE2E_GetDependencies_Upstream(t *testing.T) {
	c := newClient(t)

	// Look up the SG id first.
	res := callTool(t, c, "find_resource", map[string]any{"query": "aws_security_group.app"})
	var payload struct {
		Matches []map[string]any `json:"matches"`
	}
	unmarshalJSON(t, res, &payload)
	var sgID string
	for _, r := range payload.Matches {
		if r["Identifier"] == "aws_security_group.app" {
			sgID, _ = r["ID"].(string)
		}
	}
	if sgID == "" {
		t.Fatalf("could not find aws_security_group.app id, body: %s", textContent(res))
	}

	res = callTool(t, c, "get_dependencies", map[string]any{"resource_id": sgID})
	body := textContent(res)
	if !strings.Contains(body, "aws_vpc.main") {
		t.Errorf("expected aws_vpc.main in dependencies of SG, got:\n%s", body)
	}
}

func TestE2E_SimulateImpact_Create(t *testing.T) {
	c := newClient(t)
	hcl := `
resource "aws_security_group" "new_sg" {
  name        = "new-sg"
  description = "another sg"
  vpc_id      = aws_vpc.main.id
}`
	res := callTool(t, c, "simulate_impact", map[string]any{"code": hcl})

	var result struct {
		Created     []map[string]any `json:"created"`
		Modified    []map[string]any `json:"modified"`
		BlastRadius []map[string]any `json:"blast_radius"`
	}
	unmarshalJSON(t, res, &result)

	var createdNew bool
	for _, r := range result.Created {
		if r["identifier"] == "aws_security_group.new_sg" {
			createdNew = true
		}
	}
	if !createdNew {
		t.Errorf("expected aws_security_group.new_sg in created, got: %s", textContent(res))
	}

	var blastVPC bool
	for _, r := range result.BlastRadius {
		if r["identifier"] == "aws_vpc.main" {
			blastVPC = true
		}
	}
	if !blastVPC {
		t.Errorf("expected aws_vpc.main in blast_radius (upstream of new SG), got: %s", textContent(res))
	}
}

func TestE2E_SimulateImpact_BrokenRef(t *testing.T) {
	c := newClient(t)
	hcl := `
resource "aws_security_group" "orphan" {
  name        = "orphan"
  description = "broken"
  vpc_id      = aws_vpc.nonexistent.id
}`
	res := callTool(t, c, "simulate_impact", map[string]any{"code": hcl})
	body := textContent(res)
	if !strings.Contains(body, "aws_vpc.nonexistent") {
		t.Errorf("expected warning mentioning aws_vpc.nonexistent, got:\n%s", body)
	}
}

func TestE2E_DumpGraph(t *testing.T) {
	c := newClient(t)
	res := callTool(t, c, "dump_graph", map[string]any{})

	var snap struct {
		ResourceCount int              `json:"resource_count"`
		DepCount      int              `json:"dep_count"`
		Resources     []map[string]any `json:"resources"`
	}
	unmarshalJSON(t, res, &snap)

	// 7 managed resources in the fixture + 1 terraform_module aggregate node = 8.
	if snap.ResourceCount < 7 {
		t.Errorf("expected at least 7 resources in graph, got %d", snap.ResourceCount)
	}
	if snap.DepCount < 4 {
		// expected edges: subnet->vpc (x2), sg->vpc, db_subnet_group->subnet (x2), db->sg, db->subnet_group, replica->primary
		t.Errorf("expected at least 4 dependency edges, got %d", snap.DepCount)
	}
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
