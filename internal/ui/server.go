package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

type Server struct {
	store *graph.Store
}

func NewServer(store *graph.Store) *Server {
	return &Server{store: store}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/graph", s.handleGraph)
	return mux
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTemplate.Execute(w, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshot, err := s.store.LoadGraphSnapshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response := buildGraphResponse(snapshot)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type graphResponse struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

type graphNode struct {
	ID           string         `json:"id"`
	Label        string         `json:"label"`
	Type         string         `json:"type"`
	ManagedBy    string         `json:"managed_by"`
	ModulePath   string         `json:"module_path"`
	Source       string         `json:"source"`
	Tags         map[string]any `json:"tags"`
	Attributes   map[string]any `json:"attributes"`
	Summary      string         `json:"summary"`
	SearchText   string         `json:"search_text"`
	LastSeenUnix int64          `json:"last_seen_unix"`
}

type graphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
}

func buildGraphResponse(snapshot graph.GraphSnapshot) graphResponse {
	response := graphResponse{
		Nodes: make([]graphNode, 0, len(snapshot.Resources)),
		Edges: make([]graphEdge, 0, len(snapshot.Dependencies)),
	}

	for _, resource := range snapshot.Resources {
		response.Nodes = append(response.Nodes, graphNode{
			ID:           resource.ID,
			Label:        resource.Identifier,
			Type:         resource.Type,
			ManagedBy:    resource.ManagedBy,
			ModulePath:   resource.ModulePath,
			Source:       resource.Source,
			Tags:         resource.Tags,
			Attributes:   resource.Attributes,
			Summary:      summarizeResource(resource),
			SearchText:   searchText(resource),
			LastSeenUnix: resource.LastSeen.Unix(),
		})
	}

	for _, dependency := range snapshot.Dependencies {
		response.Edges = append(response.Edges, graphEdge{
			From:   dependency.FromResource,
			To:     dependency.ToResource,
			Kind:   dependency.Kind,
			Source: dependency.Source,
		})
	}

	return response
}

func summarizeResource(resource graph.Resource) string {
	switch resource.Type {
	case "terraform_module":
		managed := asSlice(resource.Attributes["managed_resources"])
		return fmt.Sprintf("%d managed resources", len(managed))
	case "terraform_convention":
		commonArgs := asStringSlice(resource.Attributes["common_arguments"])
		if len(commonArgs) == 0 {
			return "Convention summary"
		}
		return "Common args: " + strings.Join(commonArgs, ", ")
	default:
		parts := []string{}
		for _, key := range []string{"id", "identifier", "engine", "instance_class", "name"} {
			if value, ok := resource.Attributes[key]; ok {
				parts = append(parts, fmt.Sprintf("%s=%v", key, value))
			}
		}
		if len(parts) == 0 {
			return resource.Type
		}
		return strings.Join(parts, " | ")
	}
}

func searchText(resource graph.Resource) string {
	b := strings.Builder{}
	b.WriteString(strings.ToLower(resource.Identifier))
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(resource.Type))
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(resource.ModulePath))
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(marshalCompact(resource.Attributes)))
	b.WriteByte(' ')
	b.WriteString(strings.ToLower(marshalCompact(resource.Tags)))
	return b.String()
}

func marshalCompact(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func asSlice(value any) []any {
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	return values
}

func asStringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, item := range values {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

var indexTemplate = template.Must(template.New("graph").Parse(`<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Casper Graph</title>
    <style>
      :root {
        --bg: #07111f;
        --panel: rgba(7, 17, 31, 0.78);
        --panel-strong: rgba(10, 23, 40, 0.96);
        --line: rgba(148, 163, 184, 0.18);
        --text: #e5edf8;
        --muted: #8da2bf;
        --accent: #67e8f9;
        --accent-2: #f59e0b;
        --resource: #7dd3fc;
        --module: #86efac;
        --convention: #fcd34d;
        --edge: rgba(148, 163, 184, 0.28);
        --edge-active: rgba(103, 232, 249, 0.9);
      }

      * { box-sizing: border-box; }
      html, body { margin: 0; height: 100%; font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background:
        radial-gradient(circle at top left, rgba(34, 211, 238, 0.14), transparent 28%),
        radial-gradient(circle at bottom right, rgba(245, 158, 11, 0.12), transparent 24%),
        linear-gradient(180deg, #020817 0%, #07111f 48%, #030712 100%);
        color: var(--text);
      }

      .shell {
        display: grid;
        grid-template-columns: 320px minmax(0, 1fr) 360px;
        height: 100vh;
      }

      .panel {
        background: var(--panel);
        backdrop-filter: blur(20px);
        border-right: 1px solid var(--line);
        min-height: 0;
      }

      .sidebar, .inspector {
        padding: 24px 20px;
        overflow: auto;
      }

      .canvas-wrap {
        position: relative;
        min-height: 0;
      }

      .canvas-header {
        position: absolute;
        inset: 20px 20px auto 20px;
        z-index: 2;
        display: flex;
        justify-content: space-between;
        align-items: flex-start;
        pointer-events: none;
      }

      .badge {
        display: inline-flex;
        gap: 8px;
        align-items: center;
        padding: 8px 12px;
        border: 1px solid rgba(125, 211, 252, 0.15);
        background: rgba(7, 17, 31, 0.7);
        color: var(--muted);
        font-size: 12px;
        letter-spacing: 0.02em;
      }

      h1, h2, h3, p { margin: 0; }
      h1 { font-size: 32px; line-height: 1.05; margin-top: 10px; }
      h2 { font-size: 12px; letter-spacing: 0.14em; text-transform: uppercase; color: var(--muted); margin-bottom: 12px; }
      h3 { font-size: 14px; margin-bottom: 8px; }
      p { color: var(--muted); line-height: 1.5; }

      .hero-copy { max-width: 360px; pointer-events: auto; }

      .meta-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 10px;
        margin: 22px 0 0;
      }

      .meta-cell {
        padding: 12px;
        border: 1px solid var(--line);
        background: rgba(255, 255, 255, 0.02);
      }

      .meta-label {
        font-size: 11px;
        text-transform: uppercase;
        letter-spacing: 0.12em;
        color: var(--muted);
        margin-bottom: 6px;
      }

      .meta-value {
        font-size: 18px;
      }

      input[type="search"] {
        width: 100%;
        border: 1px solid var(--line);
        background: rgba(255, 255, 255, 0.03);
        color: var(--text);
        padding: 12px 14px;
        outline: none;
      }

      .legend, .list, .detail-list {
        display: grid;
        gap: 10px;
      }

      .legend { margin-top: 20px; }
      .legend-item {
        display: flex;
        gap: 10px;
        align-items: center;
        color: var(--muted);
        font-size: 13px;
      }

      .swatch {
        width: 10px;
        height: 10px;
        border-radius: 999px;
      }

      .list {
        margin-top: 18px;
      }

      .list button {
        text-align: left;
        padding: 12px;
        background: rgba(255, 255, 255, 0.02);
        border: 1px solid transparent;
        color: var(--text);
        cursor: pointer;
        transition: border-color 160ms ease, background 160ms ease, transform 160ms ease;
      }

      .list button:hover,
      .list button.active {
        border-color: rgba(103, 232, 249, 0.45);
        background: rgba(103, 232, 249, 0.08);
        transform: translateX(2px);
      }

      .list small, .inspector small {
        color: var(--muted);
        display: block;
        margin-top: 6px;
      }

      .graph-stage {
        width: 100%;
        height: 100%;
        display: block;
      }

      .empty {
        padding: 18px;
        border: 1px dashed var(--line);
        color: var(--muted);
      }

      .inspector-panel {
        display: grid;
        gap: 18px;
      }

      .detail-section {
        padding-bottom: 18px;
        border-bottom: 1px solid var(--line);
      }

      .kv {
        display: grid;
        gap: 8px;
      }

      .kv-item {
        display: grid;
        gap: 4px;
      }

      .kv-key {
        color: var(--muted);
        font-size: 12px;
        text-transform: uppercase;
        letter-spacing: 0.12em;
      }

      pre {
        margin: 0;
        white-space: pre-wrap;
        word-break: break-word;
        font-size: 12px;
        line-height: 1.5;
        color: #d6e2f4;
      }

      @media (max-width: 1080px) {
        .shell {
          grid-template-columns: 280px minmax(0, 1fr);
        }
        .inspector {
          display: none;
        }
      }

      @media (max-width: 760px) {
        .shell {
          grid-template-columns: 1fr;
          grid-template-rows: auto minmax(0, 1fr);
        }
        .sidebar {
          border-bottom: 1px solid var(--line);
          border-right: 0;
          max-height: 42vh;
        }
        .canvas-header {
          inset: 14px;
        }
        h1 {
          font-size: 24px;
        }
      }
    </style>
  </head>
  <body>
    <div class="shell">
      <aside class="panel sidebar">
        <h2>Casper Graph</h2>
        <p>Infrastructure graph from Terraform state, modules, and conventions.</p>
        <div style="margin-top: 18px;">
          <input id="search" type="search" placeholder="Search nodes, types, tags, attributes" />
        </div>
        <div class="legend">
          <div class="legend-item"><span class="swatch" style="background: var(--resource);"></span>State resources</div>
          <div class="legend-item"><span class="swatch" style="background: var(--module);"></span>Terraform modules</div>
          <div class="legend-item"><span class="swatch" style="background: var(--convention);"></span>Conventions</div>
        </div>
        <div id="list" class="list"></div>
      </aside>

      <main class="canvas-wrap">
        <div class="canvas-header">
          <div class="hero-copy">
            <div class="badge">Local graph viewer</div>
            <h1>See resources, modules, and dependency edges together.</h1>
            <p style="margin-top: 10px;">Select a node to trace connected infrastructure and inspect extracted Terraform details.</p>
            <div class="meta-grid">
              <div class="meta-cell">
                <div class="meta-label">Nodes</div>
                <div id="nodeCount" class="meta-value">0</div>
              </div>
              <div class="meta-cell">
                <div class="meta-label">Edges</div>
                <div id="edgeCount" class="meta-value">0</div>
              </div>
            </div>
          </div>
          <div class="badge" id="selectionBadge">No selection</div>
        </div>
        <svg id="graph" class="graph-stage" viewBox="0 0 1200 900" preserveAspectRatio="xMidYMid slice"></svg>
      </main>

      <aside class="panel inspector">
        <div id="inspector" class="inspector-panel">
          <div class="empty">Select a node to inspect attributes, tags, and connections.</div>
        </div>
      </aside>
    </div>

    <script>
      const svg = document.getElementById("graph");
      const list = document.getElementById("list");
      const searchInput = document.getElementById("search");
      const inspector = document.getElementById("inspector");
      const selectionBadge = document.getElementById("selectionBadge");
      const nodeCount = document.getElementById("nodeCount");
      const edgeCount = document.getElementById("edgeCount");

      const palette = {
        aws_db_instance: "#7dd3fc",
        aws_security_group: "#38bdf8",
        aws_db_subnet_group: "#0ea5e9",
        terraform_module: "#86efac",
        terraform_convention: "#fcd34d"
      };

      const state = {
        data: null,
        selectedId: null,
        visibleIds: new Set()
      };

      fetch("/api/graph")
        .then((response) => response.json())
        .then((data) => {
          state.data = decorateGraph(data);
          state.visibleIds = new Set(state.data.nodes.map((node) => node.id));
          nodeCount.textContent = state.data.nodes.length;
          edgeCount.textContent = state.data.edges.length;
          renderList();
          runLayout();
          renderInspector();
        })
        .catch((error) => {
          inspector.innerHTML = '<div class="empty">Failed to load graph: ' + error.message + '</div>';
        });

      searchInput.addEventListener("input", () => {
        if (!state.data) return;
        const query = searchInput.value.trim().toLowerCase();
        state.visibleIds = new Set(
          state.data.nodes
            .filter((node) => !query || node.search_text.includes(query))
            .map((node) => node.id)
        );
        if (state.selectedId && !state.visibleIds.has(state.selectedId)) {
          state.selectedId = null;
        }
        renderList();
        renderGraph();
        renderInspector();
      });

      function decorateGraph(data) {
        const nodes = data.nodes.map((node, index) => ({
          ...node,
          x: 220 + (index % 5) * 170,
          y: 180 + Math.floor(index / 5) * 160,
          vx: 0,
          vy: 0
        }));
        const nodeById = new Map(nodes.map((node) => [node.id, node]));
        const edges = data.edges.filter((edge) => nodeById.has(edge.from) && nodeById.has(edge.to));
        return { nodes, edges, nodeById };
      }

      function runLayout() {
        const iterations = 260;
        for (let step = 0; step < iterations; step++) {
          applyForces();
        }
        renderGraph();
      }

      function applyForces() {
        const nodes = state.data.nodes;
        const width = 1200;
        const height = 900;

        for (const node of nodes) {
          node.vx *= 0.86;
          node.vy *= 0.86;
        }

        for (let i = 0; i < nodes.length; i++) {
          for (let j = i + 1; j < nodes.length; j++) {
            const a = nodes[i];
            const b = nodes[j];
            const dx = a.x - b.x;
            const dy = a.y - b.y;
            const distSq = Math.max(dx * dx + dy * dy, 36);
            const force = 5200 / distSq;
            const fx = dx * force * 0.015;
            const fy = dy * force * 0.015;
            a.vx += fx;
            a.vy += fy;
            b.vx -= fx;
            b.vy -= fy;
          }
        }

        for (const edge of state.data.edges) {
          const from = state.data.nodeById.get(edge.from);
          const to = state.data.nodeById.get(edge.to);
          if (!from || !to) continue;
          const dx = to.x - from.x;
          const dy = to.y - from.y;
          const distance = Math.max(Math.hypot(dx, dy), 1);
          const target = 160;
          const delta = distance - target;
          const force = delta * 0.0028;
          const fx = (dx / distance) * force * 40;
          const fy = (dy / distance) * force * 40;
          from.vx += fx;
          from.vy += fy;
          to.vx -= fx;
          to.vy -= fy;
        }

        for (const node of nodes) {
          const centerBias = node.type === "terraform_module" ? 0.004 : node.type === "terraform_convention" ? 0.002 : 0.0012;
          const targetX = node.type === "terraform_module" ? width * 0.5 : node.type === "terraform_convention" ? width * 0.78 : width * 0.4;
          const targetY = node.type === "terraform_convention" ? height * 0.68 : height * 0.45;
          node.vx += (targetX - node.x) * centerBias;
          node.vy += (targetY - node.y) * centerBias;
          node.x = clamp(node.x + node.vx, 70, width - 70);
          node.y = clamp(node.y + node.vy, 90, height - 90);
        }
      }

      function renderGraph() {
        if (!state.data) return;
        const visibleNodes = state.data.nodes.filter((node) => state.visibleIds.has(node.id));
        const visibleNodeIds = new Set(visibleNodes.map((node) => node.id));
        const connectedIds = connectedNodeIds(state.selectedId);

        const edgesMarkup = state.data.edges
          .filter((edge) => visibleNodeIds.has(edge.from) && visibleNodeIds.has(edge.to))
          .map((edge) => {
            const from = state.data.nodeById.get(edge.from);
            const to = state.data.nodeById.get(edge.to);
            const active = state.selectedId && (edge.from === state.selectedId || edge.to === state.selectedId);
            return '<line x1="' + from.x + '" y1="' + from.y + '" x2="' + to.x + '" y2="' + to.y + '" stroke="' + (active ? 'var(--edge-active)' : 'var(--edge)') + '" stroke-width="' + (active ? 2.2 : 1.1) + '" />';
          })
          .join("");

        const nodesMarkup = visibleNodes
          .map((node) => {
            const color = palette[node.type] || "var(--resource)";
            const active = state.selectedId === node.id;
            const related = connectedIds.has(node.id);
            const radius = node.type === "terraform_module" ? 17 : node.type === "terraform_convention" ? 14 : 12;
            const opacity = state.selectedId ? (active || related ? 1 : 0.26) : 1;
            const textY = node.y + radius + 18;
	            return '<g data-node="' + node.id + '" style="cursor:pointer; opacity:' + opacity + '">' +
	              '<circle cx="' + node.x + '" cy="' + node.y + '" r="' + (radius + (active ? 6 : 0)) + '" fill="rgba(103,232,249,0.08)" />' +
	              '<circle cx="' + node.x + '" cy="' + node.y + '" r="' + radius + '" fill="' + color + '" stroke="' + (active ? '#f8fafc' : 'rgba(255,255,255,0.22)') + '" stroke-width="' + (active ? 2.6 : 1.2) + '" />' +
	              '<text x="' + node.x + '" y="' + textY + '" fill="' + (active ? '#f8fafc' : '#b7c7db') + '" text-anchor="middle" font-size="12">' + escapeHtml(shortLabel(node.label)) + '</text>' +
	              '</g>';
	          })
	          .join("");

	        svg.innerHTML = '<defs>' +
	          '<filter id="glow">' +
	          '<feGaussianBlur stdDeviation="10" result="coloredBlur"></feGaussianBlur>' +
	          '<feMerge><feMergeNode in="coloredBlur"></feMergeNode><feMergeNode in="SourceGraphic"></feMergeNode></feMerge>' +
	          '</filter>' +
	          '</defs>' +
	          '<rect x="0" y="0" width="1200" height="900" fill="transparent"></rect>' +
	          '<g filter="url(#glow)">' + edgesMarkup + '</g>' +
	          '<g>' + nodesMarkup + '</g>';

        svg.querySelectorAll("[data-node]").forEach((element) => {
          element.addEventListener("click", () => {
            state.selectedId = element.getAttribute("data-node");
            renderList();
            renderGraph();
            renderInspector();
          });
        });
      }

      function renderList() {
        if (!state.data) return;
        const visible = state.data.nodes.filter((node) => state.visibleIds.has(node.id));
        if (visible.length === 0) {
          list.innerHTML = '<div class="empty">No nodes match the current search.</div>';
          return;
        }

        list.innerHTML = visible
	          .map((node) => '<button class="' + (state.selectedId === node.id ? 'active' : '') + '" data-item="' + node.id + '">' +
	            escapeHtml(node.label) +
	            '<small>' + escapeHtml(node.type) + ' | ' + escapeHtml(node.summary) + '</small>' +
	            '</button>')
	          .join("");

        list.querySelectorAll("[data-item]").forEach((button) => {
          button.addEventListener("click", () => {
            state.selectedId = button.getAttribute("data-item");
            renderList();
            renderGraph();
            renderInspector();
          });
        });
      }

      function renderInspector() {
        if (!state.data || !state.selectedId) {
          selectionBadge.textContent = "No selection";
          inspector.innerHTML = '<div class="empty">Select a node to inspect attributes, tags, and connections.</div>';
          return;
        }

        const node = state.data.nodeById.get(state.selectedId);
        const relatedEdges = state.data.edges.filter((edge) => edge.from === node.id || edge.to === node.id);
        const relatedNodes = relatedEdges.map((edge) => {
          const otherId = edge.from === node.id ? edge.to : edge.from;
          return state.data.nodeById.get(otherId);
        }).filter(Boolean);

        selectionBadge.textContent = node.type;
	        inspector.innerHTML = '<div class="detail-section">' +
	          '<h2>Selected</h2>' +
	          '<h3>' + escapeHtml(node.label) + '</h3>' +
	          '<p>' + escapeHtml(node.summary) + '</p>' +
	          '</div>' +
	          '<div class="detail-section kv">' +
	          renderKV("Type", node.type) +
	          renderKV("Managed by", node.managed_by) +
	          renderKV("Module path", node.module_path || "n/a") +
	          renderKV("Source", node.source) +
	          '</div>' +
	          '<div class="detail-section">' +
	          '<h2>Connections</h2>' +
	          '<div class="detail-list">' +
	          (relatedEdges.length ? relatedEdges.map((edge, index) => {
	            const other = relatedNodes[index];
	            return '<div><h3>' + escapeHtml(other ? other.label : edge.to) + '</h3><small>' + escapeHtml(edge.kind + ' | ' + edge.source) + '</small></div>';
	          }).join("") : '<div class="empty">No connected edges.</div>') +
	          '</div>' +
	          '</div>' +
	          '<div class="detail-section">' +
	          '<h2>Attributes</h2>' +
	          '<pre>' + escapeHtml(JSON.stringify(node.attributes, null, 2)) + '</pre>' +
	          '</div>' +
	          '<div class="detail-section">' +
	          '<h2>Tags</h2>' +
	          '<pre>' + escapeHtml(JSON.stringify(node.tags, null, 2)) + '</pre>' +
	          '</div>';
      }

      function connectedNodeIds(selectedId) {
        const ids = new Set();
        if (!selectedId || !state.data) return ids;
        ids.add(selectedId);
        for (const edge of state.data.edges) {
          if (edge.from === selectedId) ids.add(edge.to);
          if (edge.to === selectedId) ids.add(edge.from);
        }
        return ids;
      }

      function renderKV(key, value) {
        return '<div class="kv-item"><div class="kv-key">' + escapeHtml(key) + '</div><div>' + escapeHtml(String(value)) + '</div></div>';
      }

      function shortLabel(label) {
        if (label.length <= 26) return label;
	        return label.slice(0, 23) + "...";
      }

      function clamp(value, min, max) {
        return Math.max(min, Math.min(max, value));
      }

      function escapeHtml(value) {
        return value
          .replaceAll("&", "&amp;")
          .replaceAll("<", "&lt;")
          .replaceAll(">", "&gt;")
          .replaceAll('"', "&quot;");
      }
    </script>
  </body>
</html>`))
