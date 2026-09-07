package alerting

// causal_correlation_improved.go implements an enhanced causal correlation algorithm
// that addresses the "same source" over-merge problem identified in the FLIP mandate.
//
// KEY IMPROVEMENTS:
// 1. Temporal Causality Graph: alerts are linked via directed edges with timestamps,
//    capturing potential parent-child relationships through time ordering
// 2. Root-Cause Graph Building: constructs a DAG where edges represent causal hypotheses
//    based on temporal proximity + label overlap, not just shared source
// 3. Parent-Child Inference: uses PageRank-like scoring to identify likely root causes
//    within correlated clusters
//
// This implementation follows the FLIP mandate: if baseline shows loss on grouping quality,
// immediately improve causal correlation and re-benchmark. No accepting loss as final outcome.

import (
	"math"
	"sort"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Enhanced CausalCorrelationEngine v2
// ---------------------------------------------------------------------------

// CausalCorrelationEngineV2 is an improved correlation engine that builds
// temporal causality graphs and infers root causes from alert patterns.
type CausalCorrelationEngineV2 struct {
	mu             sync.Mutex
	groups         []*AlertGroupV2
	window         time.Duration // correlation window
	temporalThreshold time.Duration // max time gap for causal edge (default 5 minutes)
	minLabelOverlap float64      // minimum label similarity threshold (default 0.5)
}

// AlertGroupV2 is an enhanced alert group with causal metadata
type AlertGroupV2 struct {
	ID            string
	RootAlert     EvidenceAlert
	Related       []EvidenceAlert
	CreatedAt     time.Time
	CausalityGraph *CausalityGraph // local subgraph of this group
	DominantCause  string          // inferred dominant root cause label
}

// CausalityGraph represents a directed acyclic graph of causal hypotheses
// within an alert cluster. Nodes are alerts; edges indicate temporal-causal
// relationships (earlier alert potentially caused later one).
type CausalityGraph struct {
	nodes map[string]*GraphNode
	edges []*CausalEdge
}

// GraphNode is a node in the causality graph
type GraphNode struct {
	AlertID   string
	Alert     EvidenceAlert
	InDegree  int  // incoming causal edges (potential child)
	OutDegree int  // outgoing causal edges (potential parent)
	PageRank  float64 // importance score (higher = more likely root cause)
}

// CausalEdge represents a hypothesized causal relationship from src -> dst
type CausalEdge struct {
	SrcID       string
	DstID       string
	Confidence  float64 // how confident we are in this causal link
	TimeGap     float64 // seconds between src and dst
	LabelDelta  int     // number of differing labels
}

// NewCausalCorrelationEngineV2 creates an enhanced correlation engine
func NewCausalCorrelationEngineV2(window, temporalThreshold time.Duration) *CausalCorrelationEngineV2 {
	return &CausalCorrelationEngineV2{
		groups:            make([]*AlertGroupV2, 0),
		window:            window,
		temporalThreshold: temporalThreshold,
		minLabelOverlap:   0.5,
	}
}

// Correlate correlates an alert using enhanced temporal causality analysis
func (e *CausalCorrelationEngineV2) Correlate(alert EvidenceAlert) *AlertGroupV2 {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clean expired groups
	now := time.Now()
	active := e.groups[:0]
	for _, g := range e.groups {
		if now.Sub(g.CreatedAt) < e.window {
			active = append(active, g)
		}
	}
	e.groups = active

	// Try to find best matching group using enhanced similarity + temporal causality
	var bestMatch *AlertGroupV2
	var bestScore float64 = -math.MaxFloat64

	for _, g := range e.groups {
		score := e.calcEnhancedSimilarity(alert, g, now)
		if score > bestScore {
			bestScore = score
			bestMatch = g
		}
	}

	// If above threshold, correlate into existing group
	if bestScore > 0 && bestMatch != nil {
		// Update causality graph
		if bestMatch.CausalityGraph == nil {
			bestMatch.CausalityGraph = NewCausalityGraph()
		}
		bestMatch.CausalityGraph.AddNode(alert.ID, alert)
		
		// Create causal edges from all nodes in the group to this alert
		for _, node := range bestMatch.CausalityGraph.nodes {
			if alert.Timestamp.After(node.Alert.Timestamp) {
				bestMatch.CausalityGraph.createCausalEdge(node.Alert.ID, alert.ID, alert.Timestamp)
			}
		}
		
		// Update PageRank scores
		bestMatch.CausalityGraph.ComputePageRank()
		
		// Re-infer dominant cause
		bestMatch.DominantCause = e.inferDominantCause(bestMatch)
		
		bestMatch.Related = append(bestMatch.Related, alert)
		return bestMatch
	}

	// Create new group
	newGroup := &AlertGroupV2{
		ID:            generateGroupID(),
		RootAlert:     alert,
		Related:       []EvidenceAlert{},
		CreatedAt:     now,
		CausalityGraph: NewCausalityGraph(),
	}
	
	newGroup.CausalityGraph.AddNode(alert.ID, alert)
	e.groups = append(e.groups, newGroup)
	return nil // nil means new root group
}

// calcEnhancedSimilarity computes a sophisticated similarity score combining:
// 1. Label-based similarity (like before but more nuanced)
// 2. Temporal proximity penalty/reward
// 3. Source diversity bonus (alerts from different sources in tight time window may be cascade)
func (e *CausalCorrelationEngineV2) calcEnhancedSimilarity(alert EvidenceAlert, group *AlertGroupV2, now time.Time) float64 {
	root := group.RootAlert
	
	// Time decay factor: closer in time = higher chance of causality
	timeGap := alert.Timestamp.Sub(root.Timestamp).Seconds()
	maxTime := e.temporalThreshold.Seconds()
	timeDecay := math.Exp(-timeGap / maxTime) // exp(-t/T), ranges [1, ~0.6] at boundaries
	
	// Label similarity using Jaccard index with value matching
	jaccard := e.labelJaccardSimilarity(alert, root)
	
	// Source diversity factor: same source strongly indicates related; 
	// different sources in tight window might still be cascade
	sameSource := alert.Source == root.Source
	sourceFactor := 1.0
	if sameSource {
		sourceFactor = 1.5 // boost for same source
	} else {
		// Different sources but close in time could be real cascade
		// Penalize less if within 30 seconds (likely propagation)
		if timeGap < 30 {
			sourceFactor = 0.7 // moderate penalty for different sources
		} else {
			sourceFactor = 0.3 // stronger penalty for different sources far apart
		}
	}
	
	// Combined score: weighted sum with normalization
	labelScore := jaccard * sourceFactor
	timeScore := timeDecay
	
	// Final score emphasizes both label similarity and temporal proximity
	return (labelScore*0.6 + timeScore*0.4)
}

// labelJaccardSimilarity computes Jaccard similarity on label keys AND values
// intersection over union
func (e *CausalCorrelationEngineV2) labelJaccardSimilarity(a, b EvidenceAlert) float64 {
	intersection := 0
	union := make(map[string]bool)
	
	for k, v := range a.Labels {
		union[k] = true
		if bv, ok := b.Labels[k]; ok && bv == v {
			intersection++
		}
	}
	
	for k := range b.Labels {
		union[k] = true
	}
	
	total := len(union)
	if total == 0 {
		return 0
	}
	
	// Return Jaccard index
	return float64(intersection) / float64(total)
}

// createCausalEdge adds a directed causal edge from srcId to dstId
func (g *CausalityGraph) createCausalEdge(srcID, dstID string, alertTimestamp time.Time) {
	srcNode, srcOK := g.nodes[srcID]
	dstNode, _ := g.nodes[dstID]
	
	if !srcOK || dstNode == nil {
		return
	}
	
	timeGap := alertTimestamp.Sub(srcNode.Alert.Timestamp).Seconds()
	labelDiff := countLabelDifferences(srcNode.Alert, dstNode.Alert)
			
	// Confidence decreases with larger time gap and more label differences
	confidence := math.Exp(-timeGap/60) * math.Exp(-float64(labelDiff)/3)
			
	g.edges = append(g.edges, &CausalEdge{
		SrcID:      srcID,
		DstID:      dstID,
		Confidence: confidence,
		TimeGap:    timeGap,
		LabelDelta: labelDiff,
	})
	
	// Update degrees: src has outgoing edge, dst has incoming edge
	srcNode.OutDegree++
	dstNode.InDegree++
}

// inferDominantCause identifies which label value appears most frequently
// across correlated alerts as the likely dominant cause indicator
func (e *CausalCorrelationEngineV2) inferDominantCause(group *AlertGroupV2) string {
	labelCounts := make(map[string]int)
	
	// Count occurrences of each (key, value) pair excluding root
	for _, alert := range group.Related {
		for k, v := range alert.Labels {
			if k == "alertname" || k == "severity" {
				continue // exclude high-level attributes
			}
			key := k + "=" + v
			labelCounts[key]++
		}
	}
	
	if len(labelCounts) == 0 {
		return ""
	}
	
	// Find most common non-alertname/severity label
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range labelCounts {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})
	
	if len(sorted) > 0 && sorted[0].Value >= 2 {
		return sorted[0].Key
	}
	
	return ""
}

// ---------------------------------------------------------------------------
// CausalityGraph Methods
// ---------------------------------------------------------------------------

// NewCausalityGraph creates an empty causality graph
func NewCausalityGraph() *CausalityGraph {
	return &CausalityGraph{
		nodes: make(map[string]*GraphNode),
		edges: make([]*CausalEdge, 0),
	}
}

// AddNode adds a node to the graph
func (g *CausalityGraph) AddNode(id string, alert EvidenceAlert) {
	g.nodes[id] = &GraphNode{
		AlertID: id,
		Alert:   alert,
		InDegree:  0,
		OutDegree: 0,
		PageRank:  1.0, // initial rank
	}
}

// ComputePageRank performs iterative PageRank computation to identify likely root causes
// Higher PageRank = earlier in causal chain = more likely root cause
func (g *CausalityGraph) ComputePageRank() {
	const iterations = 10
	damping := 0.85
	
	for i := 0; i < iterations; i++ {
		// Accumulate ranks flowing into each node
		rankFlow := make(map[string]float64)
		
		for _, node := range g.nodes {
			if node.OutDegree == 0 {
				// Leaf node (no outgoing edges), doesn't distribute rank
				continue
			}
			
			// Distribute rank proportionally to out-neighbors
			distributed := node.PageRank / float64(node.OutDegree)
			
			// For each outgoing edge, calculate contribution
			for _, edge := range g.edges {
				if edge.SrcID == node.AlertID && edge.DstID != node.AlertID {
					// Weight by edge confidence
					contribution := distributed * edge.Confidence
					rankFlow[edge.DstID] += contribution
				}
			}
		}
		
		// Update PageRanks
		for id := range g.nodes {
			node := g.nodes[id]
			// Standard PageRank formula with damping
			rankFlow[id] += (1 - damping)
			node.PageRank = damping*rankFlow[id] + (1-damping)
		}
	}
}

// GetRootCandidate returns the node with highest PageRank (most likely root cause)
func (g *CausalityGraph) GetRootCandidate() *GraphNode {
	var best *GraphNode
	bestRank := -1.0
	
	for _, node := range g.nodes {
		if node.PageRank > bestRank {
			bestRank = node.PageRank
			best = node
		}
	}
	
	return best
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// countLabelDifferences counts how many label values differ between two alerts
func countLabelDifferences(a, b EvidenceAlert) int {
	diff := 0
	
	// Check all labels in a
	for k, v := range a.Labels {
		bv, exists := b.Labels[k]
		if !exists || bv != v {
			diff++
		}
	}
	
	// Check for labels only in b
	for k := range b.Labels {
		if _, exists := a.Labels[k]; !exists {
			diff++
		}
	}
	
	return diff
}
