package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ASHUTOSH-SWAIN-GIT/casper-mcp/internal/graph"
)

// Export writes a self-contained HTML file with the graph data embedded.
// The visual layout mirrors the hand-authored reference: vis-network rendering,
// Tokyo Night palette, sidebar with meta / search / groups legend / resource
// types breakdown.
func Export(snapshot graph.GraphSnapshot, outputPath string) error {
	payload := buildExportPayload(snapshot)

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal graph: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}

	html := strings.Replace(exportBase, "/*GRAPH_DATA*/", string(data), 1)
	return os.WriteFile(outputPath, []byte(html), 0o644)
}

type exportNode struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Title      string `json:"title"`
	Group      string `json:"group"`
	Type       string `json:"type"`
	Module     string `json:"module"`
	Identifier string `json:"identifier"`
}

type exportEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Arrows string `json:"arrows"`
}

type exportType struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type exportMeta struct {
	ResourceCount int    `json:"resource_count"`
	DepCount      int    `json:"dep_count"`
	FetchedAt     string `json:"fetched_at"`
}

type exportPayload struct {
	Nodes  []exportNode `json:"nodes"`
	Edges  []exportEdge `json:"edges"`
	Groups []string     `json:"groups"`
	Types  []exportType `json:"types"`
	Meta   exportMeta   `json:"meta"`
}

func buildExportPayload(snapshot graph.GraphSnapshot) exportPayload {
	nodes := make([]exportNode, 0, len(snapshot.Resources))
	groupSet := map[string]struct{}{}
	typeCounts := map[string]int{}

	for _, r := range snapshot.Resources {
		group := groupFor(r.ModulePath)
		label := shortLabel(r.Identifier)
		title := r.Identifier + "\n" + r.Type + "\n" + group

		nodes = append(nodes, exportNode{
			ID:         r.ID,
			Label:      label,
			Title:      title,
			Group:      group,
			Type:       r.Type,
			Module:     group,
			Identifier: r.Identifier,
		})
		groupSet[group] = struct{}{}
		typeCounts[r.Type]++
	}

	edges := make([]exportEdge, 0, len(snapshot.Dependencies))
	for _, d := range snapshot.Dependencies {
		edges = append(edges, exportEdge{From: d.FromResource, To: d.ToResource, Arrows: "to"})
	}

	groups := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	types := make([]exportType, 0, len(typeCounts))
	for t, c := range typeCounts {
		types = append(types, exportType{Type: t, Count: c})
	}
	sort.Slice(types, func(i, j int) bool {
		if types[i].Count != types[j].Count {
			return types[i].Count > types[j].Count
		}
		return types[i].Type < types[j].Type
	})

	return exportPayload{
		Nodes:  nodes,
		Edges:  edges,
		Groups: groups,
		Types:  types,
		Meta: exportMeta{
			ResourceCount: len(nodes),
			DepCount:      len(edges),
			FetchedAt:     time.Now().UTC().Format(time.RFC3339),
		},
	}
}

func groupFor(modulePath string) string {
	p := strings.TrimSpace(modulePath)
	if p == "" || p == "root" || p == "." {
		return "root"
	}
	p = strings.TrimPrefix(p, "./")
	if rest, ok := strings.CutPrefix(p, "modules/"); ok {
		return "module:" + rest
	}
	return p
}

func shortLabel(identifier string) string {
	if i := strings.LastIndex(identifier, "."); i >= 0 && i+1 < len(identifier) {
		return identifier[i+1:]
	}
	return identifier
}

const exportBase = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Casper Infrastructure Graph</title>
<script src="https://unpkg.com/vis-network@9.1.9/standalone/umd/vis-network.min.js"></script>
<style>
  html,body{margin:0;height:100%;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f1115;color:#e6e6e6}
  #app{display:grid;grid-template-columns:280px 1fr;height:100vh}
  #sidebar{padding:14px;overflow:auto;border-right:1px solid #222;background:#13161c}
  #sidebar h1{font-size:14px;margin:0 0 8px;letter-spacing:.05em;text-transform:uppercase;color:#7aa2f7}
  #sidebar h2{font-size:11px;margin:14px 0 6px;color:#9aa5b1;text-transform:uppercase;letter-spacing:.08em}
  .meta{font-size:12px;color:#9aa5b1;margin-bottom:10px}
  .meta b{color:#e6e6e6}
  .row{display:flex;justify-content:space-between;font-size:12px;padding:2px 0;border-bottom:1px solid #1c1f26}
  .row span:last-child{color:#9aa5b1}
  input[type=text]{width:100%;box-sizing:border-box;padding:6px 8px;background:#0f1115;color:#e6e6e6;border:1px solid #2a2f3a;border-radius:4px;font-size:12px}
  .legend{display:flex;flex-wrap:wrap;gap:6px}
  .chip{display:inline-flex;align-items:center;gap:6px;font-size:11px;padding:3px 7px;border-radius:10px;background:#1c1f26;cursor:pointer;user-select:none}
  .chip.off{opacity:.35}
  .dot{width:10px;height:10px;border-radius:50%}
  #network{height:100vh}
  #info{position:absolute;right:16px;top:16px;max-width:340px;background:#13161cdd;border:1px solid #2a2f3a;border-radius:6px;padding:10px 12px;font-size:12px;display:none}
  #info h3{margin:0 0 6px;font-size:13px;color:#7aa2f7}
  #info pre{white-space:pre-wrap;word-break:break-word;margin:0;font-family:ui-monospace,Menlo,monospace;font-size:11px;color:#cdd2da}
</style>
</head>
<body>
<div id="app">
  <aside id="sidebar">
    <h1>Casper Graph</h1>
    <div class="meta">
      <div class="row"><span>Resources</span><span><b id="m_res"></b></span></div>
      <div class="row"><span>Dependencies</span><span><b id="m_dep"></b></span></div>
      <div class="row"><span>Fetched</span><span id="m_at"></span></div>
    </div>
    <input id="search" type="text" placeholder="Search by name / type…">
    <h2>Groups</h2>
    <div id="legend" class="legend"></div>
    <h2>Resource Types</h2>
    <div id="types"></div>
  </aside>
  <div id="network"></div>
</div>
<div id="info"></div>
<script>
const DATA = /*GRAPH_DATA*/;

const palette = ["#7aa2f7","#bb9af7","#9ece6a","#e0af68","#f7768e","#7dcfff","#ff9e64","#73daca","#c0caf5","#b4f9f8","#ffc777","#c099ff","#86e1fc","#fb7185","#a9b1d6","#ff8e72","#5eead4","#facc15","#f472b6","#34d399"];
const groupColor = {};
DATA.groups.forEach((g,i)=> groupColor[g] = palette[i % palette.length]);

document.getElementById('m_res').textContent = DATA.meta.resource_count;
document.getElementById('m_dep').textContent = DATA.meta.dep_count;
document.getElementById('m_at').textContent = DATA.meta.fetched_at;

const legend = document.getElementById('legend');
const groupOn = {};
DATA.groups.forEach(g=>{
  groupOn[g]=true;
  const el = document.createElement('span');
  el.className='chip';
  el.dataset.group=g;
  el.innerHTML = '<span class="dot" style="background:'+groupColor[g]+'"></span>'+g;
  el.onclick=()=>{ groupOn[g]=!groupOn[g]; el.classList.toggle('off',!groupOn[g]); applyFilter(); };
  legend.appendChild(el);
});

const typesEl = document.getElementById('types');
DATA.types.forEach(t=>{
  const r=document.createElement('div'); r.className='row';
  r.innerHTML = '<span>'+t.type+'</span><span>'+t.count+'</span>';
  typesEl.appendChild(r);
});

const nodes = new vis.DataSet(DATA.nodes.map(n=>({
  id:n.id, label:n.label, title:n.title, group:n.group,
  color:{background:groupColor[n.group], border:'#1a1d23', highlight:{background:'#fff', border:groupColor[n.group]}},
  font:{color:'#e6e6e6', size:11},
  shape:'dot', size:10,
  _type:n.type, _module:n.module, _identifier:n.identifier,
})));
const edges = new vis.DataSet(DATA.edges.map((e,i)=>({id:'e'+i, from:e.from, to:e.to, arrows:e.arrows, color:{color:'#2a2f3a',highlight:'#7aa2f7'}, smooth:{type:'continuous'}})));

const network = new vis.Network(document.getElementById('network'), {nodes, edges}, {
  physics:{ solver:'forceAtlas2Based', forceAtlas2Based:{gravitationalConstant:-40, springLength:90, springConstant:0.08}, stabilization:{iterations:300}},
  interaction:{ hover:true, tooltipDelay:120 },
  edges:{ width:0.6 },
});

const info = document.getElementById('info');
network.on('click', p=>{
  if(!p.nodes.length){ info.style.display='none'; return; }
  const n = nodes.get(p.nodes[0]);
  info.style.display='block';
  info.innerHTML = '<h3>'+n._identifier+'</h3><pre>type:    '+n._type+'\ngroup:   '+n.group+'\nmodule:  '+n._module+'</pre>';
});

const search = document.getElementById('search');
search.addEventListener('input', applyFilter);

function applyFilter(){
  const q = search.value.toLowerCase().trim();
  const visibleIds = new Set();
  DATA.nodes.forEach(n=>{
    const groupOk = groupOn[n.group];
    const qOk = !q || n.label.toLowerCase().includes(q) || n.type.toLowerCase().includes(q) || n.module.toLowerCase().includes(q);
    if(groupOk && qOk) visibleIds.add(n.id);
  });
  nodes.update(DATA.nodes.map(n=>({id:n.id, hidden:!visibleIds.has(n.id)})));
  edges.update(DATA.edges.map((e,i)=>({id:'e'+i, hidden: !(visibleIds.has(e.from) && visibleIds.has(e.to))})));
}
</script>
</body>
</html>`
