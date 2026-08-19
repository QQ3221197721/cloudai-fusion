// Package aiops - Self-healing engine continued (Part 2)
package aiops

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// ISOLATION FOREST MODEL ✅ HIGH-DIMENSIONAL ANOMALY DETECTION
// ===========================================================================

// IsolationForestModel implements random forest-based anomaly detection
type IsolationForestModel struct {
	logger       *logrus.Logger
	trees        []*IsolationTree
	sampleSize   int
	maxHeight    int
	cutoff       float64 // anomaly threshold
	trained      bool
}

// IsolationTree is a single tree in the isolation forest
type IsolationTree struct {
	root     *Node
	height   int
	maxHeight int
}

// Node represents a node in isolation tree
type Node struct {
	left         *Node
	right        *Node
	splitValue   float64
	splitFeature int
	size         int
	isExternal   bool
}

// NewIsolationForestModel creates new model instance
func NewIsolationForestModel(logger *logrus.Logger, numTrees int, sampleSize int) *IsolationForestModel {
	return &IsolationForestModel{
		logger: logger,
		sampleSize: sampleSize,
		trees: make([]*IsolationTree, numTrees),
		maxHeight: int(math.Ceil(math.Log2(float64(sampleSize)))),
		trained: false,
	}
}

// Train model on historical data
func (ifm *IsolationForestModel) Train(data []MetricsSnapshot) error {
	if len(data) < ifm.sampleSize {
		return fmt.Errorf("insufficient training data")
	}
	
	// Build isolation trees
	for i := range ifm.trees {
		sampledData := ifm.resampleData(data)
		tree := &IsolationTree{
			root:      buildIsolationTree(sampledData, 0, ifm.maxHeight),
			height:    0,
			maxHeight: ifm.maxHeight,
		}

		ifm.trees[i] = tree
	}
	
	ifm.trained = true
	ifm.cutoff = 3.5 // empirical threshold for anomaly score < cutoff → anomaly
	
	ifm.logger.Infof("Isolation Forest trained with %d trees", len(ifm.trees))
	return nil
}

// resampleData randomly samples from dataset
func (ifm *IsolationForestModel) resampleData(data []MetricsSnapshot) []MetricsSnapshot {
	n := len(data)
	sampled := make([]MetricsSnapshot, 0, ifm.sampleSize)
	
	// Random sampling with replacement
	for i := 0; i < ifm.sampleSize && i < n; i++ {
		idx := time.Now().UnixNano() % int64(n)
		sampled = append(sampled, data[idx])
	}
	
	return sampled
}

// IsAnomaly computes anomaly score using ensemble of isolation trees
func (ifm *IsolationForestModel) IsAnomaly(snapshot MetricsSnapshot, confidenceThreshold float64) (bool, float64, error) {
	if !ifm.trained {
		return false, 0.0, fmt.Errorf("model not trained")
	}
	
	// Extract feature vector
	x := extractFeatures(snapshot)
	
	// Compute average path length across all trees
	pathLengths := make([]float64, len(ifm.trees))
	
	for i, tree := range ifm.trees {
		pathLengths[i] = isolate(x, tree.root, 0)
	}
	
	// Average path length
	avgPathLen := 0.0
	for _, pl := range pathLengths {
	avgPathLen += pl
	}
	avgPathLen /= float64(len(pathLengths))
	
	// Normalize by expected path length for random forest
	c := ifm.expectedPathLength(ifm.sampleSize)
	anomalyScore := math.Pow(2, -avgPathLen/c)

	return anomalyScore > confidenceThreshold, anomalyScore, nil
}

// AnomallyScore computes the isolation-forest anomaly score for a feature vector.
func (ifm *IsolationForestModel) AnomallyScore(x []float64) float64 {
	if !ifm.trained || len(ifm.trees) == 0 {
		return 0.0
	}

	total := 0.0
	for _, tree := range ifm.trees {
		total += isolate(x, tree.root, 0)
	}
	avgPathLen := total / float64(len(ifm.trees))

	c := ifm.expectedPathLength(ifm.sampleSize)
	if c == 0 {
		return 0.0
	}
	return math.Pow(2, -avgPathLen/c)
}

// isolate computes path length for a point in isolation tree
func isolate(x []float64, node *Node, height int) float64 {
	// Base case: external node reached
	if node == nil {
		return float64(height)
	}
	if node.isExternal {
		return float64(height) + c(node.size)
	}

	// Recurse left or right based on split value
	if node.splitFeature < len(x) && x[node.splitFeature] < node.splitValue {
		return isolate(x, node.left, height+1)
	}

	return isolate(x, node.right, height+1)
}

// buildIsolationTree constructs an isolation tree, returning its root node.
func buildIsolationTree(data []MetricsSnapshot, height, maxHeight int) *Node {
	node := &Node{}
	
	// Determine feature dimensions (excluding timestamp)
	numFeatures := 8
	
	// Base case: leaf node reached
	if height >= maxHeight || len(data) <= 1 {
		node.isExternal = true
		node.size = len(data)
		return node
	}
	
	// Randomly select split feature
	featureIdx := int(time.Now().UnixNano() % int64(numFeatures))
	
	// Find min and max for selected feature
	minVal := data[0].CPUUtilization
	maxVal := data[0].CPUUtilization
	
	for i := range data {
		val := getFeatureValue(data[i], featureIdx)
		if val < minVal {
			minVal = val
		}
		if val > maxVal {
			maxVal = val
		}
	}
	
	// Random split value
	splitValue := minVal + randFloat(maxVal-minVal)
	
	node.splitFeature = featureIdx
	node.splitValue = splitValue
	
	// Split data into left and right partitions
	leftData := make([]MetricsSnapshot, 0)
	rightData := make([]MetricsSnapshot, 0)
	
	for i := range data {
		val := getFeatureValue(data[i], featureIdx)
		if val < splitValue {
			leftData = append(leftData, data[i])
		} else {
			rightData = append(rightData, data[i])
		}
	}
	
	// Recursively build subtrees
	node.left = buildIsolationTree(leftData, height+1, maxHeight)
	node.right = buildIsolationTree(rightData, height+1, maxHeight)
	
	return node
}

// Helper functions for Isolation Forest
func maxheight(x int) float64 {
	if x <= 1 {
		return 0
	}
	return 2.0 * math.Log2(float64(x-1))
}

func c(n int) float64 {
	if n <= 1 {
		return 0
	}
	return 2.0*(math.Log2(float64(n-1)) + 0.5772156649) - (2.0*float64(n-1)/float64(n))
}

func randFloat(max float64) float64 {
	if max <= 0 {
		return 0
	}
	return rand.Float64() * max
}

func getFeatureValue(snapshot MetricsSnapshot, featureIdx int) float64 {
	switch featureIdx {
	case 0:
		return snapshot.CPUUtilization
	case 1:
		return snapshot.MemoryUsage
	case 2:
		return snapshot.DiskIORead
	case 3:
		return snapshot.DiskIOWrite
	case 4:
		return snapshot.NetworkIn
	case 5:
		return snapshot.NetworkOut
	case 6:
		return float64(snapshot.Connections)
	default:
		return snapshot.ErrorRate
	}
}

func extractFeatures(snapshot MetricsSnapshot) []float64 {
	return []float64{
		snapshot.CPUUtilization,
		snapshot.MemoryUsage,
		snapshot.DiskIORead,
		snapshot.DiskIOWrite,
		snapshot.NetworkIn,
		snapshot.NetworkOut,
		float64(snapshot.Connections),
		snapshot.ErrorRate,
	}
}

func (ifm *IsolationForestModel) expectedPathLength(n int) float64 {
	return c(n)
}
