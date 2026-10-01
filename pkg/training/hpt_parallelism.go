package training

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

// ParallelSearchStrategy implements parallel Bayesian optimization with batch suggestions.
type ParallelSearchStrategy struct {
	mu            sync.RWMutex
	baseOptimizer *BayesianOptimizer
	batchSize     int
	trials        []Trial
	jobID         string
	rand          *rand.Rand
}

// NewParallelSearchStrategy creates a parallel hyperparameter search optimizer.
func NewParallelSearchStrategy(jobID string, batchSize int) *ParallelSearchStrategy {
	if batchSize <= 0 {
		batchSize = 10
	}

	return &ParallelSearchStrategy{
		baseOptimizer: NewBayesianOptimizer(),
		batchSize:     batchSize,
		trials:        make([]Trial, 0),
		jobID:         jobID,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// SuggestParameters generates multiple parameter sets for parallel execution.
// Time Complexity: O(B * M * D) where B=batch_size, M=MC samples, D=params
func (ps *ParallelSearchStrategy) SuggestParameters(jobID string, trials []Trial) ([]map[string]float64, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(trials) < 1 {
		initialParams := ps.generateInitialDesign(len(trials))
		result := make([]map[string]float64, ps.batchSize)
		copy(result, initialParams)
		return result, nil
	}

	suggestions := make([]map[string]float64, 0, ps.batchSize)
	existingBest, _ := ps.baseOptimizer.GetBestParameters()

	for i := 0; i < ps.batchSize; i++ {
		params, err := ps.suggestSingleParameter(existingBest, trials)
		if err != nil {
			return nil, fmt.Errorf("failed to suggest parameters for iteration %d: %w", i, err)
		}
		suggestions = append(suggestions, params)
	}

	return suggestions, nil
}

// suggestSingleParameter generates one parameter suggestion using expected improvement.
func (ps *ParallelSearchStrategy) suggestSingleParameter(bestParams map[string]float64, trials []Trial) (map[string]float64, error) {
	currentBest := math.Inf(+1)
	for _, trial := range trials {
		if trial.Status == TrialSuccess {
			if loss, ok := trial.Metrics["loss"]; ok && loss < currentBest {
				currentBest = loss
			}
		}
	}

	paramNames := ps.extractParameterNames(trials)
	newParams := make(map[string]float64, len(paramNames))

	for _, paramName := range paramNames {
		value, _, _ := ps.sampleValueWithUncertainty(paramName, bestParams)
		newParams[paramName] = value
	}

	hash := fmt.Sprintf("%s_%v_%f", ps.jobID, newParams, currentBest)
	ps.trials = append(ps.trials, Trial{
		ID:         hash[:16],
		JobID:      ps.jobID,
		Parameters: newParams,
		Status:     TrialPending,
	})

	return newParams, nil
}

// sampleValueWithUncertainty applies exploration-exploitation tradeoff.
func (ps *ParallelSearchStrategy) sampleValueWithParameter(name string) float64 {
	paramRanges := map[string]struct {
		min, max float64
		logScale bool
	}{
		"learning_rate":     {1e-5, 1e-1, true},
		"batch_size":        {16, 512, false},
		"dropout_rate":      {0.1, 0.7, false},
		"weight_decay":      {1e-5, 1e-2, true},
		"temperature":       {0.5, 2.0, false},
	}

	rangeInfo, ok := paramRanges[name]
	if !ok {
		return 0.5
	}

	minVal, maxVal := rangeInfo.min, rangeInfo.max

	explorationFactor := ps.rand.Float64()
	if explorationFactor > 0.3 {
		if rangeInfo.logScale {
			logMin, logMax := math.Log(minVal), math.Log(maxVal)
			sampledLog := ps.rand.Float64()*(logMax-logMin) + logMin
			return math.Exp(sampledLog)
		}
		return ps.rand.Float64()*(maxVal-minVal) + minVal
	}

	return (minVal + maxVal) / 2.0
}

// extractParameterNames collects unique parameter names from completed trials.
func (ps *ParallelSearchStrategy) extractParameterNames(trials []Trial) []string {
	paramSet := make(map[string]bool)
	for _, trial := range trials {
		for paramName := range trial.Parameters {
			paramSet[paramName] = true
		}
	}

	names := make([]string, 0, len(paramSet))
	for name := range paramSet {
		names = append(names, name)
	}

	if len(names) == 0 {
		names = []string{"learning_rate", "batch_size", "dropout_rate"}
	}

	return names
}

// generateInitialDesign creates Latin Hypercube Sampling for initial exploration.
func (ps *ParallelSearchStrategy) generateInitialDesign(count int) []map[string]float64 {
	results := make([]map[string]float64, count)
	paramNames := []string{"learning_rate", "batch_size", "dropout_rate"}

	for i := 0; i < count; i++ {
		params := make(map[string]float64, len(paramNames))
		for _, name := range paramNames {
			params[name] = ps.sampleUniformParam(name)
		}
		results[i] = params
	}

	return results
}

// sampleUniformParam samples uniformly within parameter bounds.
func (ps *ParallelSearchStrategy) sampleUniformParam(name string) float64 {
	switch name {
	case "learning_rate":
		logMin, logMax := math.Log(1e-5), math.Log(1e-1)
		logSample := ps.rand.Float64()*(logMax-logMin) + logMin
		return math.Exp(logSample)
	case "batch_size":
		powers := []int{4, 5, 6, 7, 8}
		return math.Pow(2, float64(powers[ps.rand.Intn(len(powers))]))
	case "dropout_rate":
		return ps.rand.Float64()*0.6 + 0.1
	default:
		return ps.rand.Float64()
	}
}

// UpdateHistory records trial results for next iteration.
func (ps *ParallelSearchStrategy) UpdateHistory(trial Trial) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	for i := range ps.trials {
		if ps.trials[i].ID == trial.ID {
			ps.trials[i] = trial
			return nil
		}
	}

	ps.trials = append(ps.trials, trial)
	return nil
}

// GetBestParameters returns the best parameters found across all batches.
func (ps *ParallelSearchStrategy) GetBestParameters() (map[string]float64, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if len(ps.trials) == 0 {
		return nil, fmt.Errorf("no trials recorded yet")
	}

	bestParams := make(map[string]float64)
	bestLoss := math.Inf(+1)

	for _, trial := range ps.trials {
		if trial.Status == TrialSuccess {
			if loss, ok := trial.Metrics["loss"]; ok && loss < bestLoss {
				bestLoss = loss
				bestParams = copyParameters(trial.Parameters)
			}
		}
	}

	if math.IsInf(bestLoss, +1) {
		return nil, fmt.Errorf("no successful trials completed")
	}

	return bestParams, nil
}

// EstimateParallelSpeedup calculates theoretical speedup from parallel evaluation.
func (ps *ParallelSearchStrategy) EstimateParallelSpeedupsequentialTime, parallelTime time.Duration) float64 {
	if parallelTime <= 0 {
		return 1.0
	}
	return sequentialTime / parallelTime
}

// copyParameters creates independent parameter map copy.
func copyParameters(source map[string]float64) map[string]float64 {
	result := make(map[string]float64, len(source))
	for k, v := range source {
		result[k] = v
	}
	return result
}
