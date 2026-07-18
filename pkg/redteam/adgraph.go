package redteam

// adgraph.go is the M4 Active Directory attack graph and BloodHound-style
// pathfinding. Nodes are principals (users/computers/groups); directed edges are
// abusable relationships (MemberOf, AdminTo, GenericAll, HasSession, ...), each
// tagged with the MITRE technique that abuses it. Shortest-path search over this
// graph is the "which hops get me to Domain Admin" reasoning a real engagement
// performs - implemented as a real BFS, unit-testable without a live domain.

// ADNode is a principal in the directory.
type ADNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // user | computer | group
	HighValue bool   `json:"high_value"`
}

// ADEdge is a directed, abusable relationship from one principal to another.
type ADEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`      // MemberOf | AdminTo | GenericAll | HasSession | ...
	Technique string `json:"technique"` // MITRE ATT&CK ID for abusing this edge
}

// ADGraph is a directed attack graph with adjacency for fast traversal.
type ADGraph struct {
	nodes map[string]*ADNode
	adj   map[string][]ADEdge
}

// NewADGraph creates an empty AD attack graph.
func NewADGraph() *ADGraph {
	return &ADGraph{nodes: make(map[string]*ADNode), adj: make(map[string][]ADEdge)}
}

// AddNode adds (or updates) a principal.
func (g *ADGraph) AddNode(id, kind string, highValue bool) {
	g.nodes[id] = &ADNode{ID: id, Kind: kind, HighValue: highValue}
}

// AddEdge adds a directed abusable relationship (auto-creating endpoints).
func (g *ADGraph) AddEdge(from, to, kind, technique string) {
	if _, ok := g.nodes[from]; !ok {
		g.AddNode(from, "unknown", false)
	}
	if _, ok := g.nodes[to]; !ok {
		g.AddNode(to, "unknown", false)
	}
	g.adj[from] = append(g.adj[from], ADEdge{From: from, To: to, Kind: kind, Technique: technique})
}

// ShortestPath returns the fewest-hop edge path from start to target (BFS).
func (g *ADGraph) ShortestPath(start, target string) ([]ADEdge, bool) {
	if start == target {
		return nil, true
	}
	prev := map[string]ADEdge{}
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range g.adj[n] {
			if visited[e.To] {
				continue
			}
			visited[e.To] = true
			prev[e.To] = e
			if e.To == target {
				return reconstructPath(prev, target), true
			}
			queue = append(queue, e.To)
		}
	}
	return nil, false
}

// PathToHighValue returns the fewest-hop path from start to the nearest
// high-value principal, and that principal's ID.
func (g *ADGraph) PathToHighValue(start string) ([]ADEdge, string, bool) {
	prev := map[string]ADEdge{}
	visited := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range g.adj[n] {
			if visited[e.To] {
				continue
			}
			visited[e.To] = true
			prev[e.To] = e
			if node, ok := g.nodes[e.To]; ok && node.HighValue {
				return reconstructPath(prev, e.To), e.To, true
			}
			queue = append(queue, e.To)
		}
	}
	return nil, "", false
}

// reconstructPath walks predecessor edges back from target to build the path.
func reconstructPath(prev map[string]ADEdge, target string) []ADEdge {
	var rev []ADEdge
	cur := target
	for {
		e, ok := prev[cur]
		if !ok {
			break
		}
		rev = append(rev, e)
		cur = e.From
	}
	// reverse into start->target order
	path := make([]ADEdge, len(rev))
	for i, e := range rev {
		path[len(rev)-1-i] = e
	}
	return path
}
