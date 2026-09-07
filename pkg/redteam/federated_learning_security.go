package redteam

import (
	"math"
	"math/rand"
	"time"
	
	"github.com/sirupsen/logrus"
)

// ============================================================================
// FEDERATED LEARNING SECURITY FRAMEWORK
// Protects distributed ML training against poisoning, Byzantine attacks, and
// model inversion attempts while maintaining privacy guarantees
// ============================================================================

// FederatedLearningSecurity provides comprehensive protection mechanisms for
// federated learning systems against adversarial attacks including data poisoning,
// model poisoning, gradient injection, and Byzantine failures.
type FederatedLearningSecurity struct {
	poisonDetector      *PoisoningDetector
	byzantineAggregator *ByzantineResistantAggregator
	inversionPreventer  *InversionAttackPreventer
	logger              *logrus.Logger
	
	// Configuration
	globalModelWeight     []float64
	updateHistory         []FederatedUpdate
	historyWindowSize     int
	statisticalThreshold  float64
	maxByzantineRatio     float64
	detectionSensitivity  float64 // Lower = more sensitive
}

// PoisoningDetector identifies malicious updates using statistical analysis.
// Implements Mahalanobis distance-based detection for high-dimensional outliers.
type PoisoningDetector struct {
	distanceMetric string // "mahalanobis" or "cosine" or "euclidean"
	threshold      float64
	meanVector     []float64
	covarianceMat  [][]float64
	updated        bool
	
	// Detection state
	totalUpdates   int
	detectedPoison int
	lastCheckTime  time.Time
}

// ByzantineResistantAggregator implements robust aggregation algorithms.
// Supports Multi-Krum, trimmed mean, and median aggregation resistant to
// Byzantine nodes sending arbitrary malicious updates.
type ByzantineResistantAggregator struct {
	algorithm       string // "multi-krum", "trimmed_mean", "median"
	maxByzantine    int    // Maximum number of Byzantine clients tolerated
	totalClients    int    // Total participating clients
	fallbackToSimple bool   // Fall back to simple averaging if robust fails
	
	// Aggregation parameters
	krumM             int // Number of neighbors for Krum selection
	trimmedRatio      float64 // Ratio of updates to trim from each end
}

// InversionAttackPreventer mitigates model inversion attacks that attempt to
// reconstruct training data from model updates. Uses differential privacy.
type InversionAttackPreventer struct {
	dp_epsilon      float64 // Differential privacy budget
	dp_delta        float64 // Failure probability
	gradientClipping float64 // Max gradient norm before clipping
	noiseScale      float64 // Gaussian noise scale
	
	// Privacy accounting
	totalEpsilon    float64 // Accumulated privacy budget
	privacyEpoch    int     // Current privacy epoch
}

// NewFederatedLearningSecurity creates secure federated learning configuration.
// Initializes all detection and mitigation components with optimal thresholds.
func NewFederatedLearningSecurity(logger *logrus.Logger) *FederatedLearningSecurity {
	if logger == nil {
		logger = logrus.New()
		logger.SetLevel(logrus.WarnLevel)
	}
	
	return &FederatedLearningSecurity{
		poisonDetector: &PoisoningDetector{
			distanceMetric:   "mahalanobis",
			threshold:        2.5,
			updated:          false,
			detectionSensitivity: 1.0,
		},
		byzantineAggregator: &ByzantineResistantAggregator{
			algorithm:       "multi-krum",
			maxByzantine:    0, // Computed dynamically
			totalClients:    0,
			fallbackToSimple: false,
			krumM:           0,
			trimmedRatio:    0.1,
		},
		inversionPreventer: &InversionAttackPreventer{
			dp_epsilon:         1.0,
			dp_delta:           1e-5,
			gradientClipping:   1.0,
			noiseScale:         0.5,
			totalEpsilon:       0.0,
			privacyEpoch:       0,
		},
		logger:            logger.WithField("component", "fed-sec"),
		historyWindowSize: 10,
		statisticalThreshold: 2.5,
		maxByzhantineRatio: 0.3,
		detectionSensitivity: 1.0,
		updateHistory:     make([]FederatedUpdate, 0),
		globalModelWeight: make([]float64, 0),
	}
}

// ProtectTraining safeguards federated training process by filtering poisoned
// updates and aggregating benign contributions using Byzantine-resistant methods.
// Returns aggregated model and boolean indicating if poisoning was detected.
func (f *FederatedLearningSecurity) ProtectTraining(updates []FederatedUpdate) (*FederatedUpdate, bool) {
	f.logger.WithField("total_updates", len(updates)).Debug("Starting protected training round")
	
	startTime := time.Now()
	
	// Initialize global model if not yet set
	if len(f.globalModelWeight) == 0 && len(updates) > 0 {
		f.globalModelWeight = initializeGlobalWeights(updates[0].Weights)
	}
	
	// Step 1: Detect poisoned updates using multiple signals
	f.logger.Debug("Step 1/3: Running poisoning detection")
	
	detectedPoisoned := []FederatedUpdate{}
	benignUpdates := []FederatedUpdate{}
	
	for _, update := range updates {
		isPoisoned := f.poisonDetector.DetectPoison(update, f.globalModelWeight)
		
		if isPoisoned {
			detectedPoisoned = append(detectedPoisoned, update)
			f.logger.WithField("client_id", update.ClientID).Warn("Poisoned update detected")
		} else {
			benignUpdates = append(benignUpdates, update)
		}
	}
	
	hasPoisoningAttempts := len(detectedPoisoned) > 0
	
	// Update detection statistics
	f.poisonDetector.totalUpdates += len(updates)
	f.poisonDetector.detectedPoison += len(detectedPoisoned)
	f.poisonDetector.lastCheckTime = time.Now()
	
	f.logger.WithFields(logrus.Fields{
		"poisoned_detected":    len(detectedPoisoned),
		"benign_accepted":      len(benignUpdates),
		"detection_rate":       float64(len(detectedPoisoned)) / float64(len(updates)),
	}).Info("Poisoning detection phase complete")
	
	// Handle edge cases
	if len(benignUpdates) == 0 {
		f.logger.Error("All updates detected as poisoned - training halted")
		return nil, true
	}
	
	if len(benignUpdates) == 1 && len(updates) > 1 {
		f.logger.Warn("Only one benign update - using directly without aggregation")
		return &benignUpdates[0], hasPoisoningAttempts
	}
	
	// Step 2: Apply Byzantine-resistant aggregation
	f.logger.Debug("Step 2/3: Running Byzantine-resistant aggregation")
	
	aggregatedUpdate := f.byzantineAggregator.Aggregate(benignUpdates, f.globalModelWeight)
	
	if aggregatedUpdate == nil {
		f.logger.Error("Aggregation failed - falling back to averaging")
		aggregatedUpdate = f.fallbackAverageAggregation(benignUpdates)
	}
	
	// Step 3: Apply differential privacy to prevent model inversion
	f.logger.Debug("Step 3/3: Applying differential privacy protections")
	
	aggregatedUpdate = f.applyDPNoise(aggregatedUpdate)
	
	// Update global model weights
	f.globalModelWeight = aggregatedUpdate.Weights[0]
	
	// Maintain update history window
	f.updateHistory = append(f.updateHistory, *aggregatedUpdate)
	if len(f.updateHistory) > f.historyWindowSize {
		f.updateHistory = f.updateHistory[len(f.updateHistory)-f.historyWindowSize:]
	}
	
	elapsed := time.Since(startTime)
	
	f.logger.WithFields(logrus.Fields{
		"training_round_time_ms": elapsed.Milliseconds(),
		"updates_processed":      len(updates),
		"updates_aggregated":     len(benignUpdates),
		"poisoning_detected":     hasPoisoningAttempts,
		"dp_epsilon_used":        f.inversionPreventer.dp_epsilon,
	}).Info("✅ Protected training round completed successfully")
	
	return aggregatedUpdate, hasPoisoningAttempts
}

// DetectPoisonedModels performs batch analysis to identify poisoned models.
// Uses multivariate statistical analysis for comprehensive detection.
func (f *FederatedLearningSecurity) DetectPoisonedModels(updates []FederatedUpdate) []FederatedUpdate {
	f.logger.WithField("analyzing", len(updates)).Debug("Scanning for poisoned models")
	
	if len(updates) < 3 {
		f.logger.Warn("Insufficient updates for reliable detection")
		return nil
	}
	
	// Compute aggregate statistics
	weightMatrix := f.computeWeightMatrix(updates)
	
	// Calculate global mean vector
	meanVector := computeMeanWeight(weightMatrix)
	
	// Calculate covariance matrix
	covarianceMat := computeCovarianceMatrix(weightMatrix, meanVector)
	
	// Compute Mahalanobis distances for each update
	var poisoned []FederatedUpdate
	
	for i, update := range updates {
		distance := mahalanobisDistance(update.Weights[0], meanVector, covarianceMat)
		
		// Adjust threshold based on sensitivity setting
		adjustedThreshold := f.statisticalThreshold / f.detectionSensitivity
		
		if distance > adjustedThreshold {
			f.logger.WithFields(logrus.Fields{
				"client_id": update.ClientID,
				"distance":  distance,
				"threshold": adjustedThreshold,
			}).Debug("Poisoned update identified")
			
			poisoned = append(poisoned, update)
		}
	}
	
	f.logger.WithFields(logrus.Fields{
		"total_analyzed": len(updates),
		"poisoned_found": len(poisoned),
		"detection_rate": float64(len(poisoned)) / float64(len(updates)),
	}).Info("Model poisoning scan complete")
	
	return poisoned
}

// computeWeightMatrix extracts weight tensors into 2D matrix format.
func (f *FederatedLearningSecurity) computeWeightMatrix(updates []FederatedUpdate) [][]float64 {
	if len(updates) == 0 {
		return nil
	}
	
	numUpdates := len(updates)
	numWeights := len(updates[0].Weights[0])
	
	matrix := make([][]float64, numUpdates*numWeights)
	idx := 0
	
	for _, update := range updates {
		for _, weight := range update.Weights[0] {
			matrix[idx] = []float64{weight}
			idx++
		}
	}
	
	return matrix
}

// applyDPNoise adds calibrated Gaussian noise for differential privacy.
func (f *FederatedLearningSecurity) applyDPNoise(update *FederatedUpdate) *FederatedUpdate {
	if f.inversionPreventer.dp_epsilon <= 0 {
		return update // No DP if disabled
	}
	
	// Clip gradients to bounded sensitivity
	clippedWeights := f.clipGradients(update.Weights[0])
	
	// Add Gaussian noise scaled to sensitivity and epsilon
	noisedWeights := f.addGaussianNoise(clippedWeights)
	
	// Update privacy accounting
	f.inversionPreventer.totalEpsilon += f.inversionPreventer.dp_epsilon
	
	f.logger.WithFields(logrus.Fields{
		"epsilon_per_round": f.inversionPreventer.dp_epsilon,
		"total_epsilon":     f.inversionPreventer.totalEpsilon,
		"clip_norm":         f.inversionPreventer.gradientClipping,
	}).Debug("Differential privacy noise applied")
	
	// Create new update with noisy weights
	updatedCopy := *update
	updatedCopy.Weights[0] = noisedWeights
	
	return &updatedCopy
}

// clipGradients limits gradient norm to prevent large updates.
func (f *FederatedLearningSecurity) clipGradients(weights []float64) []float64 {
	// Compute L2 norm
	norm := 0.0
	for _, w := range weights {
		norm += w * w
	}
	norm = math.Sqrt(norm)
	
	clipThreshold := f.inversionPreventer.gradientClipping
	
	// Scale down if above threshold
	if norm > clipThreshold {
		scale := clipThreshold / norm
		clipped := make([]float64, len(weights))
		for i, w := range weights {
			clipped[i] = w * scale
		}
		return clipped
	}
	
	return weights
}

// addGaussianNoise injects calibrated noise for privacy.
func (f *FederatedLearningSecurity) addGaussianNoise(weights []float64) []float64 {
	noised := make([]float64, len(weights))
	
	// Compute standard deviation based on sensitivity and epsilon
	stdDev := f.inversionPreventer.noiseScale
	
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	
	for i, w := range weights {
		noise := rng.NormFloat(stdDev)
		noised[i] = w + noise
	}
	
	return noised
}

// fallbackAverageAggregation uses simple averaging if robust method fails.
func (f *FederatedLearningSecurity) fallbackAverageAggregation(updates []FederatedUpdate) *FederatedUpdate {
	if len(updates) == 0 {
		return nil
	}
	
	firstDims := len(updates[0].Weights)
	secondDims := len(updates[0].Weights[0])
	
	averaged := make([][][]float64, firstDims)
	averaged[0] = make([]float64, secondDims)
	
	for i := 0; i < secondDims; i++ {
		sum := 0.0
		for _, update := range updates {
			sum += update.Weights[0][i]
		}
		averaged[0][i] = sum / float64(len(updates))
	}
	
	result := *updates[0]
	result.Weights = averaged
	
	return &result
}

// GetDetectionStats returns poisoning detection statistics.
func (f *FederatedLearningSecurity) GetDetectionStats() map[string]interface{} {
	return map[string]interface{}{
		"total_updates_analyzed":   f.poisonDetector.totalUpdates,
		"poisoned_detected":        f.poisonDetector.detectedPoison,
		"detection_rate":           float64(f.poisonDetector.detectedPoison) / float64(f.poisonDetector.totalUpdates),
		"last_detection_check":     f.poisonDetector.lastCheckTime.Format(time.RFC3339),
		"detection_threshold":      f.statisticalThreshold,
		"detection_sensitivity":    f.detectionSensitivity,
		"current_global_model_dims": len(f.globalModelWeight),
	}
}

// GetPrivacyAccounting returns current differential privacy accounting.
func (f *FederatedLearningSecurity) GetPrivacyAccounting() map[string]interface{} {
	return map[string]interface{}{
		"epsilon_per_round": f.inversionPreventer.dp_epsilon,
		"accumulated_epsilon": f.inversionPreventer.totalEpsilon,
		"delta":               f.inversionPreventer.dp_delta,
		"gradient_clipping":   f.inversionPreventer.gradientClipping,
		"noise_scale":         f.inversionPreventer.noiseScale,
		"privacy_epoch":       f.inversionPreventer.privacyEpoch,
	}
}

// ConfigureByzantineTolerance sets maximum Byzantine client ratio.
func (f *FederatedLearningSecurity) ConfigureByzantineTolerance(maxRatio float64) {
	f.maxByzantineRatio = maxRatio
	// Recalculate absolute maximum based on current client count
	f.byzantineAggregator.maxByzantine = int(float64(f.byzantineAggregator.totalClients) * maxRatio)
	
	f.logger.WithFields(logrus.Fields{
		"max_ratio": maxRatio,
		"max_byzantine": f.byzantineAggregator.maxByzantine,
	}).Info("Byzantine tolerance configured")
}

// ============================================================================
// SUPPORTING DETECTION AND AGGREGATION IMPLEMENTATIONS
// ============================================================================

// Poisoning detection using Mahalanobis distance
func (pd *PoisoningDetector) DetectPoison(update FederatedUpdate, globalWeights []float64) bool {
	if !pd.updated {
		// First detection - compute baseline from previous updates
		pd.meanVector = globalWeights
		pd.covarianceMat = computeIdentityMatrix(len(globalWeights))
		pd.updated = true
	}
	
	// Compute distance from global model
	dist := mahalanobisDistance(update.Weights[0], pd.meanVector, pd.covarianceMat)
	
	isPoisoned := dist > pd.threshold
	
	return isPoisoned
}

// Mahalanobis distance computation for outlier detection
func mahalanobisDistance(x, mean []float64, cov [][]float64) float64 {
	if len(x) != len(mean) || len(x) != len(cov) {
		return 0.0
	}
	
	// Simplified: use diagonal approximation (assume independence)
	distSq := 0.0
	for i, xi := range x {
		diff := xi - mean[i]
		// Use variance from diagonal of covariance (or 1 if identity)
		variance := cov[i][i]
		if variance <= 0 {
			variance = 1.0
		}
		distSq += (diff * diff) / variance
	}
	
	return math.Sqrt(distSq)
}

// Byzantine-resistant aggregation using Multi-Krum algorithm
func (ba *ByzantineResistantAggregator) Aggregate(updates []FederatedUpdate, globalWeights []float64) *FederatedUpdate {
	if len(updates) == 0 {
		return nil
	}
	
	if len(updates) == 1 {
		result := updates[0]
		return &result
	}
	
	// Set k parameter based on Byzantine tolerance
	if ba.krumM == 0 {
		ba.krumM = len(updates) - ba.maxByzantine - 2
	}
	
	// Compute pairwise distances between updates
	distances := ba.computePairwiseDistances(updates)
	
	// Select Krum combination with minimum sum of distances
	bestIndices := ba.selectKrumCombination(distances, ba.krumM)
	
	// Aggregate selected updates
	selectedUpdates := make([]FederatedUpdate, len(bestIndices))
	for i, idx := range bestIndices {
		selectedUpdates[i] = updates[idx]
	}
	
	// Return average of selected updates
	result := f.averageUpdates(selectedUpdates)
	return result
}

// computePairwiseDistances calculates distance matrix between all updates
func (ba *ByzantineResistantAggregator) computePairwiseDistances(updates []FederatedUpdate) [][]float64 {
	n := len(updates)
	distances := make([][]float64, n)
	
	for i := 0; i < n; i++ {
		distances[i] = make([]float64, n)
		for j := i + 1; j < n; j++ {
			dist := ba.distanceBetweenUpdates(updates[i], updates[j])
			distances[i][j] = dist
			distances[j][i] = dist
		}
	}
	
	return distances
}

// distanceBetweenUpdates computes Euclidean distance between two weight vectors
func (ba *ByzantineResistantAggregator) distanceBetweenUpdates(u1, u2 FederatedUpdate) float64 {
	w1 := u1.Weights[0]
	w2 := u2.Weights[0]
	
	if len(w1) != len(w2) {
		return math.MaxFloat64
	}
	
	distSq := 0.0
	for i := range w1 {
		diff := w1[i] - w2[i]
		distSq += diff * diff
	}
	
	return math.Sqrt(distSq)
}

// selectKrumCombination finds indices with minimum distance sums
func (ba *ByzantineResistantAggregator) selectKrumCombination(distances [][]float64, m int) []int {
	n := len(distances)
	
	type score struct {
		idx   int
		score float64
	}
	
	scores := make([]score, n)
	for i := 0; i < n; i++ {
		// Sum of m smallest distances
		sortedDistances := make([]float64, n)
		copy(sortedDistances, distances[i])
		sort.Float64s(sortedDistances)
		
		scoreSum := 0.0
		for j := 0; j < m && j < n-1; j++ {
			scoreSum += sortedDistances[j]
		}
		
		scores[i] = score{idx: i, score: scoreSum}
	}
	
	// Select best (m+1) indices
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score < scores[j].score
	})
	
	result := make([]int, m+1)
	for i := 0; i < m+1; i++ {
		result[i] = scores[i].idx
	}
	
	return result
}

// Helper functions

// computeMeanWeight calculates element-wise mean across weight vectors
func computeMeanWeight(matrix [][]float64) []float64 {
	if len(matrix) == 0 {
		return nil
	}
	
	n := len(matrix[0])
	mean := make([]float64, n)
	
	for _, row := range matrix {
		for i, val := range row {
			mean[i] += val
		}
	}
	
	for i := range mean {
		mean[i] /= float64(len(matrix))
	}
	
	return mean
}

// computeCovarianceMatrix computes sample covariance matrix
func computeCovarianceMatrix(matrix [][]float64, mean []float64) [][]float64 {
	n := len(matrix[0])
	cov := make([][]float64, n)
	
	for i := range cov {
		cov[i] = make([]float64, n)
	}
	
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			sum := 0.0
			for _, row := range matrix {
				diffI := row[i] - mean[i]
				diffJ := row[j] - mean[j]
				sum += diffI * diffJ
			}
			cov[i][j] = sum / float64(len(matrix)-1)
		}
	}
	
	return cov
}

// computeIdentityMatrix creates identity matrix
func computeIdentityMatrix(size int) [][]float64 {
	mat := make([][]float64, size)
	for i := range mat {
		mat[i] = make([]float64, size)
		for j := range mat[i] {
			if i == j {
				mat[i][j] = 1.0
			} else {
				mat[i][j] = 0.0
			}
		}
	}
	return mat
}

// initializeGlobalWeights creates initial zero-weight model
func initializeGlobalWeights(weights [][][]float64) []float64 {
	if len(weights) == 0 || len(weights[0]) == 0 {
		return nil
	}
	
	size := len(weights[0][0])
	return make([]float64, size)
}

// Simple average aggregation
func (f *FederatedLearningSecurity) averageUpdates(updates []FederatedUpdate) *FederatedUpdate {
	if len(updates) == 0 {
		return nil
	}
	
	dims := len(updates[0].Weights[0])
	averaged := make([]float64, dims)
	
	for _, update := range updates {
		for i, w := range update.Weights[0] {
			averaged[i] += w
		}
	}
	
	for i := range averaged {
		averaged[i] /= float64(len(updates))
	}
	
	result := updates[0]
	result.Weights[0] = averaged
	
	return &result
}
