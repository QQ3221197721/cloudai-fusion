package redteam

import (
	"sync"
)

// attackgraph.go holds the evolving engagement state the planner reasons over:
// discovered assets, accumulated findings, and free-form observations. It is the
// "memory" that turns single-shot planning into an iterative (ReAct) loop - the
// planner proposes the next actions given what has been learned so far.

// Asset is a discovered target element (host, service, URL, credential ref).
type Asset struct {
	Kind  string         `json:"kind"`
	Value string         `json:"value"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// AttackGraph is the mutable, concurrency-safe engagement state.
type AttackGraph struct {
	mu           sync.Mutex
	assets       map[string]*Asset
	findings     []*Finding
	observations []string
}

// NewAttackGraph creates an empty graph.
func NewAttackGraph() *AttackGraph {
	return &AttackGraph{assets: make(map[string]*Asset)}
}

// AddAsset records a discovered asset (idempotent by value).
func (g *AttackGraph) AddAsset(kind, value string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.assets[value]; !ok {
		g.assets[value] = &Asset{Kind: kind, Value: value}
	}
}

// AddFinding appends a finding to the graph.
func (g *AttackGraph) AddFinding(f *Finding) {
	if f == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.findings = append(g.findings, f)
}

// Observe records a free-form observation (e.g. a tool summary).
func (g *AttackGraph) Observe(note string) {
	if note == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.observations = append(g.observations, note)
}

// State is a point-in-time, serializable snapshot for prompt-building and export.
type State struct {
	Assets       []Asset    `json:"assets"`
	Findings     []*Finding `json:"findings"`
	Observations []string   `json:"observations"`
}

// Snapshot returns a copy of the current graph state.
func (g *AttackGraph) Snapshot() State {
	g.mu.Lock()
	defer g.mu.Unlock()
	st := State{
		Findings:     append([]*Finding{}, g.findings...),
		Observations: append([]string{}, g.observations...),
	}
	for _, a := range g.assets {
		st.Assets = append(st.Assets, *a)
	}
	return st
}

// FindingCount returns how many findings have accumulated.
func (g *AttackGraph) FindingCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.findings)
}
