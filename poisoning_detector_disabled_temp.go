// Package patent implements the Poisoning Attack Detector and Byzantine-Resistant Aggregation System.
// This module provides federated learning security through statistical anomaly detection
// and robust aggregation algorithms that tolerate malicious client updates.
//
// Technical Innovation: Mahalanobis distance-based detection combined with 
// Krum/Multi-Krum/Median hybrid aggregation for Byzantine fault tolerance.
package patent

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// ==================== Section 3A: Poisoning Attack Detector (250 LOC) ====================

// FederatedUpdate represents a client's model update in federated learning
type FederatedUpdate struct {
	ClientID    string
	Weights     [][]float64
	Biases      []float64
	Iteration   int
	Timestamp   time.Time
	Confidence  float64
}

// StatisticalModel captures distribution statistics for anomaly detection
type StatisticalModel struct {
	mean        []float64
	variance    []float64
	stdDev      []float64
	covariance  [][]float64
	sampleCount int
	lastUpdate  time.Time
}

// NewStatisticalModel initializes empty statistical model
func NewStatisticalModel() *StatisticalModel {
	return &StatisticalModel{
		sampleCount: 0,
	}
}

// Update computes running statistics from training updates
func (sm *StatisticalModel) Update(updates []FederatedUpdate) {
	if len(updates) == 0 {
		return
	}
	
	// Extract weight dimensions
	numDimensions := len(updates[0].Weights)
	for _, layer := range updates[0].Weights {
		numDimensions += len(layer)
	}
	
	// Accumulate means
	means := make([]float64, numDimensions)
	for _, update := range updates {
		dim := 0
		for layerIdx := range update.Weights {
			for paramIdx := range update.Weights[layerIdx] {
				if dim < numDimensions {
					means[dim] += update.Weights[layerIdx][paramIdx]
					dim++
				}
			}
		}
	}
	
	for i := range means {
		means[i] /= float64(len(updates))
	}
	
	sm.mean = means
	sm.sampleCount += len(updates)
	sm.lastUpdate = time.Now()
}

// PoisoningDetector identifies malicious updates using statistical methods
type PoisoningDetector struct {
	benignUpdates    []FederatedUpdate
	statisticalModel *StatisticalModel
	anomalyThreshold float64
	detectionHistory []DetectionResult
	maxHistorySize   int
	mu               sync.RWMutex
}

// DetectionResult records detection decision and confidence
type DetectionResult struct {
	UpdateID       string
	IsPoisoned     bool
	Distance       float64
	Confidence     float64
	DecisionTime   time.Time
	Method         string
}

// NewPoisoningDetector creates attack detector instance
func NewPoisoningDetector(threshold float64) *PoisoningDetector {
	return &PoisoningDetector{
		benignUpdates:    make([]FederatedUpdate, 0),
		statisticalModel: NewStatisticalModel(),
		anomalyThreshold: threshold,
		detectionHistory: make([]DetectionResult, 0),
		maxHistorySize:   1000,
	}
}

// DetectPoison determines if an update is malicious using Mahalanobis distance
func (pd *PoisoningDetector) DetectPoison(update FederatedUpdate) bool {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	
	if len(pd.benignUpdates) < 3 {
		return false // Insufficient baseline
	}
	
	deviation := pd.computeDeviation(update, pd.benignUpdates)
	isPoisoned := deviation > pd.anomalyThreshold
	
	result := DetectionResult{
		UpdateID:     update.ClientID,
		IsPoisoned:   isPoisoned,
		Distance:     deviation,
		Confidence:   math.Min(deviation/pd.anomalyThreshold, 1.0),
		DecisionTime: time.Now(),
		Method:       "Mahalanobis",
	}
	
	pd.detectionHistory = append(pd.detectionHistory, result)
	if len(pd.detectionHistory) > pd.maxHistorySize {
		pd.detectionHistory = pd.detectionHistory[len(pd.detectionHistory)-pd.maxHistorySize:]
	}
	
	return isPoisoned
}

// computeDeviation calculates Mahalanobis distance between update and baseline
func (pd *PoisoningDetector) computeDeviation(update FederatedUpdate, baseline []FederatedUpdate) float64 {
	// Flatten weights into single vector
	updateVector := pd.flattenUpdate(update)
	baselineVectors := pd.flattenBatch(baseline)
	
	// Compute sample mean
	sampleMean := pd.computeMean(baselineVectors)
	
	// Compute covariance matrix
	covMatrix := pd.computeCovariance(baselineVectors, sampleMean)
	
	// Compute Mahalanobis distance
	distance := pd.mahalanobisDistance(updateVector, sampleMean, covMatrix)
	
	return distance
}

// flattenUpdate converts model update to flat feature vector
func (pd *PoisoningDetector) flattenUpdate(update FederatedUpdate) []float64 {
	size := 0
	for _, layer := range update.Weights {
		size += len(layer)
	}
	size += len(update.Biases)
	
	flat := make([]float64, size)
	idx := 0
	
	for _, layer := range update.Weights {
		for _, param := range layer {
			flat[idx] = param
			idx++
		}
	}
	
	for _, bias := range update.Biases {
		flat[idx] = bias
		idx++
	}
	
	return flat
}

// flattenBatch converts multiple updates to matrix form
func (pd *PoisoningDetector) flattenBatch(updates []FederatedUpdate) [][]float64 {
	vectors := make([][]float64, len(updates))
	for i, update := range updates {
		vectors[i] = pd.flattenUpdate(update)
	}
	return vectors
}

// computeMean calculates element-wise average of vectors
func (pd *PoisoningDetector) computeMean(vectors [][]float64) []float64 {
	if len(vectors) == 0 {
		return nil
	}
	
	dimension := len(vectors[0])
	mean := make([]float64, dimension)
	
	for _, vec := range vectors {
		for i, val := range vec {
			mean[i] += val
		}
	}
	
	n := float64(len(vectors))
	for i := range mean {
		mean[i] /= n
	}
	
	return mean
}

// computeCovariance calculates covariance matrix from sample vectors
func (pd *PoisoningDetector) computeCovariance(vectors [][]float64, mean []float64) [][]float64 {
	dimension := len(mean)
	cov := make([][]float64, dimension)
	
	for i := range cov {
		cov[i] = make([]float64, dimension)
	}
	
	// Center data
	centered := make([][]float64, len(vectors))
	for i, vec := range vectors {
		centered[i] = make([]float64, dimension)
		for j := range vec {
			centered[i][j] = vec[j] - mean[j]
		}
	}
	
	// Compute covariance
	n := float64(len(vectors) - 1)
	if n <= 0 {
		n = 1.0
	}
	
	for i := 0; i < dimension; i++ {
		for j := 0; j < dimension; j++ {
			sum := 0.0
			for k := range vectors {
				sum += centered[k][i] * centered[k][j]
			}
			cov[i][j] = sum / n
		}
	}
	
	return cov
}

// mahalanobisDistance computes D^2 = (x-μ)^T Σ^(-1) (x-μ)
func (pd *PoisoningDetector) mahalanobisDistance(x, mean []float64, cov [][]float64) float64 {
	dimension := len(x)
	
	// Compute x - μ
	diff := make([]float64, dimension)
	for i := range diff {
		diff[i] = x[i] - mean[i]
	}
	
	// Invert covariance matrix using Gauss-Jordan elimination
	invCov := pd.invertMatrix(cov)
	
	// Multiply by inverse
	rightProduct := make([]float64, dimension)
	for i := range rightProduct {
		for j := 0; j < dimension; j++ {
			rightProduct[i] += invCov[i][j] * diff[j]
		}
	}
	
	// Final dot product: diff^T * invCov * diff
	distance := 0.0
	for i := range diff {
		distance += diff[i] * rightProduct[i]
	}
	
	return math.Sqrt(math.Max(0, distance))
}

// invertMatrix performs matrix inversion using Gauss-Jordan elimination
func (pd *PoisoningDetector) invertMatrix(matrix [][]float64) [][]float64 {
	dimension := len(matrix)
	
	// Create augmented matrix [A|I]
	augmented := make([][]float64, dimension)
	for i := range augmented {
		augmented[i] = make([]float64, dimension*2)
		
		// Copy original matrix
		for j := 0; j < dimension; j++ {
			augmented[i][j] = matrix[i][j]
		}
		
		// Identity matrix on right side
		augmented[i][dimension+i] = 1.0
	}
	
	// Gauss-Jordan elimination
	for i := 0; i < dimension; i++ {
		// Find pivot
		maxVal := math.Abs(augmented[i][i])
		maxRow := i
		
		for k := i + 1; k < dimension; k++ {
			if math.Abs(augmented[k][i]) > maxVal {
				maxVal = math.Abs(augmented[k][i])
				maxRow = k
			}
		}
		
		// Swap rows
		if maxRow != i {
			augmented[i], augmented[maxRow] = augmented[maxRow], augmented[i]
		}
		
		// Normalize pivot row
		pivot := augmented[i][i]
		if math.Abs(pivot) < 1e-10 {
			pivot = 1e-10 // Avoid division by zero
		}
		
		for j := 0; j < dimension*2; j++ {
			augmented[i][j] /= pivot
		}
		
		// Eliminate column
		for k := 0; k < dimension; k++ {
			if k != i {
				factor := augmented[k][i]
				for j := 0; j < dimension*2; j++ {
					augmented[k][j] -= factor * augmented[i][j]
				}
			}
		}
	}
	
	// Extract inverse matrix
	inverse := make([][]float64, dimension)
	for i := range inverse {
		inverse[i] = make([]float64, dimension)
		for j := 0; j < dimension; j++ {
			inverse[i][j] = augmented[i][dimension+j]
		}
	}
	
	return inverse
}

// AddBenignUpdate records benign update for baseline maintenance
func (pd *PoisoningDetector) AddBenignUpdate(update FederatedUpdate) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	
	pd.benignUpdates = append(pd.benignUpdates, update)
	pd.statisticalModel.Update(pd.benignUpdates)
	
	if len(pd.benignUpdates) > 500 {
		pd.benignUpdates = pd.benignUpdates[len(pd.benignUpdates)-500:]
	}
}

// GetDetectionHistory returns historical detection results
func (pd *PoisoningDetector) GetDetectionHistory() []DetectionResult {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	
	history := make([]DetectionResult, len(pd.detectionHistory))
	copy(history, pd.detectionHistory)
	
	return history
}

// GetAnomalyThreshold returns current detection threshold
func (pd *PoisoningDetector) GetAnomalyThreshold() float64 {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.anomalyThreshold
}

// SetAnomalyThreshold updates detection sensitivity threshold
func (pd *PoisoningDetector) SetAnomalyThreshold(threshold float64) {
	if threshold <= 0 || threshold > 10 {
		panic("threshold must be in (0, 10]")
	}
	
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.anomalyThreshold = threshold
}

// ==================== Section 3B: Byzantine-Resistant Aggregation (250 LOC) ===================

// ByzantineResistantAggregator implements robust aggregation strategies
type ByzantineResistantAggregator struct {
	algorithms           []string
	aggregationAlgorithm string
	maxByzantineRatio    float64
	minRequiredUpdates   int
	currentMethod        string
	mu                   sync.RWMutex
}

// ModelWeights represents complete model parameter set
type ModelWeights struct {
	Layers [][]float64
	Biases []float64
}

// NewByzantineAggregator creates robust aggregator instance
func NewByzantineAggregator(algorithms []string, maxByzantineRatio float64) *ByzantineResistantAggregator {
	return &ByzantineResistantAggregator{
		algorithms:           algorithms,
		aggregationAlgorithm: "Krum",
		maxByzantineRatio:    maxByzantineRatio,
		minRequiredUpdates:   3,
		currentMethod:        "",
	}
}

// Aggregate combines multiple updates robustly against Byzantine attacks
func (bra *ByzantineResistantAggregator) Aggregate(updates []FederatedUpdate) (ModelWeights, error) {
	bra.mu.Lock()
	defer bra.mu.Unlock()
	
	if len(updates) < bra.minRequiredUpdates {
		return ModelWeights{}, errors.New("insufficient updates for aggregation")
	}
	
	switch bra.aggregationAlgorithm {
	case "Krum":
		return bra.krumAggregate(updates)
	case "Multi-Krum":
		return bra.multiKrumAggregate(updates)
	case "Median":
		return bra.medianAggregate(updates)
	case "TrimmedMean":
		return bra.trimmedMeanAggregate(updates)
	default:
		return bra.simpleAverage(updates)
	}
}

// setAlgorithm switches between available aggregation strategies
func (bra *ByzantineResistantAggregator) setAlgorithm(name string) error {
	valid := false
	for _, alg := range bra.algorithms {
		if alg == name {
			valid = true
			break
		}
	}
	
	if !valid {
		return fmt.Errorf("unsupported algorithm: %s", name)
	}
	
	bra.aggregationAlgorithm = name
	bra.currentMethod = name
	return nil
}

// krumAggregate selects the most central update as aggregate
func (bra *ByzantineResistantAggregator) krumAggregate(updates []FederatedUpdate) (ModelWeights, error) {
	n := len(updates)
	f := int(float64(n)*bra.maxByzantineRatio)
	if f < 1 {
		f = 1
	}
	
	// Compute pairwise distances between all updates
	distances := make([][]float64, n)
	for i := range distances {
		distances[i] = make([]float64, n)
	}
	
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dist := bra.computeWeightedDistance(updates[i], updates[j])
			distances[i][j] = dist
			distances[j][i] = dist
		}
	}
	
	// Compute selection scores (sum of n-f-1 closest neighbors)
	scores := make([]float64, n)
	k := n - f - 2
	
	for i := 0; i < n; i++ {
		scores[i] = 0
		sorted := slices.Clone(distances[i])
		slices.Sort(sorted)
		
		for j := 0; j < k; j++ {
			scores[i] += sorted[j]
		}
	}
	
	// Select update with minimum score
	minScore := scores[0]
	bestIdx := 0
	
	for i := 1; i < n; i++ {
		if scores[i] < minScore {
			minScore = scores[i]
			bestIdx = i
		}
	}
	
	return ModelWeights{
		Layers: updates[bestIdx].Weights,
		Biases: updates[bestIdx].Biases,
	}, nil
}

// multiKrumAggregate combines k+2 best selections for enhanced robustness
func (bra *ByzantineResistantAggregator) multiKrumAggregate(updates []FederatedUpdate) (ModelWeights, error) {
	n := len(updates)
	q := 2
	f := int(float64(n-1)*bra.maxByzantineRatio)
	
	// Compute pairwise distances
	distances := make([][]float64, n)
	for i := range distances {
		distances[i] = make([]float64, n)
	}
	
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dist := bra.computeWeightedDistance(updates[i], updates[j])
			distances[i][j] = dist
			distances[j][i] = dist
		}
	}
	
	// Multi-Krum selection
	selected := make([]int, q)
	used := make(map[int]bool)
			
	for s := 0; s < q; s++ {
		minScore := math.Inf(1)
		bestIdx := -1
				
		for i := 0; i < n; i++ {
			if used[i] {
				continue
			}
					
			score := 0
			count := 0
			for j := 0; j < n; j++ {
				if i != j && !used[j] {
					score += distances[i][j]
					count++
				}
			}
					
			if count > 0 && score/float64(count) < minScore {
				minScore = score / float64(count)
				bestIdx = i
			}
		}
				
		if bestIdx >= 0 {
			selected[s] = bestIdx
			used[bestIdx] = true
		}
	}
			
	// Average selected updates
	result := bra.averageUpdates(updates[:len(selected)])
	
	return result, nil
}

// medianAggregate computes per-element medians across updates
func (bra *ByzantineResistantAggregator) medianAggregate(updates []FederatedUpdate) (ModelWeights, error) {
	n := len(updates)
	if n == 0 {
		return ModelWeights{}, errors.New("no updates to aggregate")
	}
	
	// Flatten all weights for median computation
	allParams := pd.extractAllParameters(updates)
	
	medians := make([]float64, len(allParams))
	for i, params := range allParams {
		sortable := make([]float64, len(params))
		copy(sortable, params)
		slices.Sort(sortable)
		
		if len(sortable)%2 == 0 {
			medians[i] = (sortable[len(sortable)/2-1] + sortable[len(sortable)/2]) / 2
		} else {
			medians[i] = sortable[len(sortable)/2]
		}
	}
	
	return convertToModelWeights(medians), nil
}

// trimmedMeanAggregate removes extreme values before averaging
func (bra *ByzantineResistantAggregator) trimmedMeanAggregate(updates []FederatedUpdate) (ModelWeights, error) {
	allParams := pd.extractAllParameters(updates)
	
	trimmedMean := make([]float64, len(allParams))
	trimRatio := 0.1
	
	for i, params := range allParams {
		sortable := make([]float64, len(params))
		copy(sortable, params)
		slices.Sort(sortable)
		
		trimCount := int(float64(len(sortable)) * trimRatio)
		if trimCount < 1 {
			trimCount = 1
		}
		
		start := trimCount
		end := len(sortable) - trimCount
		
		sum := 0.0
		for j := start; j < end; j++ {
			sum += sortable[j]
		}
		
		trimmedMean[i] = sum / float64(end-start)
	}
	
	return convertToModelWeights(trimmedMean), nil
}

// simpleAverage performs basic arithmetic mean aggregation
func (bra *ByzantineResistantAggregator) simpleAverage(updates []FederatedUpdate) (ModelWeights, error) {
	if len(updates) == 0 {
		return ModelWeights{}, errors.New("empty updates list")
	}
	
	firstUpdate := updates[0]
	numLayers := len(firstUpdate.Weights)
	
	result := ModelWeights{
		Layers: make([][]float64, numLayers),
		Biases: make([]float64, len(firstUpdate.Biases)),
	}
	
	// Initialize sums
	for i := range result.Layers {
		result.Layers[i] = make([]float64, len(firstUpdate.Weights[i]))
	}
	
	// Sum all parameters
	for _, update := range updates {
		for l := 0; l < numLayers; l++ {
			for p := range result.Layers[l] {
				result.Layers[l][p] += update.Weights[l][p]
			}
		}
		for b := range result.Biases {
			result.Biases[b] += update.Biases[b]
		}
	}
	
	// Average
	n := float64(len(updates))
	for l := range result.Layers {
		for p := range result.Layers[l] {
			result.Layers[l][p] /= n
		}
	}
	for b := range result.Biases {
		result.Biases[b] /= n
	}
	
	return result, nil
}

// computeWeightedDistance calculates Euclidean distance between two updates
func computeWeightedDistance(u1, u2 FederatedUpdate) float64 {
	v1 := pd.flattenUpdate(u1)
	v2 := pd.flattenUpdate(u2)
	
	distance := 0.0
	for i := range v1 {
		diff := v1[i] - v2[i]
		distance += diff * diff
	}
	
	return math.Sqrt(distance)
}

// extractAllParameters gathers all model parameters across updates
func extractAllParameters(updates []FederatedUpdate) [][]float64 {
	params := make([][]float64, 0)
	
	for _, update := range updates {
		flat := bra.flattenUpdate(update)
		params = append(params, flat)
	}
	
	return params
}

// averageUpdates computes weighted average of subset updates
func averageUpdates(updates []FederatedUpdate) ModelWeights {
	flatSums := make([]float64, 0)
	
	for _, update := range updates {
		flat := bra.flattenUpdate(update)
		for _, val := range flat {
			if len(flatSums) == 0 {
				flatSums = append(flatSums, val)
			} else {
				flatSums[len(flatSums)-1] += val
			}
		}
	}
	
	return bra.convertToModelWeights(flatSums)
}

// convertToModelWeights transforms flat vector back to model structure
func convertToModelWeights(flat []float64) ModelWeights {
	result := ModelWeights{
		Layers: make([][]float64, 0),
		Biases: make([]float64, 0),
	}
	
	current := 0
	for current < len(flat) {
		layerSize := rand.Intn(10) + 5
		if current+layerSize > len(flat) {
			layerSize = len(flat) - current
		}
		
		layer := make([]float64, layerSize)
		copy(layer, flat[current:current+layerSize])
		result.Layers = append(result.Layers, layer)
		current += layerSize
	}
	
	return result
}

// GetCurrentAlgorithm returns currently selected aggregation method
func (bra *ByzantineResistantAggregator) GetCurrentAlgorithm() string {
	bra.mu.RLock()
	defer bra.mu.RUnlock()
	return bra.aggregationAlgorithm
}

// SwitchAlgorithm changes aggregation strategy at runtime
func (bra *ByzantineResistantAggregator) SwitchAlgorithm(name string) error {
	return bra.setAlgorithm(name)
}

// ValidateConvergence checks if aggregated weights have stabilized
func (bra *ByzantineResistantAggregator) ValidateConvergence(previous, current ModelWeights) bool {
	distance := bra.weightDistance(previous, current)
	return distance < 0.01
}

// weightDistance measures parameter space distance between two models
func weightDistance(w1, w2 ModelWeights) float64 {
	totalDiff := 0.0
	
	for l := 0; l < len(w1.Layers); l++ {
		for i := range w1.Layers[l] {
			diff := w1.Layers[l][i] - w2.Layers[l][i]
			totalDiff += diff * diff
		}
	}
	
	for i := range w1.Biases {
		diff := w1.Biases[i] - w2.Biases[i]
		totalDiff += diff * diff
	}
	
	return math.Sqrt(totalDiff)
}

// GetRobustnessMetrics returns quality metrics for aggregation
func GetRobustnessMetrics(updates []FederatedUpdate, threshold float64) map[string]interface{} {
	metrics := make(map[string]interface{})
	
	if len(updates) < 2 {
		metrics["byzantine_ratio"] = 0.0
		metrics["detection_count"] = 0
		return metrics
	}
	
	detectedCount := 0
	for _, update := range updates {
		detector := NewPoisoningDetector(threshold)
		if detector.DetectPoison(update) {
			detectedCount++
		}
	}
	
	metrics["byzantine_ratio"] = float64(detectedCount) / float64(len(updates))
	metrics["detection_count"] = detectedCount
	metrics["total_updates"] = len(updates)
	
	return metrics
}
