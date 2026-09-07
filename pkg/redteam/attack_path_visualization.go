package redteam

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	
	"github.com/sirupsen/logrus"
)

// ============================================================================
// ATTACK PATH VISUALIZATION ENGINE
// Renders attack paths into interactive SVG/HTML formats for security analysis
// ============================================================================

// AttackPathVisualizer provides rendering capabilities for attack path visualization
// supporting multiple output formats including SVG diagrams and HTML reports.
type AttackPathVisualizer struct {
	layoutEngine  *LayoutEngine
	renderer      *RenderEngine
	styleConfig   VisualizationStyle
	cache         *VisualizationCache
}

// LayoutEngine positions nodes in attack graph for optimal visual clarity
type LayoutEngine struct {
	nodeSpacing   float64
	layerHeight   float64
	horizontalGap float64
	maxWidth      float64
	rankSpacing   float64
}

// RenderEngine handles actual drawing of nodes, edges, and annotations
type RenderEngine struct {
	fontFamily  string
	lineCap     string
	lineJoin    string
	strokeWidth float64
	nodeRadius  float64
	colorScheme *ColorPalette
	logger      *logrus.Logger
}

// VisualizationStyle defines aesthetic parameters
type VisualizationStyle struct {
	DarkMode        bool
	AirbrushNodes   bool
	ShowLabels      bool
	HighlightCritical bool
	AnimationFPS    int
}

// GraphNode represents a positioned node in the visualization
type GraphNode struct {
	X        float64
	Y        float64
	Label    string
	NodeType string
	IsCritical bool
}

// AttackStep represents individual step in attack chain
type AttackStep struct {
	StepIndex         int
	NodeType          string
	IsCritical        bool
	Description       string
	Layer             int
	SuccessRate       float64
	DetectionRisk     float64
	EstimatedDuration time.Duration
}

// NewAttackPathVisualizer creates new visualizer instance with configured components.
func NewAttackPathVisualizer() *AttackPathVisualizer {
	return &AttackPathVisualizer{
		layoutEngine: NewLayoutEngine(),
		renderer:     NewRenderEngine(),
		styleConfig: VisualizationStyle{
			DarkMode:          false,
			AirbrushNodes:     true,
			ShowLabels:        true,
			HighlightCritical: true,
			AnimationFPS:      30,
		},
		cache: NewVisualizationCache(),
	}
}

// NewLayoutEngine creates layout engine with optimized spacing parameters.
func NewLayoutEngine() *LayoutEngine {
	return &LayoutEngine{
		nodeSpacing:   120.0,
		layerHeight:   150.0,
		horizontalGap: 80.0,
		maxWidth:      1200.0,
		rankSpacing:   100.0,
	}
}

// NewRenderEngine creates render engine with default styling.
func NewRenderEngine() *RenderEngine {
	return &RenderEngine{
		fontFamily:  "Arial, sans-serif",
		lineCap:     "round",
		lineJoin:    "round",
		strokeWidth: 2.0,
		nodeRadius:  30.0,
		colorScheme: DefaultColorPalette(),
		logger:      logrus.New(),
	}
}

// GenerateSVG renders attack path into Scalable Vector Graphics format.
func (v *AttackPathVisualizer) GenerateSVG(steps []AttackStep, outputPath string) error {
	v.renderer.logger.Debugf("Generating SVG for %d attack steps", len(steps))
	
	svgBuffer := bytes.Buffer{}
	canvasWidth := v.calculateCanvasWidth(steps)
	canvasHeight := v.calculateCanvasHeight(steps)
	
	svgHeader := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">
`, canvasWidth, canvasHeight, canvasWidth, canvasHeight)
	svgBuffer.WriteString(svgHeader)
	
	v.writeDefinitions(&svgBuffer)
	v.writeBackground(&svgBuffer, canvasWidth, canvasHeight)
	
	nodes := v.layoutEngine.LayoutNodes(steps)
	
	for i := 0; i < len(nodes)-1; i++ {
		sourceNode := nodes[i]
		targetNode := nodes[i+1]
		v.drawEdge(&svgBuffer, sourceNode, targetNode, false)
	}
	
	for _, node := range nodes {
		v.renderer.DrawNode(&svgBuffer, node, v.styleConfig)
	}
	
	v.writeAnnotations(&svgBuffer, steps, nodes)
	
	svgBuffer.WriteString("</svg>\n")
	
	outDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	
	if err := os.WriteFile(outputPath, svgBuffer.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write SVG file: %w", err)
	}
	
	v.renderer.logger.WithField("output_path", outputPath).Info("✅ SVG generation complete")
	return nil
}

// calculateCanvasWidth computes optimal width based on attack path complexity.
func (v *AttackPathVisualizer) calculateCanvasWidth(steps []AttackStep) float64 {
	baseWidth := 200.0
	numSteps := float64(len(steps))
	requiredWidth := baseWidth + (numSteps * v.layoutEngine.nodeSpacing)
	return requiredWidth
}

// calculateCanvasHeight computes optimal height based on node count per layer.
func (v *AttackPathVisualizer) calculateCanvasHeight(steps []AttackStep) float64 {
	layers := make(map[int]int)
	maxNodesPerLayer := 1.0
	
	for _, step := range steps {
		layers[step.Layer]++
		if layers[step.Layer] > maxNodesPerLayer {
			maxNodesPerLayer = float64(layers[step.Layer])
		}
	}
	
	baseHeight := 100.0
	requiredHeight := baseHeight + (maxNodesPerLayer * v.layoutEngine.layerHeight)
	return requiredHeight
}

// writeDefinitions adds reusable graphical elements.
func (v *AttackPathVisualizer) writeDefinitions(buf *bytes.Buffer) {
	buf.WriteString("<defs>\n")
	
	// Node gradient
	buf.WriteString("  <linearGradient id=\"nodeGradient\" x1=\"0%\" y1=\"0%\" x2=\"0%\" y2=\"100%\">\n")
	buf.WriteString("    <stop offset=\"0%\" style=\"stop-color:#4a90e2;stop-opacity:1\" />\n")
	buf.WriteString("    <stop offset=\"100%\" style=\"stop-color:#357abd;stop-opacity:1\" />\n")
	buf.WriteString("  </linearGradient>\n\n")
	
	// Critical node gradient
	buf.WriteString("  <linearGradient id=\"criticalGradient\" x1=\"0%\" y1=\"0%\" x2=\"0%\" y2=\"100%\">\n")
	buf.WriteString("    <stop offset=\"0%\" style=\"stop-color:#e74c3c;stop-opacity:1\" />\n")
	buf.WriteString("    <stop offset=\"100%\" style=\"stop-color:#c0392b;stop-opacity:1\" />\n")
	buf.WriteString("  </linearGradient>\n\n")
	
	// Arrow marker
	buf.WriteString("  <marker id=\"arrowhead\" markerWidth=\"10\" markerHeight=\"7\" refX=\"9\" refY=\"3.5\" orient=\"auto\">\n")
	buf.WriteString("    <polygon points=\"0 0, 10 3.5, 0 7\" fill=\"#666\"/>\n")
	buf.WriteString("  </marker>\n")
	
	buf.WriteString("</defs>\n")
}

// writeBackground fills canvas background.
func (v *AttackPathVisualizer) writeBackground(buf *bytes.Buffer, width, height float64) {
	backgroundFill := "#f8f9fa"
	if v.styleConfig.DarkMode {
		backgroundFill = "#1a1a2e"
	}
	
	buf.WriteString(fmt.Sprintf("<rect width=\"%f\" height=\"%f\" fill=\"%s\"/>\n", width, height, backgroundFill))
}

// drawEdge draws connection line between two nodes.
func (e *RenderEngine) DrawNode(buf *bytes.Buffer, node GraphNode, config VisualizationStyle) {
	x, y := node.X, node.Y
	radius := e.nodeRadius
	
	if node.IsCritical {
		radius += 4
	}
	
	fill := "url(#nodeGradient)"
	if node.IsCritical {
		fill = "url(#criticalGradient)"
	} else if node.NodeType == "entry" {
		fill = "#2ecc71"
	} else if node.NodeType == "exit" {
		fill = "#9b59b6"
	}
	
	buf.WriteString(fmt.Sprintf("<circle cx=\"%f\" cy=\"%f\" r=\"%f\" fill=\"%s\" stroke=\"#333\" stroke-width=\"2\"/>\n", 
		x, y, radius, fill))
	
	if config.ShowLabels && node.Label != "" {
		label := truncateString(node.Label, 20)
		buf.WriteString(fmt.Sprintf("<text x=\"%f\" y=\"%f\" text-anchor=\"middle\" font-family=\"%s\" font-size=\"11\" fill=\"white\">%s</text>\n",
			x, y+radius+15, e.fontFamily, label))
	}
}

// writeAnnotations adds supplementary information labels and legends.
func (v *AttackPathVisualizer) writeAnnotations(buf *bytes.Buffer, steps []AttackStep, nodes []GraphNode) {
	buf.WriteString("<g class=\"annotations\">\n")
	
	currentTime := time.Now().Format("2006-01-02 15:04:05")
	yPos := float64(len(nodes)*v.layoutEngine.layerHeight - 10)
	
	buf.WriteString(fmt.Sprintf("<text x=\"20\" y=\"%f\" class=\"timestamp\" font-size=\"12\" fill=\"#999\">", yPos))
	buf.WriteString(fmt.Sprintf("Generated: %s", currentTime))
	buf.WriteString("</text>\n")
	
	buf.WriteString("  <g transform=\"translate(20, 40)\">\n")
	buf.WriteString("    <rect x=\"0\" y=\"0\" width=\"20\" height=\"20\" fill=\"url(#nodeGradient)\" stroke=\"#333\" stroke-width=\"1\"/>\n")
	buf.WriteString("    <text x=\"25\" y=\"15\" font-size=\"11\" fill=\"#666\">Normal Step</text>\n")
	
	if v.styleConfig.HighlightCritical {
		buf.WriteString("    <rect x=\"150\" y=\"0\" width=\"20\" height=\"20\" fill=\"url(#criticalGradient)\" stroke=\"#333\" stroke-width=\"1\"/>\n")
		buf.WriteString("    <text x=\"175\" y=\"15\" font-size=\"11\" fill=\"#666\">Critical Step</text>\n")
	}
	
	buf.WriteString("  </g>\n")
	buf.WriteString("</g>\n")
}

// LayoutNodes computes positions for all nodes.
func (l *LayoutEngine) LayoutNodes(steps []AttackStep) []GraphNode {
	nodes := make([]GraphNode, len(steps))
	
	for i, step := range steps {
		nodes[i] = GraphNode{
			X:          100 + float64(i)*l.nodeSpacing,
			Y:          50 + float64(step.Layer)*l.layerHeight,
			Label:      step.Description,
			NodeType:   step.NodeType,
			IsCritical: step.IsCritical,
		}
	}
	
	return nodes
}

// DrawEdge draws arrow from source to target.
func (e *RenderEngine) DrawEdge(buf *bytes.Buffer, from, to GraphNode, isCritical bool) {
	lineColor := "#666"
	if isCritical || from.IsCritical || to.IsCritical {
		lineColor = "#e74c3c"
	}
	
	buf.WriteString(fmt.Sprintf("<line x1=\"%f\" y1=\"%f\" x2=\"%f\" y2=\"%f\" stroke=\"%s\" stroke-width=\"%.1f\" marker-end=\"url(#arrowhead)\"/>\n",
		from.X, from.Y, to.X, to.Y, lineColor, e.strokeWidth))
}

// Helper functions
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ============================================================================
// ADDITIONAL EXERCISES AND SUPPORTING CODE
// ============================================================================

// countCriticalSteps tallies critical attack steps
func countCriticalSteps(steps []AttackStep) int {
	count := 0
	for _, step := range steps {
		if step.IsCritical {
			count++
		}
	}
	return count
}

// getMaxLayer returns highest layer index in attack chain
func getMaxLayer(steps []AttackStep) int {
	maxLayer := 0
	for _, step := range steps {
		if step.Layer > maxLayer {
			maxLayer = step.Layer
		}
	}
	return maxLayer
}

// calculateDuration estimates attack chain execution time
func calculateDuration(steps []AttackStep) float64 {
	totalSeconds := 0.0
	for _, step := range steps {
		totalSeconds += step.EstimatedDuration.Seconds()
	}
	return totalSeconds
}

// serializeAttackSteps converts AttackStep slice to serializable format
func serializeAttackSteps(steps []AttackStep) []map[string]interface{} {
	serialized := make([]map[string]interface{}, len(steps))
	
	for i, step := range steps {
		serialized[i] = map[string]interface{}{
			"index":            i,
			"node_type":        step.NodeType,
			"is_critical":      step.IsCritical,
			"description":      step.Description,
			"layer":            step.Layer,
			"success_rate":     step.SuccessRate,
			"detection_risk":   step.DetectionRisk,
			"estimated_seconds": step.EstimatedDuration.Seconds(),
		}
	}
	
	return serialized
}
