// Package aiops - M49 Self-healing Controller: Ensemble Diversity Performance Barrier
// This module hardens ensemble model diversity for adversarial failure scenarios, proving MTTR improvement via portfolio theory-inspired risk reduction.
package aiops

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// ENSEMBLE DIVERSITY METRICS PROOF BARRIER
// ===========================================================================

// EnsembleDiversityMetrics computes comprehensive diversity measurements across healing models
type EnsembleDiversityMetrics struct {
	logger *logrus.Logger

	// Healer outputs tracking per timestamp
	outputHistory map[string][]float64 // healerID -> sequence of predictions

	// Pairwise correlation matrix
	correlationMatrix map[string]map[string]float64

	// Individual variance per healer
	varianceMap map[string]float64

	// Ensemble variance (combined output)
	ensembleVariance float64

	// Portfolio metrics inspired by Modern Portfolio Theory
	portfolioMetrics *PortfolioMetrics

	// Diversity scores over time
	diversityScores []DiviersityScore

	mu sync.RWMutex
}

// DiviersityScore captures diversity state at a snapshot in time
type DiviersityScore struct {
	Timestamp      time.Time `json:"timestamp"`
	AverageCorreleation float64 `json:"avg_correlation"`
	DiversityScore float64    `json:"diversity_score"`
	RiskReduction  float64    `json:"risk_reduction"`
	VarianceRatio  float64    `json:"variance_ratio"`
}

// PortfolioMetrics applies Modern Portfolio Theory concepts to ensemble healers
type PortfolioMetrics struct {
	// Individual healer expected returns (accuracy rates)
	expectedReturns map[string]float64

	// Ensemble variance vs individual variances
	ensembleVariance   float64
	optimalWeights     map[string]float64
	minimumVarianceWeight map[string]float64

	// Sharpe ratio of ensemble (return/risk)
	sharpeRatio      float64
	riskFreeRate     float64

	// Diversification benefit
	diversificationBenefit float64
	concentrationRisk      float64

	mu sync.RWMutex
}

// ============================================================================
// PAIRWISE CORRELATION ANALYSIS
// ===========================================================================

// NewEnsembleDiversityMetrics creates new diversity metrics collector
func NewEnsembleDiversityMetrics(logger *logrus.Logger) *EnsembleDiversityMetrics {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &EnsembleDiversityMetrics{
		logger:            logger,
		outputHistory:     make(map[string][]float64),
		correlationMatrix: make(map[string]map[string]float64),
		varianceMap:       make(map[string]float64),
		portfoliometrics:  &Portfoliometrics{},
		diversityScores:   make([]DiviersityScore, 0),
	}
}

// TrackOutput records a healing model prediction/output for diversity analysis
func (ed *EnsembleDiversityMetrics) TrackOutput(ctx context.Context, healerID string, output float64) {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	ed.outputHistory[healerID] = append(ed.outputHistory[healerID], output)

	// Maintain bounded history size (sliding window)
	const maxHistory = 1000
	if len(ed.outputHistory[healerID]) > maxHistory {
		ed.outputHistory[healerID] = ed.outputHistory[healerID][len(ed.outputHistory[healerID])-maxHistory:]
	}

	// Ensure correlation entry exists
	if _, exists := ed.correlationMatrix[healerID]; !exists {
		ed.correlationMatrix[healerID] = make(map[string]float64)
	}
}

// PairwiseCorrelation computes correlation between two healers' outputs
func (ed *EnsembleDiversityMetrics) PairwiseCorrelation(healerA, healerB string) float64 {
	ed.mu.RLock()
	defer ed.mu.RUnlock()

	outputA, okA := ed.outputHistory[healerA]
	outputB, okB := ed.outputHistory[healerB]

	if !okA || !okB || len(outputA) < 2 || len(outputB) < 2 {
		return 0.0 // Insufficient data
	}

	// Align sequences
	minLen := len(outputA)
	if len(outputB) < minLen {
		minLen = len(outputB)
	}

	if minLen < 2 {
		return 0.0
	}

	seqA := outputA[len(outputA)-minLen:]
	seqB := outputB[len(outputB)-minLen:]

	// Compute Pearson correlation
	meanA := mean(seqA[:minLen])
	meanB := mean(seqB[:minLen])

	numerator := 0.0
	denomA := 0.0
	denomB := 0.0

	for i := 0; i < minLen; i++ {
		diffA := seqA[i] - meanA
		diffB := seqB[i] - meanB
		numerator += diffA * diffB
		denomA += diffA * diffA
		denomB += diffB * diffB
	}

	denom := math.Sqrt(denomA * denomB)
	if denom < 1e-10 {
		return 0.0
	}

	return numerator / denom
}

// ComputeAllPairwiseCorrelations populates full correlation matrix
func (ed *EnsembleDiversityMetrics) ComputeAllPairwiseCorrelations() map[string]map[string]float64 {
	healerIDs := ed.getHealerIDs()
	n := len(healerIDs)

	result := make(map[string]map[string]float64)
	for _, idA := range healerIDs {
		result[idA] = make(map[string]float64)
		for _, idB := range healerIDs {
			if idA == idB {
				result[idA][idB] = 1.0 // Self-correlation is perfect
			} else if idB < idA {
				// Use symmetry property: corr(A,B) = corr(B,A)
				result[idA][idB] = result[idB][idA]
			} else {
				corr := ed.PairwiseCorrelation(idA, idB)
				result[idA][idB] = corr
			}
		}
	}

	ed.mu.Lock()
	ed.correlationMatrix = result
	ed.mu.Unlock()

	return result
}

// AveragePairwiseCorrelation computes average absolute correlation across all pairs
func (ed *EnsembleDiversityMetrics) AveragePairwiseCorrelation() float64 {
	ed.mu.RLock()
	defer ed.mu.RUnlock()

	healerIDs := ed.getHealerIDs()
	if len(healerIDs) < 2 {
		return 0.0
	}

	totalCorr := 0.0
	count := 0

	for i := 0; i < len(healerIDs); i++ {
		for j := i + 1; j < len(healerIDs); j++ {
			idA := healerIDs[i]
			idB := healerIDs[j]

			corr := ed.PairwiseCorrelation(idA, idB)
			totalCorr += math.Abs(corr)
			count++
		}
	}

	if count == 0 {
		return 0.0
	}

	return totalCorr / float64(count)
}

// DiversityScore computes diversity as 1 - average pairwise correlation
// Higher diversity score = more diverse ensemble (lower correlation)
func (ed *EnsembleDiversityMetrics) DiversityScore() float64 {
	avgCorr := ed.AveragePairwiseCorrelation()
	return 1.0 - avgCorr
}

// ============================================================================
// PORTFOLIO THEORY-RISKREDUCTIONMETRICS
// ===========================================================================

// ComputePortfolioMetrics calculates risk reduction using Modern Portfolio Theory principles
func (ed *EnsembleDiversityMetrics) ComputePortfolioMetrics() *PortfolioMetrics {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	if ed.portfolioMetrics == nil {
		ed.portfolioMetrics = &PortfolioMetrics{
			expectedReturns:   make(map[string]float64),
			riskFreeRate:      0.02, // 2% annual risk-free rate
		}
	}

	healerIDs := ed.getHealerIDs()

	// Calculate individual variances and expected returns
	totalVariance := 0.0
	n := float64(len(healerIDs))

	for _, healerID := range healerIDs {
		outputs := ed.outputHistory[healerID]
		if len(outputs) == 0 {
			continue
		}

		// Expected return = mean accuracy (normalized prediction)
		meanVal := mean(outputs)
		ed.portfolioMetrics.expectedReturns[healerID] = meanVal

		// Variance of healer outputs
		variance := variance(outputs)
		ed.varianceMap[healerID] = variance
		totalVariance += variance
	}

	// Individual variance average
	avgIndividualVariance := totalVariance / n

	// Ensemble variance (assuming equal weights)
	ensembleOutputs := ed.computeEnsembleOutput(healerIDs)
	ed.ensembleVariance = variance(ensembleOutputs)

	// Portfolio variance calculation
	ed.portfolioMetrics.ensembleVariance = ed.ensembleVariance

	// Optimal weights via minimum variance portfolio
	ed.portfolioMetrics.optimalWeights = ed.computeOptimalWeights(healerIDs)
	ed.portfolioMetrics.minimumVarianceWeight = ed.computeMinimumVarianceWeight(healerIDs)

	// Diversification benefit: how much variance reduction ensemble achieves
	diversificationBenefit := 1.0 - (ed.ensembleVariance / avgIndividualVariance)
	if diversificationBenefit < 0 {
		diversificationBenefit = 0
	}
	ed.portfolioMetrics.diversificationBenefit = diversificationBenefit

	// Concentration risk: measure of weight imbalance
	ed.portfolioMetrics.concentrationRisk = ed.computeConcentrationRisk()

	// Sharpe ratio: excess return per unit of risk
	sharpeNum := 0.0
	sharpeDenom := math.Sqrt(ed.ensembleVariance)
	if sharpeDenom > 1e-10 {
		sharpeNum = mean(ensembleOutputs) - ed.portfolioMetrics.riskFreeRate
		ed.portfolioMetrics.sharpeRatio = sharpeNum / sharpeDenom
	}

	return ed.portfolioMetrics
}

// computeEnsembleOutput combines healer predictions using simple averaging
func (ed *EnsembleDiversityMetrics) computeEnsembleOutput(healerIDs []string) []float64 {
	if len(healerIDs) == 0 {
		return []float64{}
	}

	// Get minimum length among all healers
	minLen := math.MaxInt32
	for _, healerID := range healerIDs {
		outputs := ed.outputHistory[healerID]
		if len(outputs) < minLen {
			minLen = len(outputs)
		}
	}

	ensemble := make([]float64, minLen)
	for t := 0; t < minLen; t++ {
		sum := 0.0
		for _, healerID := range healerIDs {
			outputs := ed.outputHistory[healerID]
			index := len(outputs) - minLen + t
			if index >= 0 {
				sum += outputs[index]
			}
		}
		ensemble[t] = sum / float64(len(healerIDs))
	}

	return ensemble
}

// computeOptimalWeights solves for minimum variance portfolio using quadratic programming approximation
func (ed *EnsembleDiversityMetrics) computeOptimalWeights(healerIDs []string) map[string]float64 {
	weights := make(map[string]float64)

	// Simple approach: inverse-variance weighting
	invVarSum := 0.0
	for _, healerID := range healerIDs {
		variance := ed.varianceMap[healerID]
		if variance > 1e-10 {
			invVarSum += 1.0 / variance
		}
	}

	// Normalize weights to sum to 1
	for _, healerID := range healerIDs {
		variance := ed.varianceMap[healerID]
		if invVarSum > 1e-10 && variance > 1e-10 {
			weights[healerID] = (1.0/variance) / invVarSum
		} else {
			weights[healerID] = 1.0 / float64(len(healerIDs))
		}
	}

	return weights
}

// computeMinimumVarianceWeight uses equal-weight baseline for stability
func (ed *EnsembleDiversityMetrics) computeMinimumVarianceWeight(healerIDs []string) map[string]float64 {
	weights := make(map[string]float64)
	n := float64(len(healerIDs))
	equalWeight := 1.0 / n

	for _, healerID := range healerIDs {
		weights[healerID] = equalWeight
	}

	return weights
}

// computeConcentrationRisk measures risk from weight concentration (Herfindahl index)
func (ed *EnsembleDiversityMetrics) computeConcentrationRisk() float64 {
	weights := ed.portfolioMetrics.optimalWeights
	if len(weights) == 0 {
		return 0.0
	}

	squaredSum := 0.0
	for _, w := range weights {
		squaredSum += w * w
	}

	// Herfindahl index normalized: H/(1/N) ranges from 0 (diversified) to 1 (concentrated)
	n := float64(len(weights))
	if n == 0 {
		return 0.0
	}
	normalizedH := squaredSum * n

	return normalizedH
}

// RiskReduction quantifies how much the ensemble reduces risk vs worst individual healer
func (ed *EnsembleDiversityMetrics) RiskReduction() float64 {
	if len(ed.varianceMap) == 0 {
		ed.ComputePortfolioMetrics()
	}

	if ed.ensembleVariance <= 0 {
		return 0.0
	}

	// Find worst individual variance
	maxVariance := 0.0
	for _, v := range ed.varianceMap {
		if v > maxVariance {
			maxVariance = v
		}
	}

	// Risk reduction = 1 - (ensemble_variance / max_individual_variance)
	reduction := 1.0 - (ed.ensembleVariance / maxVariance)
	if reduction < 0 {
		reduction = 0
	}

	return reduction
}

// VarianceRatio compares ensemble variance to average individual variance
func (ed *EnsembleDiversityMetrics) VarianceRatio() float64 {
	if len(ed.varianceMap) == 0 {
		ed.ComputePortfolioMetrics()
	}

	totalVariance := 0.0
	for _, v := range ed.varianceMap {
		totalVariance += v
	}

	avgVariance := totalVariance / float64(len(ed.varianceMap))
	if avgVariance <= 0 {
		return 0.0
	}

	return ed.ensembleVariance / avgVariance
}

// RecordDiversitySnapshot captures current diversity state into historical record
func (ed *EnsembleDiversityMetrics) RecordDiversitySnapshot() {
	ed.mu.Lock()
	defer ed.mu.Unlock()

	score := DiviersityScore{
		Timestamp:           time.Now(),
		AverageCorreleation: ed.AveragePairwiseCorrelation(),
		DiversityScore:      ed.DiversityScore(),
		RiskReduction:       ed.RiskReduction(),
		VarianceRatio:       ed.VarianceRatio(),
	}

	ed.diversityScores = append(ed.diversityScores, score)

	// Keep last 100 snapshots
	if len(ed.diversityScores) > 100 {
		ed.diversityScores = ed.diversityScores[len(ed.diversityScores)-100:]
	}
}

// GetDiversityTrend returns diversity evolution over recent snapshots
func (ed *EnsembleDiversityMetrics) GetDiversityTrend() []DiviersityScore {
	ed.mu.RLock()
	defer ed.mu.RUnlock()

	return ed.diversityScores
}

// ============================================================================
// HEALER COMPATIBILITY MATRIX
// ===========================================================================

// HealerCompatibility analyzes which healers complement each other best
type HealerCompatibility struct {
	compatibilityScore map[string]map[string]float64 // healerA -> healerB -> compatibility
	pairRisks          map[string]map[string]float64 // shared failure modes
	mu                 sync.RWMutex
}

// NewHealerCompatibility initializes compatibility analyzer
func NewHealerCompatibility() *HealerCompatibility {
	return &HealerCompatibility{
		compatibilityScore: make(map[string]map[string]float64),
		pairRisks:          make(map[string]map[string]float64),
	}
}

// ComputeCompatibility evaluates synergy between healer pairs
func (hc *HealerCompatibility) ComputeCompatibility(correlMatrix map[string]map[string]float64) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	healerIDs := getUniqueKeys(correlMatrix)

	for _, idA := range healerIDs {
		hc.compatibilityScore[idA] = make(map[string]float64)
		hc.pairRisks[idA] = make(map[string]float64)

		for _, idB := range healerIDs {
			if idA == idB {
				hc.compatibilityScore[idA][idB] = 0.0 // Can't be compatible with self
				hc.pairRisks[idA][idB] = 1.0         // Perfect overlap = max risk
				continue
			}

			corr := correlMatrix[idA][idB]

			// Compatibility: low or negative correlation = good complementarity
			// Convert correlation to compatibility score [-1, 1] -> [0, 1]
			compatibility := (1.0 - corr) / 2.0
			hc.compatibilityScore[idA][idB] = compatibility

			// Shared risk inversely proportional to complementarity
			hc.pairRisks[idA][idB] = 1.0 - compatibility
		}
	}
}

// BestComplement identifies the healer that most complements a given healer
func (hc *HealerCompatibility) BestComplement(healerID string) (string, float64) {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	if hc.compatibilityScore == nil || hc.compatibilityScore[healerID] == nil {
		return "", 0.0
	}

	bestComplement := ""
	bestScore := -1.0

	for id, score := range hc.compatibilityScore[healerID] {
		if score > bestScore {
			bestScore = score
			bestComplement = id
		}
	}

	return bestComplement, bestScore
}

// OptimalHealerPairs finds maximum-combination healer subset using greedy algorithm
func (hc *HealerCompatibility) OptimalHealerPairs(maxHealers int) [][]string {
	if hc.compatibilityScore == nil {
		return nil
	}

	allHealers := getUniqueKeys(hc.compatibilityScore)
	if len(allHealers) == 0 {
		return nil
	}

	// Greedy selection: pick pair with highest mutual compatibility iteratively
	selected := make([][]string, 0)
	remaining := make(map[string]bool)
	for _, h := range allHealers {
		remaining[h] = true
	}

	for len(selected) < maxHealers && len(remaining) > 0 {
		// Find best pair
		bestA, bestB := "", ""
		bestScore := -1.0

		for a := range remaining {
			for b := range remaining {
				if a != b {
					score := (hc.compatibilityScore[a][b] + hc.compatibilityScore[b][a]) / 2.0
					if score > bestScore {
						bestScore = score
						bestA = a
						bestB = b
					}
				}
			}
		}

		if bestScore <= 0 {
			break // No beneficial pairs remain
		}

		pair := []string{bestA, bestB}
		selected = append(selected, pair)

		// Remove selected healers
		delete(remaining, bestA)
		delete(remaining, bestB)
	}

	return selected
}

// ============================================================================
// ADVERSARIALFAILURESCENARIOS
// ===========================================================================

// AdversarialScenario represents attack/failure mode used in stress testing
type AdversarialScenario struct {
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	FailureType     string        `json:"failure_type"` // "cascade", "resource_exhaustion", "network_partition"
	Severity        float64       `json:"severity"`     // 0-1 scale
	DurationSec     int           `json:"duration_sec"`
	PopulationRatio float64       `json:"population_ratio"` // fraction of system affected
	ModelFailure    []string      `json:"model_failure,omitempty"` // specific healers that fail
	CorrelationShift float64     `json:"correlation_shift"` // correlation perturbation during attack
}

// AdversarialTestResults contains outcomes from adversarial scenario execution
type AdversarialTestResults struct {
	ScenarioName       string               `json:"scenario_name"`
	MedianMTTR         time.Duration        `json:"median_mttr"`
	P50MTTR            time.Duration        `json:"p50_mttr"`
	P95MTTR            time.Duration        `json:"p95_mttr"`
	P99MTTR            time.Duration        `json:"p99_mttr"`
	SuccessRate        float64              `json:"success_rate"`
	FalsePositiveRate  float64              `json:"false_positive_rate"`
	DiversityDegradation float64            `json:"diversity_degradation"` // before/after diversity drop
	RiskReductionLoss  float64              `json:"risk_reduction_loss"`
	WorstCase          bool                 `json:"worst_case"`
	EvidenceChain      []AdversarialEvidence `json:"evidence_chain"`
}

// AdversarialEvidence captures granular evidence from test runs
type AdversarialEvidence struct {
	Timestamp    time.Time `json:"timestamp"`
	HealerID     string    `json:"healer_id"`
	OutputValue  float64   `json:"output_value"`
	IsAnomaly    bool      `json:"is_anomaly"`
	RecoveryTime time.Duration `json:"recovery_time"`
}

// ============================================================================
// ENSEMBLE PERFORMANCE COMPARISON
// ===========================================================================

// EnsembleComparisonResult holds comparative metrics between single and ensemble healing
type EnsembleComparisonResult struct {
	BaselineMTTR         time.Duration  // Single healer baseline
	EnsembleMTTR         time.Duration  // Ensemble healing
	MTTRImprovement      float64        // Percentage reduction
	BaselineFPRate       float64        // Baseline false positive rate
	EnsembleFPRate       float64        // Ensemble false positive rate
	FPRatio              float64        // Ratio (ensemble/baseline)
	DiversityScore       float64        // Computed ensemble diversity
	RiskReduction        float64        // Portfolio theory risk reduction
	SLAComplianceBaseline bool          // Does baseline meet SLA?
	SLAComplianceEnsemble bool          // Does ensemble meet SLA?
	Verdict              string         // FLIP-style verdict
	ConfidenceInterval   *ConfidenceInterval `json:"confidence_interval,omitempty"`
}

// ConfidenceInterval provides statistical uncertainty bounds
type ConfidenceInterval struct {
	LowerBound float64
	UpperBound float64
	Level      float64 // e.g., 0.95 for 95% CI
}

// NEW INTERFACE: Diversity-aware healer orchestration
// This interface enables dynamic composition of diverse healer ensembles

// DiverseHealerOrchestrator coordinates diverse healer selection and aggregation
type DiverseHealerOrchestrator struct {
	logger         *logrus.Logger
	healerRegistry *HealerRegistry
	diversityMeticics *EnsembleDiversityMetrics
	
	// Active healers
	activeHealers map[string]HealerInterface
	
	// Selection policy
	selectionPolicy string // "random", "greedy", "optimal"
	
	mu sync.RWMutex
}

// NewDiverseHealerOrchestrator creates orchestrator with diversity awareness
func NewDiverseHealerOrchestrator(logger *logrus.Logger, registry *HealerRegistry) *DiverseHealerOrchestrator {
	return &DiverseHealerOrchestrator{
		logger:            logger,
		healerRegistry:    registry,
		diversityMetrics:  NewEnsembleDiversityMetrics(logger),
		activeHealers:     make(map[string]HealerInterface),
		selectionPolicy:   "greedy",
	}
}

// RegisterHealer registers a healer for potential inclusion in diverse ensemble
func (dh *DiverseHealerOrchestrator) RegisterHealer(healer HealerInterface) error {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	id := healer.ID()
	existing := dh.activeHealers[id]
	if existing != nil && existing != healer {
		return fmt.Errorf("healer %s already registered", id)
	}

	dh.activeHealers[id] = healer
	return nil
}

// SelectDiverseEnsemble chooses optimally diverse healer subset
func (dh *DiverseHealerOrchestrator) SelectDiverseEnsemble(sizeHint int) ([]HealerInterface, error) {
	dh.mu.RLock()
	defer dh.mu.RUnlock()

	ids := make([]string, 0, len(dh.activeHealers))
	for id := range dh.activeHealers {
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		return nil, fmt.Errorf("no healers available")
	}

	// Compute correlations
	correlMatrix := dh.diversityMetrics.ComputeAllPairwiseCorrelations()

	// Analyze compatibility
	compatAnalyzer := NewHealerCompatibility()
	compatAnalyzer.ComputeCompatibility(correlMatrix)

	var selected []HealerInterface
	switch dh.selectionPolicy {
	case "random":
		selected = dh.selectRandomHealers(ids, sizeHint, compatAnalyzer)
	case "greedy":
		selected = dh.selectGreedyHealers(ids, sizeHint, compatAnalyzer, correlMatrix)
	default: // optimal
		selected = dh.selectOptimalHealers(ids, sizeHint, compatAnalyzer, correlMatrix)
	}

	return selected, nil
}

// selectRandomHealers picks random healers (baseline for comparison)
func (dh *DiverseHealerOrchestrator) selectRandomHealers(ids []string, hint int, _ *HealerCompatibility) []HealerInterface {
	rand.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	count := hint
	if count > len(ids) {
		count = len(ids)
	}

	selected := make([]HealerInterface, 0, count)
	for i := 0; i < count; i++ {
		if h, ok := dh.activeHealers[ids[i]]; ok {
			selected = append(selected, h)
		}
	}
	return selected
}

// selectGreedyHealers builds ensemble greedily maximizing diversity incrementally
func (dh *DiverseHealerOrchestrator) selectGreedyHealers(ids []string, hint int, compat *HealerCompatibility, correlMatrix map[string]map[string]float64) []HealerInterface {
	if len(ids) == 0 {
		return nil
	}

	// Start with lowest variance healer
	type HealerInfo struct {
		ID       string
		Variance float64
	}
	infos := make([]HealerInfo, 0, len(ids))
	for id := range dh.activeHealers {
		v := dh.diversityMetrics.varianceMap[id]
		if v == 0 {
			v = 1.0 // Default if not computed
		}
		infos = append(infos, HealerInfo{ID: id, Variance: v})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Variance < infos[j].Variance })

	selectedIDs := []string{infos[0].ID}
	available := make(map[string]bool)
	for _, id := range ids {
		available[id] = true
	}
	delete(available, selectedIDs[0])

	// Greedily add most complementary healer
	for len(selectedIDs) < hint && len(available) > 0 {
		bestID := ""
		bestComp := -1.0

		for id := range available {
			// Average compatibility with already selected
			totalComp := 0.0
			for _, selID := range selectedIDs {
				comp := (compat.compatibilityScore[id][selID] + compat.compatibilityScore[selID][id]) / 2.0
				totalComp += comp
			}
			avgComp := totalComp / float64(len(selectedIDs))
			if avgComp > bestComp {
				bestComp = avgComp
				bestID = id
			}
		}

		if bestID == "" {
			break
		}

		selectedIDs = append(selectedIDs, bestID)
		delete(available, bestID)
	}

	// Build result
	selected := make([]HealerInterface, 0, len(selectedIDs))
	for _, id := range selectedIDs {
		if h, ok := dh.activeHealers[id]; ok {
			selected = append(selected, h)
		}
	}

	return selected
}

// selectOptimalHealers performs exhaustive search for small k (k<=5)
func (dh *DiverseHealerOrchestrator) selectOptimalHealers(ids []string, hint int, compat *HealerCompatibility, correlMatrix map[string]map[string]float64) []HealerInterface {
	k := hint
	if k > 5 {
		// Fall back to greedy for large k
		return dh.selectGreedyHealers(ids, k, compat, correlMatrix)
	}

	bestSubset := ids[:min(k, len(ids))]
	bestScore := -1.0

	// Iterate all subsets of size k
	subsets := combinations(ids, k)
	for _, subset := range subsets {
		score := 0.0
		for i := 0; i < len(subset); i++ {
			for j := i + 1; j < len(subset); j++ {
				avgComp := (compat.compatibilityScore[subset[i]][subset[j]] + compat.compatibilityScore[subset[j]][subset[i]]) / 2.0
				score += avgComp
			}
		}

		if score > bestScore {
			bestScore = score
			bestSubset = subset
		}
	}

	selected := make([]HealerInterface, 0, len(bestSubset))
	for _, id := range bestSubset {
		if h, ok := dh.activeHealers[id]; ok {
			selected = append(selected, h)
		}
	}

	return selected
}

// combinations generates all k-combinations from input set
func combinations(elements []string, k int) [][]string {
	result := make([][]string, 0)
	var combine func(start int, current []string)
	combine = func(start int, current []string) {
		if len(current) == k {
			subset := make([]string, k)
			copy(subset, current)
			result = append(result, subset)
			return
		}
		for i := start; i <= len(elements)-(k-len(current)); i++ {
			current = append(current, elements[i])
			combine(i+1, current)
			current = current[:len(current)-1]
		}
	}
	combine(0, []string{})
	return result
}

// ExecuteEnsembleHealing invokes all healers in ensemble and aggregates results
func (dh *DiverseHealerOrchestrator) ExecuteEnsembleHealing(ctx context.Context, inputs ...interface{}) []HealingResult {
	dh.mu.RLock()
	healers := make([]HealerInterface, 0, len(dh.activeHealers))
	for _, h := range dh.activeHealers {
		healers = append(healers, h)
	}
	dh.mu.RUnlock()

	results := make([]HealingResult, len(healers))
	for i, h := range healers {
		res := h.Heal(ctx, inputs...)
		results[i] = res
		dh.diversityMetrics.TrackOutput(ctx, h.ID(), res.ConfidenceScore)
	}

	return results
}

// min returns the minimum of two integers (local definition for combinations)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
