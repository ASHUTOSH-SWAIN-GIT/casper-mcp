package ui

import (
	"encoding/json"
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
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Type       string         `json:"type"`
	ManagedBy  string         `json:"managed_by"`
	ModulePath string         `json:"module_path"`
	Source     string         `json:"source"`
	Tags       map[string]any `json:"tags"`
	Attributes map[string]any `json:"attributes"`
	SearchText string         `json:"search_text"`
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
			ID:         resource.ID,
			Label:      resource.Identifier,
			Type:       resource.Type,
			ManagedBy:  resource.ManagedBy,
			ModulePath: resource.ModulePath,
			Source:     resource.Source,
			Tags:       resource.Tags,
			Attributes: resource.Attributes,
			SearchText: searchText(resource),
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



var indexTemplate = template.Must(template.New("graph").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width,initial-scale=1"/>
  <title>Casper Graph</title>
  <style>
    *{box-sizing:border-box;margin:0;padding:0}
    html,body{height:100%;background:#09090f;color:#c9d1d9;font-family:ui-monospace,"SF Mono",Menlo,monospace;font-size:13px;overflow:hidden}
    .shell{display:grid;grid-template-columns:1fr 300px;height:100vh}
    .canvas-wrap{position:relative;overflow:hidden}
    svg{width:100%;height:100%;display:block}

    .panel{background:rgba(13,17,23,0.92);border-left:1px solid #1c2128;display:flex;flex-direction:column;overflow:hidden;backdrop-filter:blur(12px)}
    .panel-section{padding:14px 16px;border-bottom:1px solid #1c2128;flex-shrink:0}
    .panel-title{font-size:10px;letter-spacing:0.1em;text-transform:uppercase;color:#8b949e;margin-bottom:8px;display:flex;align-items:center;gap:6px}
    .pulse{display:inline-block;width:6px;height:6px;border-radius:50%;background:#3fb950;animation:pulse 2s ease-in-out infinite;flex-shrink:0}
    @keyframes pulse{0%,100%{opacity:1}50%{opacity:0.2}}
    .counts{font-size:11px;color:#8b949e;margin-top:2px}
    .counts span{color:#e6edf3}

    input[type=search]{width:100%;background:#0d1117;border:1px solid #1c2128;color:#c9d1d9;padding:7px 10px;outline:none;font-family:inherit;font-size:12px;border-radius:2px}
    input[type=search]::placeholder{color:#3d444d}
    input[type=search]:focus{border-color:#388bfd}

    .node-info{padding:14px 16px;border-bottom:1px solid #1c2128;flex-shrink:0;min-height:90px}
    .node-info-empty{color:#3d444d;font-size:12px;padding-top:2px}
    .node-name{color:#e6edf3;font-size:13px;margin-bottom:4px;word-break:break-all}
    .node-type-badge{display:inline-flex;align-items:center;gap:5px;font-size:11px;color:#8b949e;margin-bottom:8px}
    .node-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
    .node-meta{font-size:11px;color:#8b949e;line-height:1.7}
    .node-meta span{color:#c9d1d9}
    .conn-list{margin-top:6px;display:flex;flex-direction:column;gap:3px;max-height:100px;overflow-y:auto}
    .conn-item{font-size:11px;color:#8b949e;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .conn-item span{color:#388bfd}

    .communities-wrap{flex:1;overflow-y:auto}
    .comm-item{display:flex;align-items:center;gap:8px;padding:7px 16px;cursor:pointer;border-bottom:1px solid #1c2128;transition:background 120ms}
    .comm-item:hover{background:#0d1117}
    .comm-item.active-comm{background:#0d1117;border-left:2px solid #388bfd;padding-left:14px}
    .comm-dot{width:9px;height:9px;border-radius:50%;flex-shrink:0}
    .comm-name{flex:1;font-size:12px;color:#c9d1d9;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
    .comm-count{font-size:11px;color:#484f58;flex-shrink:0}
  </style>
</head>
<body>
<div class="shell">
  <main class="canvas-wrap">
    <svg id="graph"></svg>
  </main>
  <aside class="panel">
    <div class="panel-section">
      <div class="panel-title"><span class="pulse"></span>Casper Graph</div>
      <div class="counts">nodes: <span id="nodeCount">—</span>&ensp;edges: <span id="edgeCount">—</span>&ensp;<span id="lastUpdate" style="color:#3d444d"></span></div>
    </div>
    <div class="panel-section" style="padding:10px 16px">
      <input id="search" type="search" placeholder="search nodes…"/>
    </div>
    <div class="node-info" id="nodeInfo">
      <div class="node-info-empty">Click a node to inspect</div>
    </div>
    <div class="panel-section" style="padding:10px 16px 8px">
      <div class="panel-title" style="margin-bottom:0">Communities</div>
    </div>
    <div class="communities-wrap" id="commList"></div>
  </aside>
</div>

<script>
const svgEl = document.getElementById("graph");
const searchEl = document.getElementById("search");
const nodeCountEl = document.getElementById("nodeCount");
const edgeCountEl = document.getElementById("edgeCount");
const lastUpdateEl = document.getElementById("lastUpdate");
const nodeInfoEl = document.getElementById("nodeInfo");
const commListEl = document.getElementById("commList");

const PALETTE = [
  "#ef4444","#f97316","#eab308","#22c55e","#14b8a6",
  "#3b82f6","#a855f7","#ec4899","#06b6d4","#84cc16",
  "#f59e0b","#10b981","#6366f1","#e11d48","#0ea5e9",
  "#8b5cf6","#d946ef","#64748b","#fb923c","#4ade80",
];

const state = {
  data: null, selectedId: null, filterComm: null, visibleIds: new Set(),
  pan:{x:0,y:0}, zoom:1, dragging:false, dragStart:null, panStart:null,
};
let lastFp = "";

function communityKey(n) {
  const parts = n.label.replace(/\\/g,"/").split("/").filter(Boolean);
  if (parts.length >= 2) return parts.slice(-2,-1)[0] + "/" + parts.slice(-1)[0];
  if (parts.length === 1) return parts[0];
  return n.type;
}

function buildCommunities(nodes) {
  const map = new Map();
  for (const n of nodes) {
    const key = communityKey(n);
    if (!map.has(key)) map.set(key, []);
    map.get(key).push(n.id);
  }
  const sorted = [...map.entries()].sort((a,b) => b[1].length - a[1].length);
  const communities = new Map();
  sorted.forEach(([key, ids], i) => {
    communities.set(key, { color: PALETTE[i % PALETTE.length], ids, key });
  });
  return communities;
}

function communityColor(communities, n) {
  const key = communityKey(n);
  return communities.get(key)?.color || "#8b949e";
}

function fingerprint(data) {
  return data.nodes.length+":"+data.edges.length+":"+(data.nodes[0]?.id||"");
}

function loadGraph(initial) {
  fetch("/api/graph").then(r=>r.json()).then(data => {
    const fp = fingerprint(data);
    if (!initial && fp === lastFp) return;
    lastFp = fp;

    const prev = state.data;
    state.data = prepare(data);

    if (initial) {
      state.visibleIds = new Set(state.data.nodes.map(n=>n.id));
      layout();
      fitView();
    } else {
      const q = searchEl.value.trim().toLowerCase();
      state.visibleIds = applyFilters(q, state.filterComm);
      if (state.selectedId && !state.data.nodeById.has(state.selectedId)) state.selectedId = null;
      if (prev) for (const n of state.data.nodes) { const o=prev.nodeById.get(n.id); if(o){n.x=o.x;n.y=o.y;n.vx=0;n.vy=0;} }
      layoutNew(prev?.nodeById);
    }

    nodeCountEl.textContent = state.data.nodes.length;
    edgeCountEl.textContent = state.data.edges.length;
    lastUpdateEl.textContent = new Date().toLocaleTimeString();
    renderGraph();
    renderCommList();
    renderNodeInfo();
  }).catch(err => {
    if (initial) nodeInfoEl.innerHTML = '<div class="node-info-empty">Failed to load: '+err.message+'</div>';
  });
}

function applyFilters(q, comm) {
  return new Set(state.data.nodes.filter(n => {
    if (q && !n.search_text.includes(q)) return false;
    if (comm && communityKey(n) !== comm) return false;
    return true;
  }).map(n=>n.id));
}

function prepare(data) {
  const W=3200, H=2600;
  const communities = buildCommunities(data.nodes);
  const nodes = data.nodes.map(n => ({
    ...n,
    community: communityKey(n),
    color: communities.get(communityKey(n))?.color || "#8b949e",
    x: W/2, y: H/2, vx:0, vy:0,
  }));
  const nodeById = new Map(nodes.map(n=>[n.id,n]));
  const edges = data.edges.filter(e=>nodeById.has(e.from)&&nodeById.has(e.to));
  return { nodes, edges, nodeById, W, H, communities };
}

function layout() {
  const {nodes, edges, W, H} = state.data;
  const comms = [...state.data.communities.entries()].sort((a,b)=>b[1].ids.length-a[1].ids.length);
  const cx = W/2, cy = H/2;

  // Place community centers: top 2 near center, rest in rings
  const centers = new Map();
  comms.forEach(([key, c], i) => {
    let r, angle;
    if (i === 0) { r = 0; angle = 0; }
    else if (i <= 6) {
      r = 280 + c.ids.length * 3;
      angle = (2*Math.PI*(i-1)/6) - Math.PI/2;
    } else if (i <= 16) {
      r = 560 + c.ids.length * 2;
      angle = (2*Math.PI*(i-7)/10) - Math.PI/6;
    } else {
      r = 820;
      angle = (2*Math.PI*(i-17)/(comms.length-17));
    }
    centers.set(key, { x: cx + r*Math.cos(angle), y: cy + r*Math.sin(angle) });
  });

  // Pack nodes within each community in concentric rings
  const byComm = new Map();
  for (const [key] of comms) byComm.set(key, []);
  for (const n of nodes) byComm.get(n.community)?.push(n);

  for (const [key, members] of byComm) {
    const center = centers.get(key) || {x:cx, y:cy};
    const total = members.length;
    if (total === 1) {
      members[0].x = center.x; members[0].y = center.y;
      continue;
    }
    // Sunflower/ring packing
    members.forEach((n, i) => {
      if (i === 0) { n.x = center.x; n.y = center.y; return; }
      const ring = Math.ceil((Math.sqrt(4*i+1)-1)/2);
      const ringStart = ring*(ring-1);
      const ringTotal = ring*6 < total-ringStart ? ring*6 : total-ringStart;
      const ringIdx = i - ringStart - 1;
      const r = ring * 20;
      const angle = (2*Math.PI*ringIdx/Math.max(ringTotal,1));
      n.x = center.x + r*Math.cos(angle) + (Math.random()-0.5)*4;
      n.y = center.y + r*Math.sin(angle) + (Math.random()-0.5)*4;
    });
  }

  // Light force pass just to resolve edge springs and minor overlaps
  for (let s=0; s<40; s++) {
    for (const n of nodes) {n.vx*=0.7;n.vy*=0.7;}
    for (const e of edges) {
      const a=state.data.nodeById.get(e.from),b=state.data.nodeById.get(e.to);
      if(!a||!b)continue;
      const dx=b.x-a.x,dy=b.y-a.y,d=Math.max(Math.hypot(dx,dy),1);
      const f=(d-60)*0.04,fx=dx/d*f,fy=dy/d*f;
      a.vx+=fx;a.vy+=fy;b.vx-=fx;b.vy-=fy;
    }
    for (const n of nodes) {n.x+=n.vx;n.y+=n.vy;}
  }
}

function layoutNew(oldById) {
  if (!oldById) return;
  const newNodes = state.data.nodes.filter(n=>!oldById.has(n.id));
  if (!newNodes.length) return;
  // Re-run cluster placement for new nodes only
  const {W, H} = state.data;
  newNodes.forEach(n => { n.x=W/2+(Math.random()-0.5)*200; n.y=H/2+(Math.random()-0.5)*200; });
}

function fitView() {
  const {nodes,W,H}=state.data;
  if(!nodes.length)return;
  const rect=svgEl.getBoundingClientRect();
  const pad=60;
  let x0=Infinity,y0=Infinity,x1=-Infinity,y1=-Infinity;
  for(const n of nodes){x0=Math.min(x0,n.x);y0=Math.min(y0,n.y);x1=Math.max(x1,n.x);y1=Math.max(y1,n.y);}
  const gw=x1-x0+pad*2,gh=y1-y0+pad*2;
  const scale=Math.min(rect.width/gw,rect.height/gh,1);
  state.zoom=scale;
  state.pan.x=rect.width/2-(x0+x1)/2*scale;
  state.pan.y=rect.height/2-(y0+y1)/2*scale;
}

let vpEl = null;

function renderGraph(transformOnly) {
  if(!state.data)return;
  if(transformOnly && vpEl){
    vpEl.setAttribute("transform","translate("+state.pan.x+","+state.pan.y+") scale("+state.zoom+")");
    return;
  }
  const visible=new Set(state.data.nodes.filter(n=>state.visibleIds.has(n.id)).map(n=>n.id));
  const conn=neighbors(state.selectedId);
  const showLabels=state.zoom>0.42;

  let out='<rect width="100%" height="100%" fill="#09090f"/>'+
    '<g id="vp" transform="translate('+state.pan.x+','+state.pan.y+') scale('+state.zoom+')">';

  for(const e of state.data.edges){
    if(!visible.has(e.from)||!visible.has(e.to))continue;
    const a=state.data.nodeById.get(e.from),b=state.data.nodeById.get(e.to);
    const active=state.selectedId&&(e.from===state.selectedId||e.to===state.selectedId);
    out+='<line x1="'+a.x+'" y1="'+a.y+'" x2="'+b.x+'" y2="'+b.y+'"'+
      ' stroke="'+(active?a.color:'rgba(255,255,255,0.1)')+'" stroke-width="'+(active?1.4:0.6)+'"/>';
  }

  for(const n of state.data.nodes){
    if(!visible.has(n.id))continue;
    const active=state.selectedId===n.id;
    const rel=conn.has(n.id);
    const dim=state.selectedId&&!active&&!rel;
    const r=n.type==="terraform_module"?11:n.type==="terraform_convention"?9:7;
    const op=dim?0.13:1;
    out+='<g data-node="'+n.id+'" style="cursor:pointer;opacity:'+op+'">';
    if(active)out+='<circle cx="'+n.x+'" cy="'+n.y+'" r="'+(r+5)+'" fill="'+n.color+'" opacity="0.18"/>';
    out+='<circle cx="'+n.x+'" cy="'+n.y+'" r="'+r+'" fill="'+n.color+'"'+
      ' stroke="'+(active?'rgba(255,255,255,0.7)':'rgba(0,0,0,0.25)')+'" stroke-width="'+(active?1.2:0.4)+'"/>';
    if((showLabels&&!dim)||active){
      out+='<text x="'+n.x+'" y="'+(n.y+r+11)+'" fill="'+(active?'#e6edf3':'rgba(180,195,210,0.6)')+'"'+
        ' text-anchor="middle" font-size="9" font-family="ui-monospace,monospace">'+esc(clip(n.label))+'</text>';
    }
    out+='</g>';
  }

  out+='</g>';
  svgEl.innerHTML=out;
  vpEl=svgEl.querySelector("#vp");
  svgEl.querySelectorAll("[data-node]").forEach(el=>{
    el.addEventListener("click",ev=>{
      ev.stopPropagation();
      const id=el.getAttribute("data-node");
      state.selectedId=state.selectedId===id?null:id;
      renderGraph(false);renderNodeInfo();renderCommList();
    });
  });
}

svgEl.addEventListener("click",()=>{
  if(state.selectedId){state.selectedId=null;renderGraph(false);renderNodeInfo();}
});
svgEl.addEventListener("wheel",ev=>{
  ev.preventDefault();
  const rect=svgEl.getBoundingClientRect();
  const mx=ev.clientX-rect.left,my=ev.clientY-rect.top;
  const f=ev.deltaY<0?1.12:0.9;
  state.pan.x=mx-(mx-state.pan.x)*f;
  state.pan.y=my-(my-state.pan.y)*f;
  state.zoom=Math.max(0.05,Math.min(8,state.zoom*f));
  renderGraph(true);
},{passive:false});
svgEl.addEventListener("mousedown",ev=>{
  if(ev.button!==0)return;
  state.dragging=true;
  state.dragStart={x:ev.clientX,y:ev.clientY};
  state.panStart={x:state.pan.x,y:state.pan.y};
  svgEl.style.cursor="grabbing";
});
window.addEventListener("mousemove",ev=>{
  if(!state.dragging)return;
  state.pan.x=state.panStart.x+(ev.clientX-state.dragStart.x);
  state.pan.y=state.panStart.y+(ev.clientY-state.dragStart.y);
  renderGraph(true);
});
window.addEventListener("mouseup",()=>{state.dragging=false;svgEl.style.cursor="";});

searchEl.addEventListener("input",()=>{
  if(!state.data)return;
  const q=searchEl.value.trim().toLowerCase();
  state.visibleIds=applyFilters(q,state.filterComm);
  if(state.selectedId&&!state.visibleIds.has(state.selectedId))state.selectedId=null;
  renderGraph();renderNodeInfo();renderCommList();
});

function renderNodeInfo() {
  if(!state.data||!state.selectedId){
    nodeInfoEl.innerHTML='<div class="node-info-empty">Click a node to inspect</div>';
    return;
  }
  const n=state.data.nodeById.get(state.selectedId);
  if(!n){nodeInfoEl.innerHTML='<div class="node-info-empty">Click a node to inspect</div>';return;}
  const relEdges=state.data.edges.filter(e=>e.from===n.id||e.to===n.id);
  const connHtml=relEdges.length
    ? relEdges.map(e=>{
        const otherId=e.from===n.id?e.to:e.from;
        const other=state.data.nodeById.get(otherId);
        return '<div class="conn-item"><span>'+esc(e.from===n.id?"→":"←")+'</span> '+esc(clip(other?.label||otherId))+'</div>';
      }).join("")
    : '<div class="conn-item" style="color:#3d444d">no connections</div>';
  nodeInfoEl.innerHTML=
    '<div class="node-name">'+esc(shortName(n.label))+'</div>'+
    '<div class="node-type-badge"><span class="node-dot" style="background:'+n.color+'"></span>'+esc(n.type)+'</div>'+
    '<div class="node-meta">'+
      (n.module_path?'path: <span>'+esc(shortName(n.module_path))+'</span><br>':'')+
      'community: <span>'+esc(n.community)+'</span>'+
      (relEdges.length?'<br>connections: <span>'+relEdges.length+'</span>':'')+
    '</div>'+
    (relEdges.length?'<div class="conn-list">'+connHtml+'</div>':'');
}

function renderCommList() {
  if(!state.data)return;
  const comms=[...state.data.communities.values()].sort((a,b)=>b.ids.length-a.ids.length);
  commListEl.innerHTML=comms.map(c=>{
    const active=state.filterComm===c.key;
    const visCount=c.ids.filter(id=>state.visibleIds.has(id)).length;
    return '<div class="comm-item'+(active?' active-comm':'')+'" data-comm="'+esc(c.key)+'">'+
      '<span class="comm-dot" style="background:'+c.color+'"></span>'+
      '<span class="comm-name">'+esc(c.key)+'</span>'+
      '<span class="comm-count">'+visCount+'</span>'+
      '</div>';
  }).join("");
  commListEl.querySelectorAll("[data-comm]").forEach(el=>{
    el.addEventListener("click",()=>{
      const key=el.getAttribute("data-comm");
      state.filterComm=state.filterComm===key?null:key;
      const q=searchEl.value.trim().toLowerCase();
      state.visibleIds=applyFilters(q,state.filterComm);
      if(state.selectedId&&!state.visibleIds.has(state.selectedId))state.selectedId=null;
      renderGraph();renderNodeInfo();renderCommList();
    });
  });
}

function neighbors(id){
  const ids=new Set();if(!id||!state.data)return ids;ids.add(id);
  for(const e of state.data.edges){if(e.from===id)ids.add(e.to);if(e.to===id)ids.add(e.from);}
  return ids;
}
function shortName(label){
  const parts=label.replace(/\\/g,"/").split("/").filter(Boolean);
  if(parts.length<=2)return label;return parts.slice(-2).join("/");
}
function clip(s){const n=shortName(s);return n.length<=24?n:n.slice(0,21)+"...";}
function esc(s){return String(s).replaceAll("&","&amp;").replaceAll("<","&lt;").replaceAll(">","&gt;").replaceAll('"',"&quot;");}

loadGraph(true);
setInterval(()=>loadGraph(false),4000);
</script>
</body>
</html>`))
