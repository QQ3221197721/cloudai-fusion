package redteam

import (
	"math"
	"time"
)

// VisualGraphData represents a Neo4j knowledge graph with all relevant entities
type VisualGraphData struct {
	Nodes     []GraphNode   `json:"nodes"`
	Links     []GraphLink   `json:"links"`
	Metadata  GraphMetadata `json:"metadata"`
	Highlights []string     `json:"highlights,omitempty"` // IDs of highlighted nodes/links
}

// GraphNode represents a node in the visualization
type GraphNode struct {
	ID              string        `json:"id"`
	Label           string        `json:"label"`
	Type            NodeType      `json:"type"`
	CustomProperties map[string]any `json:"custom_properties,omitempty"`
	Position        *Vector2D     `json:"position,omitempty"` // Set during layout
	Velocity        Vector2D      `json:"-"`                   // Internal use for layout
	Size            float64       `json:"size"`               // Based on centrality
	Color           string        `json:"color"`
	FillOpacity     float64       `json:"fill_opacity"`     // 0-1 for transparency
	Pending         bool          `json:"pending"`          // Node is pending creation
	Group           int           `json:"group"`            // Community/group ID
	Value           float64       `json:"value"`            // For force-directed layout
}

// Vector2D represents a 2D position vector
type Vector2D struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeType defines different types of graph entities
type NodeType string

const (
	NodeTypeCVE             NodeType = "cve"
	NodeTypeExploit         NodeType = "exploit"
	NodeTypeMITRETechnique  NodeType = "mitre_technique"
	NodeTypeThreatIndicator NodeType = "threat_indicator"
	NodeTypeKillChainPhase  NodeType = "kill_chain_phase"
)

// GraphLink represents an edge in the visualization
type GraphLink struct {
	ID              string            `json:"id"`
	Source          string            `json:"source"`
	Target          string            `json:"target"`
	Type            LinkType          `json:"type"`
	CustomProperties map[string]any    `json:"custom_properties,omitempty"`
	Label           string            `json:"label,omitempty"`
	Width           float64           `json:"width"`
	Color           string            `json:"color"`
	Opacity         float64           `json:"opacity"` // 0-1 for transparency
	Directed        bool              `json:"directed"`
}

// LinkType defines relationship types between nodes
type LinkType string

const (
	LinkHasExploit         LinkType = "HAS_EXPLOIT"
	LinkUsesTechnique      LinkType = "USES_TECHNIQUE"
	LinkRelatedToThreat    LinkType = "RELATED_TO_THREAT"
	LinkLeadsToPhase       LinkType = "LEADS_TO_PHASE"
)

// GraphMetadata contains metadata about the graph
type GraphMetadata struct {
	TotalNodes    int               `json:"total_nodes"`
	TotalLinks    int               `json:"total_links"`
	FilterOptions map[string][]string `json:"filter_options"`
	LastUpdated   time.Time         `json:"last_updated"`
	NodeTypes     []string          `json:"node_types"`
	LinkTypes     []string          `json:"link_types"`
}

// CalculateCentrality computes node centrality scores for size/color mapping
func (g *VisualGraphData) CalculateCentrality() {
	nodeDegrees := make(map[string]int)
	
	// Count degree for each node
	for _, link := range g.Links {
		nodeDegrees[link.Source]++
		if link.Target != link.Source {
			nodeDegrees[link.Target]++
		}
	}
	
	maxDegree := 0
	for _, degree := range nodeDegrees {
		if degree > maxDegree {
			maxDegree = degree
		}
	}
	
	// Normalize degree to size (min 5, max 20 pixels)
	minSize := 5.0
	maxSize := 20.0
	
	for i := range g.Nodes {
		node := &g.Nodes[i]
		
		degree := nodeDegrees[node.ID]
		node.Value = float64(degree)
		
		// Map degree to size using log scaling for better distribution
		if maxDegree > 0 && degree > 0 {
			sizeRatio := math.Log10(float64(degree+1)) / math.Log10(float64(maxDegree+1))
			node.Size = minSize + sizeRatio*(maxSize-minSize)
		} else {
			node.Size = minSize
		}
		
		// Color based on type and centrality
		node.Color = getNodeColor(node.Type, node.Size/maxSize)
	}
}

// FilterByType filters the graph to show only specific node types
func (g *VisualGraphData) FilterByType(types ...NodeType) *VisualGraphData {
	filtered := &VisualGraphData{
		Nodes:     []GraphNode{},
		Links:     []GraphLink{},
		Metadata:  g.Metadata,
	}
	
	typeSet := make(map[NodeType]bool)
	for _, t := range types {
		typeSet[t] = true
	}
	
	// Filter nodes
	for _, node := range g.Nodes {
		if typeSet[node.Type] {
			filtered.Nodes = append(filtered.Nodes, node)
		}
	}
	
	// Filter links (only include if both source and target are in filtered nodes)
	nodeMap := make(map[string]bool)
	for _, node := range filtered.Nodes {
		nodeMap[node.ID] = true
	}
	
	for _, link := range g.Links {
		if nodeMap[link.Source] && nodeMap[link.Target] {
			filtered.Links = append(filtered.Links, link)
		}
	}
	
	return filtered
}

// HighlightPath highlights a specific attack path
func (g *VisualGraphData) HighlightPath(pathIDs []string) {
	g.Highlights = pathIDs
	
	// Update highlighting styles
	for i := range g.Nodes {
		g.Nodes[i].FillOpacity = 0.3 // Semi-transparent by default
		if contains(g.Highlights, g.Nodes[i].ID) {
			g.Nodes[i].FillOpacity = 1.0 // Fully opaque when highlighted
		}
	}
	
	for i := range g.Links {
		g.Links[i].Opacity = 0.3
		link := &g.Links[i]
		if contains(g.Highlights, link.Source) || contains(g.Highlights, link.Target) {
			g.Links[i].Opacity = 1.0
		}
	}
}

// GetConnectedNodes returns all nodes directly connected to a given node
func (g *VisualGraphData) GetConnectedNodes(nodeID string) []GraphNode {
	connected := make([]GraphNode, 0)
	visited := make(map[string]bool)
	
	for _, link := range g.Links {
		if link.Source == nodeID && !visited[link.Target] {
			if node := g.FindNode(link.Target); node != nil {
				connected = append(connected, *node)
				visited[link.Target] = true
			}
		}
		if link.Target == nodeID && !visited[link.Source] {
			if node := g.FindNode(link.Source); node != nil {
				connected = append(connected, *node)
				visited[link.Source] = true
			}
		}
	}
	
	return connected
}

// FindNode finds a node by ID
func (g *VisualGraphData) FindNode(id string) *GraphNode {
	for i, node := range g.Nodes {
		if node.ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

// Helper functions
func getNodeColor(nodeType NodeType, centrality float64) string {
	baseColors := map[NodeType]string{
		NodeTypeCVE:             "#e74c3c", // Red
		NodeTypeExploit:         "#3498db", // Blue
		NodeTypeMITRETechnique:  "#2ecc71", // Green
		NodeTypeThreatIndicator: "#f39c12", // Orange
		NodeTypeKillChainPhase:  "#9b59b6", // Purple
	}
	
	baseColor := baseColors[nodeType]
	
	// Adjust brightness based on centrality
	// Higher centrality = brighter/more saturated
	if centrality > 0.7 {
		return adjustBrightness(baseColor, 0.3) // Brighter
	} else if centrality < 0.3 {
		return adjustBrightness(baseColor, -0.3) // Darker
	}
	
	return baseColor
}

func adjustBrightness(hexColor string, amount float64) string {
	// Simplified brightness adjustment
	// In production, use proper RGB conversion
	return hexColor
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// OptimizeForVisualization improves layout performance for large graphs
func (g *VisualGraphData) OptimizeForVisualization(maxNodes int) *VisualGraphData {
	if len(g.Nodes) <= maxNodes {
		return g
	}
	
	// Create simplified view showing only most important nodes
	centralNodes := make([]GraphNode, 0)
	peripheralNodes := make([]GraphNode, 0)
	
	for _, node := range g.Nodes {
		if node.Size >= 12.0 {
			centralNodes = append(centralNodes, node)
		} else {
			peripheralNodes = append(peripheralNodes, node)
		}
	}
	
	result := &VisualGraphData{
		Nodes: centralNodes,
		Links: g.Links,
	}
	
	return result
}
