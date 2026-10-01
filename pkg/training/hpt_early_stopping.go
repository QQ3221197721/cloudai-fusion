package training

import (
	"fmt"
	"math"
)

// EarlyStoppingReason describes why early stopping was triggered.
type EarlyStoppingReason int

const (
	ReasonNone EarlyStoppingReason = iota
	ReasonConverged
	ReasonNoImprovement
	ReasonDiverging
	ReasonWorstThanBaseline
)

func (r EarlyStoppingReason) String() string {
	switch r {
	case ReasonConverged:
		return "converged"
	case ReasonNoImprovement:
		return "no_improvement"
	case ReasonDiverging:
		return "diverging"
	case ReasonWorstThanBaseline:
		return "worst_than_baseline"
	default:
		return "unknown"
	}
}

// MedianBaselineStopping implements median baseline early stopping policy.
type MedianBaselineStopping struct {
	minEpochs        int
	baselineWindow   int
patience             int
	improvementThreshold float64
}

// NewMedianBaselineStopping creates a median-based early stopping policy.
func NewMedianBaselineStopping() *MedianBaselineStopping {
	return &MedianBaselineStopping{
		minEpochs:            3,
		baselineWindow:       5,
		patience:             5,
		improvementThreshold: 0.001,
	}
}

// ShouldStop determines if trial should be stopped based on median baseline comparison.
func (ms *MedianBaselineStopping) ShouldStop(trial Trial, losses []float64) bool {
	if len(losses) < ms.minEpochs {
		return false
	}

	if len(losses) < ms.baselineWindow {
		return false
	}

	medianBaseline := ms.computeMedian(losses[:ms.baselineWindow])
	
	noImprovementCount := 0
	for i := ms.baselineWindow; i < len(losses); i++ {
		currentLoss := losses[i]
		
		if currentLoss > medianBaseline {
			noImprovementCount++
			if noImprovementCount >= ms.patience {
				return true
			}
		} else {
			improvement := medianBaseline - currentLoss
			if improvement > ms.improvementThreshold {
				medianBaseline = ms.exponentialMovingAverage(medianBaseline, currentLoss, 0.3)
				noImprovementCount = 0
			}
		}
	}

	return false
}

// Reasons returns why early stopping was triggered.
func (ms *MedianBaselineStopping) Reason(trial Trial, losses []float64) string {
	if len(losses) < ms.baselineWindow {
		return fmt.Sprintf("insufficient epochs (%d < %d)", len(losses), ms.baselineWindow)
	}

	medianBaseline := ms.computeMedian(losses[:ms.baselineWindow])
	lastLoss := losses[len(losses)-1]
	
	improvement := medianBaseline - lastLoss
	if improvement <= ms.improvementThreshold {
		return fmt.Sprintf("loss converged: %.6f vs baseline %.6f", lastLoss, medianBaseline)
	}

	noImprovementCount := 0
	for i := ms.baselineWindow; i < len(losses); i++ {
		if losses[i] > medianBaseline {
			noImprovementCount++
		} else {
			improvement := medianBaseline - losses[i]
			if improvement > ms.improvementThreshold {
				medianBaseline = ms.exponentialMovingAverage(medianBaseline, losses[i], 0.3)
				noImprovementCount = 0
			}
		}
	}

	if noImprovementCount >= ms.patience {
		return fmt.Sprintf("%d consecutive epochs without improvement", noImprovementCount)
	}

	return "unknown reason"
}

// CanEvaluateEarly indicates if early stopping evaluation is possible.
func (ms *MedianBaselineStopping) CanEvaluateEarly() bool {
	return ms.minEpochs > 0
}

// computeMedian calculates median of loss values using Quickselect algorithm.
// Time Complexity: O(n) average case
func (ms *MedianBaselineStopping) computeMedian(values []float64) float64 {
	if len(values) == 0 {
		return math.Inf(+1)
	}

	if len(values) == 1 {
		return values[0]
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)

	middle := len(sorted) / 2
	ms.quickSelect(sorted, middle)

	if len(sorted)%2 == 0 {
		return (sorted[middle-1] + sorted[middle]) / 2.0
	}

	return sorted[middle]
}

// quickSelect finds k-th smallest element using Hoare's selection algorithm.
// Time Complexity: O(n) average, Space: O(1) in-place
func (ms *MedianBaselineStopping) quickSelect(arr []float64, k int) {
	left, right := 0, len(arr)-1

	for left <= right {
		pivotIndex := ms.partition(arr, left, right)
		
		if pivotIndex == k {
			return
		} else if pivotIndex < k {
			left = pivotIndex + 1
		} else {
			right = pivotIndex - 1
		}
	}
}

// partition rearranges array around pivot using Lomuto scheme.
func (ms *MedianBaselineStopping) partition(arr []float64, left, right int) int {
	pivot := arr[right]
	i := left

	for j := left; j < right; j++ {
		if arr[j] <= pivot {
			arr[i], arr[j] = arr[j], arr[i]
			i++
		}
	}

	arr[i], arr[right] = arr[right], arr[i]
	return i
}

// exponentialMovingAverage computes EMA with configurable alpha.
func (ms *MedianBaselineStopping) exponentialMovingAverage(oldValue, newValue, alpha float64) float64 {
	return alpha*newValue + (1-alpha)*oldValue
}

// AdaptiveStopping implements adaptive early stopping with convergence detection.
type AdaptiveStopping struct {
	gradientThreshold  float64
	convergenceWindow  int
	minEpochsForStop   int
	maxPatience        int
}

// NewAdaptiveStopping creates adaptive early stopping with gradient analysis.
func NewAdaptiveStopping() *AdaptiveStopping {
	return &AdaptiveStopping{
		gradientThreshold: 1e-4,
		convergenceWindow: 10,
		minEpochsForStop:  5,
		maxPatience:       15,
	}
}

// ShouldStop checks convergence via gradient-based method.
func (as *AdaptiveStopping) ShouldStop(trial Trial, losses []float64) bool {
	if len(losses) < as.minEpochsForStop {
		return false
	}

	if len(losses) < as.convergenceWindow+as.minEpochsForStop {
		return false
	}

	windowStart := len(losses) - as.convergenceWindow
	subset := losses[windowStart:]

	gradients := make([]float64, 0, len(subset)-1)
	for i := 1; i < len(subset); i++ {
		grad := subset[i] - subset[i-1]
		gradients = append(gradients, grad)
	}

	if len(gradients) == 0 {
		return false
	}

	avgGradient := as.computeMean(gradients)
	maxAbsGradient := as.computeMaxAbs(gradients)

	if maxAbsGradient < as.gradientThreshold {
		return true
	}

	negativeGradientCount := 0
	for _, grad := range gradients {
		if grad < 0 {
			negativeGradientCount++
		}
	}

	if float64(negativeGradientCount)/float64(len(gradients)) > 0.7 {
		return true
	}

	return false
}

// Reason explains convergence decision.
func (as *AdaptiveStopping) Reason(trial Trial, losses []float64) string {
	windowStart := len(losses) - as.convergenceWindow
	subset := losses[windowStart:]

	gradients := make([]float64, 0, len(subset)-1)
	for i := 1; i < len(subset); i++ {
		gradients = append(gradients, subset[i]-subset[i-1])
	}

	maxAbsGradient := as.computeMaxAbs(gradients)
	if maxAbsGradient < as.gradientThreshold {
		return fmt.Sprintf("gradient magnitude %.4f below threshold", maxAbsGradient)
	}

	return "gradient-based convergence not detected"
}

// exponentialMovingAverage computes EMA with configurable alpha.
func (as *AdaptiveStopping) exponentialMovingAverage(oldValue, newValue, alpha float64) float64 {
	return alpha*newValue + (1-alpha)*oldValue
}

// CanEvaluateEarly always returns true after minEpochsForStop.
func (as *AdaptiveStopping) CanEvaluateEarly() bool {
	return as.minEpochsForStop > 0
}

// computeMean calculates arithmetic mean.
func (as *AdaptiveStopping) computeMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}

// computeMaxAbs finds maximum absolute value.
func (as *AdaptiveStopping) computeMaxAbs(values []float64) float64 {
	maxAbs := 0.0
	for _, v := range values {
		if abs := math.Abs(v); abs > maxAbs {
			maxAbs = abs
		}
	}
	return maxAbs
}
