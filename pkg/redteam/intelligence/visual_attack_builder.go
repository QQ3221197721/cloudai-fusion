
package redteam

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/sirupsen/logrus"
)

// VisualAttackPathBuilder constructs optimized attack paths for visualization
type VisualAttackPathBuilder struct {
	logger *logrus.Logger
	chainer *KillChainChainer
	
	// Neo4j client placeholder (would be implemented separately)
	neo4jClient interface{} // neo4j.Driver or similar
}

// NewVisualAttackPathBuilder creates a builder instance
func NewVisualAttackPathBuilder(logger *logrus.Logger) *VisualAttackPathBuilder {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	return &VisualAttackPathBuilder{
		logger: logger,
		chainer: NewKillChainChainer(logger),
	}
}

// BuildVisualization creates an optimized visual representation of an attack path
func (vab *VisualAttackPathBuilder) BuildVisualization(ctx context.Context, options VisualizationOptions) (*VisualAttackData, error) {
	startTime := time.Now()
	
	vab.logger.WithFields(logrus.Fields{
		"cves": len(options.TargetCVEs),
		"goal_phases": options.GoalPhases,
		"max_depth": options.MaxDepth,
	}).Info("Building visual attack path")
	
	// Step 1: Find optimal attack path using chainer
	chainResult, err := vab.chainer.FindOptimalAttackPath(ctx, options.TargetCVEs, options.GoalPhases, options.Constraints)
	if err != nil {
		return nil, fmt.Errorf("failed to find attack path: %w", err)
	}
	
	// Step 2: Convert chain to visual graph
	graphData := vab.convertToVisualGraph(chainResult.Path, options.IncludeMetadata)
	
	// Step 3: Calculate layout positions
	layouted := vab.applyLayout(graphData, options.LayoutType)
	
	// Step 4: Apply filters and optimizations
	filtered := vab.applyFilters(layouted, options.Filters)
	
	duration := time.Since(startTime)
	
	return &VisualAttackData{
		GraphData: filtered,
		PathAnalysis: PathAnalysisResult{
			TotalSteps: len(chainResult.Path.Steps),
			TotalDuration: chainResult.EstimatedTime,
			DetectionRisk: chainResult.DetectionRisk,
			ExploitReliability: chainResult.ExploitReliability,
		},
		LayoutMetrics: LayoutMetrics{
			ComputationTimeMs: duration.Milliseconds(),
			NodeCount: len(filtered.Nodes),
			LinkCount: len(filtered.Links),
			LayoutType: string(options.LayoutType),
		},
		Timestamp: time.Now().UTC(),
	}, nil
}

// convertToVisualGraph transforms an AttackChain into a GraphData structure
func (vab *VisualAttackPathBuilder) convertToVisualGraph(path *AttackChain, includeMetadata bool) *VisualGraphData {
	graph := &VisualGraphData{
		Nodes: make([]GraphNode, 0),
		Links: make([]GraphLink, 0),
		Metadata: GraphMetadata{
			LastUpdated: time.Now().UTC(),
			NodeTypes:   []string{},
			LinkTypes:   []string{},
		},
	}
	
	nodeMap := make(map[string]*GraphNode)
	linkIDCounter := 0
	
	// Add kill chain phases as base nodes
	for _, phase := range AllKillChainPhases {
		node := GraphNode{
			ID:              fmt.Sprintf("phase-%v", phase),
			Label:           phase.Name,
			Type:            NodeTypeKillChainPhase,
			Size:           15.0,
			Color:          "#9b59b6",
			FillOpacity:    0.8,
			CustomProperties: map[string]any{"description": phase.Description},
		}
		
		graph.Nodes = append(graph.Nodes, node)
		nodeMap[node.ID] = &graph.Nodes[len(graph.Nodes)-1]
		
		// Update metadata
		if !contains(graph.Metadata.NodeTypes, string(node.Type)) {
			graph.Metadata.NodeTypes = append(graph.Metadata.NodeTypes, string(node.Type))
		}
	}
	
	// Add CVE nodes and exploit relationships
	for _, step := range path.Steps {
		if step.CVEID != "" {
			cveNode := GraphNode{
				ID:              fmt.Sprintf("cve-%s", step.CVEID),
				Label:           step.CVEID,
				Type:            NodeTypeCVE,
				Size:           12.0,
				Color:          "#e74c3c",
				FillOpacity:    0.9,
				CustomProperties: map[string]any{
					"cvss_score": step.RiskScore,
					"severity":   getSeverityLabel(step.RiskScore),
				},
			}
			
			graph.Nodes = append(graph.Nodes, cveNode)
			nodeMap[cveNode.ID] = &graph.Nodes[len(graph.Nodes)-1]
			
			// Update metadata
			if !contains(graph.Metadata.NodeTypes, string(cveNode.Type)) {
				graph.Metadata.NodeTypes = append(graph.Metadata.NodeTypes, string(cveNode.Type))
			}
			
			// Link CVE to corresponding kill chain phase
			link := GraphLink{
				ID:      fmt.Sprintf("link-%d", linkIDCounter),
				Source:  nodeMap[fmt.Sprintf("phase-%s", step.Phase)].ID,
				Target:  cveNode.ID,
				Type:    LinkLeadsToPhase,
				Label:   "leads_to",
				Width:   2.0,
				Color:   "#ccc",
				Opacity: 0.6,
				Directed: true,
			}
			
			graph.Links = append(graph.Links, link)
			linkIDCounter++
			
			if !contains(graph.Metadata.LinkTypes, string(link.Type)) {
				graph.Metadata.LinkTypes = append(graph.Metadata.LinkTypes, string(link.Type))
			}
		}
		
		// Add MITRE technique if present
		if step.TechniqueID != "" {
			techniqueNode := GraphNode{
				ID:              fmt.Sprintf("tech-%s", step.TechniqueID),
				Label:           step.TechniqueID,
				Type:            NodeTypeMITRETechnique,
				Size:           10.0,
				Color:          "#2ecc71",
				FillOpacity:    0.85,
				CustomProperties: map[string]any{
					"name":        getTechniqueName(step.TechniqueID),
					"tactic":      step.Phase,
				},
			}
			
			graph.Nodes = append(graph.Nodes, techniqueNode)
			nodeMap[techniqueNode.ID] = &graph.Nodes[len(graph.Nodes)-1]
			
			if !contains(graph.Metadata.NodeTypes, string(techniqueNode.Type)) {
				graph.Metadata.NodeTypes = append(graph.Metadata.NodeTypes, string(techniqueNode.Type))
			}
			
			// Link CVE to technique
			if cveNodeID := fmt.Sprintf("cve-%s", step.CVEID); nodeMap[cveNodeID] != nil {
				link := GraphLink{
					ID:      fmt.Sprintf("link-%d", linkIDCounter),
					Source:  cveNodeID,
					Target:  techniqueNode.ID,
					Type:    LinkUsesTechnique,
					Label:   "uses_technique",
					Width:   1.5,
					Color:   "#27ae60",
					Opacity: 0.7,
					Directed: true,
				}
				
				graph.Links = append(graph.Links, link)
				linkIDCounter++
				
				if !contains(graph.Metadata.LinkTypes, string(link.Type)) {
					graph.Metadata.LinkTypes = append(graph.Metadata.LinkTypes, string(link.Type))
				}
			}
		}
	}
	
	// Add sequential links between steps
	for i := 0; i < len(path.Steps)-1; i++ {
		currentStep := path.Steps[i]
		nextStep := path.Steps[i+1]
		
		currentNodeID := ""
		nextNodeID := ""
		
		if currentStep.CVEID != "" {
			currentNodeID = fmt.Sprintf("cve-%s", currentStep.CVEID)
		} else if currentStep.TechniqueID != "" {
			currentNodeID = fmt.Sprintf("tech-%s", currentStep.TechniqueID)
		}
		
		if nextStep.CVEID != "" {
			nextNodeID = fmt.Sprintf("cve-%s", nextStep.CVEID)
		} else if nextStep.TechniqueID != "" {
			nextNodeID = fmt.Sprintf("tech-%s", nextStep.TechniqueID)
		}
		
		if currentNodeID != "" && nextNodeID != "" {
			link := GraphLink{
				ID:      fmt.Sprintf("link-%d", linkIDCounter),
				Source:  currentNodeID,
				Target:  nextNodeID,
				Type:    "FOLLOWS", // Custom link type
				Label:   "follows",
				Width:   3.0,
				Color:   "#34495e",
				Opacity: 0.9,
				Directed: true,
			}
			
			graph.Links = append(graph.Links, link)
			linkIDCounter++
		}
	}
	
	graph.Metadata.TotalNodes = len(graph.Nodes)
	graph.Metadata.TotalLinks = len(graph.Links)
	
	return graph
}

// applyLayout calculates positions for all nodes using force-directed algorithm
func (vab *VisualAttackPathBuilder) applyLayout(graph *VisualGraphData, layoutType LayoutType) *VisualGraphData {
	switch layoutType {
	case LayoutCircular:
		vab.layoutCircular(graph)
	case LayoutHierarchical:
		vab.layoutHierarchical(graph)
	default:
		vab.layoutForceDirected(graph)
	}
	
	return graph
}

// layoutCircular places nodes in a circular pattern
func (vab *VisualAttackPathBuilder) layoutCircular(graph *VisualGraphData) {
	n := len(graph.Nodes)
	if n == 0 {
		return
	}
	
	centerX := 400.0
	centerY := 300.0
	radius := 200.0
	
	for i, node := range graph.Nodes {
		angle := 2 * math.Pi * float64(i) / float64(n)
		
		node.Position = &Vector2D{
			X: centerX + radius*math.Cos(angle),
			Y: centerY + radius*math.Sin(angle),
		}
	}
}

// layoutHierarchical arranges nodes in hierarchical layers
func (vab *VisualAttackPathBuilder) layoutHierarchical(graph *VisualGraphData) {
	nodeLayer := make(map[string]int)
	
	// Assign layers based on kill chain phase order
	for _, node := range graph.Nodes {
		if node.Type == NodeTypeKillChainPhase {
			for layer, phase := range AllKillChainPhases {
				if phase.Name == node.Label {
					nodeLayer[node.ID] = layer
					break
				}
			}
		}
	}
	
	rows := len(AllKillChainPhases)
	_ = math.Ceil(float64(len(graph.Nodes)) / float64(rows))
	rowHeight := 100.0
	colWidth := 150.0
	marginX := 50.0
	marginY := 50.0
	
	for _, node := range graph.Nodes {
		layer := nodeLayer[node.ID]
		
		row := float64(layer)
		col := 0.0
		
		// Distribute nodes within each row
		countInRow := 0
		for _, otherNode := range graph.Nodes {
			if nodeLayer[otherNode.ID] == layer {
				if otherNode.ID == node.ID {
					break
				}
				countInRow++
			}
		}
		col = float64(countInRow)
		
		node.Position = &Vector2D{
			X: marginX + col*colWidth,
			Y: marginY + row*rowHeight,
		}
	}
}

// layoutForceDirected uses simple spring physics simulation
func (vab *VisualAttackPathBuilder) layoutForceDirected(graph *VisualGraphData) {
	// Initialize random positions
	for i := range graph.Nodes {
		graph.Nodes[i].Position = &Vector2D{
			X: float64(graph.Nodes[i].Value*100) + float64(i)*50,
			Y: float64(graph.Nodes[i].Value*100) + 300,
		}
	}
	
	// Simple spring simulation iterations
	iterations := 50
	k := 0.5 // Spring constant
	
	for iter := 0; iter < iterations; iter++ {
		// Reset velocity
		for i := range graph.Nodes {
			graph.Nodes[i].Velocity = Vector2D{X: 0, Y: 0}
		}
		
		// Apply forces
		for i, nodeA := range graph.Nodes {
			for j := i + 1; j < len(graph.Nodes); j++ {
				nodeB := graph.Nodes[j]
				
				dist := distance(nodeA.Position, nodeB.Position)
				if dist == 0 {
					dist = 1
				}
				
				// Repulsive force (nodes push apart)
				repulsion := 5000.0 / (dist * dist)
				dx := (nodeB.Position.X - nodeA.Position.X) / dist
				dy := (nodeB.Position.Y - nodeA.Position.Y) / dist
				
				graph.Nodes[i].Velocity.X += dx * -repulsion
				graph.Nodes[i].Velocity.Y += dy * -repulsion
				graph.Nodes[j].Velocity.X += dx * repulsion
				graph.Nodes[j].Velocity.Y += dy * repulsion
			}
		}
		
		// Attractive force along edges
		for _, link := range graph.Links {
			var nodeA, nodeB *GraphNode
			
			for i := range graph.Nodes {
				if graph.Nodes[i].ID == link.Source {
					nodeA = &graph.Nodes[i]
					break
				}
			}
			
			if nodeA == nil {
				continue
			}
			
			for i := range graph.Nodes {
				if graph.Nodes[i].ID == link.Target {
					nodeB = &graph.Nodes[i]
					break
				}
			}
			
			if nodeB == nil {
				continue
			}
			
			dist := distance(nodeA.Position, nodeB.Position)
			if dist == 0 {
				dist = 1
			}
			
			force := (dist - k*10) * 0.05 // Spring force
			
			dx := (nodeB.Position.X - nodeA.Position.X) / dist
			dy := (nodeB.Position.Y - nodeA.Position.Y) / dist
			
			nodeA.Velocity.X += dx * force
			nodeA.Velocity.Y += dy * force
			nodeB.Velocity.X -= dx * force
			nodeB.Velocity.Y -= dy * force
		}
		
		// Update positions with damping
		damping := 0.9
		for i := range graph.Nodes {
			node := &graph.Nodes[i]
			node.Position.X += node.Velocity.X * damping
			node.Position.Y += node.Velocity.Y * damping
			
			// Keep within bounds
			node.Position.X = math.Max(50, math.Min(750, node.Position.X))
			node.Position.Y = math.Max(50, math.Min(550, node.Position.Y))
		}
	}
}

// applyFilters filters the graph based on user options
func (vab *VisualAttackPathBuilder) applyFilters(graph *VisualGraphData, filters FilterOptions) *VisualGraphData {
	result := graph.Clone()
	
	// Filter by node types
	if len(filters.ExcludedTypes) > 0 {
		excludeSet := make(map[NodeType]bool)
		for _, t := range filters.ExcludedTypes {
			excludeSet[t] = true
		}
		
		filteredNodes := make([]GraphNode, 0)
		for _, node := range result.Nodes {
			if !excludeSet[node.Type] {
				filteredNodes = append(filteredNodes, node)
			}
		}
		result.Nodes = filteredNodes
	}
	
	// Highlight critical nodes
	if filters.HighlightCriticalNodes {
		criticalIDs := make([]string, 0)
		for _, node := range result.Nodes {
			if node.Size >= 15.0 {
				criticalIDs = append(criticalIDs, node.ID)
			}
		}
		result.Highlights = criticalIDs
	}
	
	return result
}

// Helper functions for layout
func distance(a, b *Vector2D) float64 {
	if a == nil || b == nil {
		return 1
	}
	dx := b.X - a.X
	dy := b.Y - a.Y
	return math.Sqrt(dx*dx + dy*dy)
}

func (g *VisualGraphData) Clone() *VisualGraphData {
	clone := &VisualGraphData{
		Nodes:     make([]GraphNode, len(g.Nodes)),
		Links:     make([]GraphLink, len(g.Links)),
		Metadata:  g.Metadata,
		Highlights: g.Highlights,
	}
	copy(clone.Nodes, g.Nodes)
	copy(clone.Links, g.Links)
	return clone
}

// getSeverityLabel returns a human-readable severity label for a risk score
func getSeverityLabel(score float64) string {
	switch {
	case score >= 9.0:
		return "Critical"
	case score >= 7.0:
		return "High"
	case score >= 4.0:
		return "Medium"
	default:
		return "Low"
	}
}

// getTechniqueName returns the display name for a MITRE ATT&CK technique ID
func getTechniqueName(techniqueID string) string {
	// Simplified lookup - in production would query a full MITRE database
	return techniqueID
}
