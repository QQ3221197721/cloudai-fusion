// Package edge - tensor_compression.go is the REAL data path of the
// compression pipeline. Unlike the analytical estimator in model_compression.go
// (which models sizes/losses from method parameters), the functions here
// operate on actual float32 weight tensors: magnitude pruning really zeroes
// weights, k-means weight sharing really clusters them, and quantization
// really rounds them to b-bit integers — and every result reports the MEASURED
// output size and MEASURED reconstruction error, not an estimate.
package edge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// MeasuredStageResult is the outcome of running one REAL compression stage on
// actual weights. All numbers are measured from the transformed tensor.
type MeasuredStageResult struct {
	Method CompressionMethod `json:"method"`
	// InputBytes / OutputBytes are the real storage sizes before/after
	// (dense float32 in; method-specific encoding out).
	InputBytes  int64 `json:"input_bytes"`
	OutputBytes int64 `json:"output_bytes"`
	// ReconstructionMSE is the mean squared error between the original and the
	// decompressed (reconstructed) weights.
	ReconstructionMSE float64 `json:"reconstruction_mse"`
	// SQNRdB is the signal-to-quantization-noise ratio in dB (higher = better;
	// +Inf when reconstruction is exact).
	SQNRdB float64 `json:"sqnr_db"`
	// Sparsity is the fraction of exactly-zero weights after the stage.
	Sparsity float64 `json:"sparsity"`
}

// ============================================================================
// Magnitude pruning (real)
// ============================================================================

// MagnitudePrune zeroes the smallest-magnitude fraction of weights and returns
// the pruned tensor plus a measured result. The output size is the real sparse
// encoding cost: 4 bytes value + 4 bytes index per surviving weight (COO),
// capped at dense size (a barely-pruned tensor stays dense).
func MagnitudePrune(weights []float32, sparsity float64) ([]float32, MeasuredStageResult, error) {
	if len(weights) == 0 {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: empty weight tensor")
	}
	if sparsity < 0 || sparsity >= 1 {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: sparsity %.2f out of [0,1)", sparsity)
	}

	// Threshold = |w| at the sparsity-quantile.
	mags := make([]float64, len(weights))
	for i, w := range weights {
		mags[i] = math.Abs(float64(w))
	}
	sort.Float64s(mags)
	cut := int(sparsity * float64(len(mags)))
	if cut >= len(mags) {
		cut = len(mags) - 1
	}
	threshold := mags[cut]

	out := make([]float32, len(weights))
	kept := 0
	for i, w := range weights {
		if math.Abs(float64(w)) > threshold {
			out[i] = w
			kept++
		}
	}
	// Ties at the threshold may prune slightly more than requested; that is the
	// honest, measured outcome.
	res := MeasuredStageResult{
		Method:     MethodPruning,
		InputBytes: int64(len(weights)) * 4,
	}
	res.Sparsity = 1 - float64(kept)/float64(len(weights))
	sparseBytes := int64(kept) * 8 // 4B value + 4B index (COO)
	if sparseBytes > res.InputBytes {
		sparseBytes = res.InputBytes
	}
	res.OutputBytes = sparseBytes
	res.ReconstructionMSE, res.SQNRdB = reconstructionError(weights, out)
	return out, res, nil
}

// ============================================================================
// Symmetric linear quantization (real)
// ============================================================================

// QuantizeSymmetric quantizes weights to signed b-bit integers with a single
// per-tensor scale, dequantizes them back, and measures the true error. The
// output size is the real packed size: n*bits/8 rounded up, plus 4 bytes for
// the float32 scale.
func QuantizeSymmetric(weights []float32, bits int) ([]float32, MeasuredStageResult, error) {
	if len(weights) == 0 {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: empty weight tensor")
	}
	if bits < 2 || bits > 16 {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: quantization bits %d out of [2,16]", bits)
	}

	maxAbs := 0.0
	for _, w := range weights {
		if a := math.Abs(float64(w)); a > maxAbs {
			maxAbs = a
		}
	}
	qmax := float64(int64(1)<<(bits-1)) - 1 // e.g. 127 for int8
	scale := 1.0
	if maxAbs > 0 {
		scale = maxAbs / qmax
	}

	out := make([]float32, len(weights))
	zeros := 0
	for i, w := range weights {
		q := math.Round(float64(w) / scale)
		if q > qmax {
			q = qmax
		}
		if q < -qmax-1 {
			q = -qmax - 1
		}
		out[i] = float32(q * scale)
		if out[i] == 0 {
			zeros++
		}
	}

	res := MeasuredStageResult{
		Method:      MethodQuantizationAware,
		InputBytes:  int64(len(weights)) * 4,
		OutputBytes: (int64(len(weights))*int64(bits)+7)/8 + 4, // packed ints + scale
		Sparsity:    float64(zeros) / float64(len(weights)),
	}
	res.ReconstructionMSE, res.SQNRdB = reconstructionError(weights, out)
	return out, res, nil
}

// ============================================================================
// K-means weight sharing (real)
// ============================================================================

// KMeansWeightShare clusters weights into k shared values (1-D k-means with
// deterministic quantile initialization) and replaces each weight with its
// centroid. The output size is the real shared encoding: k float32 codebook
// entries + ceil(log2 k) bits per weight index.
func KMeansWeightShare(weights []float32, k, iterations int) ([]float32, MeasuredStageResult, error) {
	if len(weights) == 0 {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: empty weight tensor")
	}
	if k < 2 || k > len(weights) {
		return nil, MeasuredStageResult{}, fmt.Errorf("edge: cluster count %d out of [2,len]", k)
	}
	if iterations <= 0 {
		iterations = 10
	}

	// Deterministic init: centroids at the k quantiles of the sorted weights.
	sorted := make([]float64, len(weights))
	for i, w := range weights {
		sorted[i] = float64(w)
	}
	sort.Float64s(sorted)
	centroids := make([]float64, k)
	for j := 0; j < k; j++ {
		idx := (2*j + 1) * len(sorted) / (2 * k)
		centroids[j] = sorted[idx]
	}

	assign := make([]int, len(weights))
	for it := 0; it < iterations; it++ {
		// Assignment step.
		changed := false
		for i, w := range weights {
			best, bestD := 0, math.MaxFloat64
			for j, c := range centroids {
				d := (float64(w) - c) * (float64(w) - c)
				if d < bestD {
					best, bestD = j, d
				}
			}
			if assign[i] != best {
				assign[i] = best
				changed = true
			}
		}
		// Update step.
		sum := make([]float64, k)
		cnt := make([]int, k)
		for i, w := range weights {
			sum[assign[i]] += float64(w)
			cnt[assign[i]]++
		}
		for j := 0; j < k; j++ {
			if cnt[j] > 0 {
				centroids[j] = sum[j] / float64(cnt[j])
			}
		}
		if !changed && it > 0 {
			break
		}
	}

	out := make([]float32, len(weights))
	zeros := 0
	for i := range weights {
		out[i] = float32(centroids[assign[i]])
		if out[i] == 0 {
			zeros++
		}
	}

	indexBits := int64(math.Ceil(math.Log2(float64(k))))
	res := MeasuredStageResult{
		Method:      MethodWeightSharing,
		InputBytes:  int64(len(weights)) * 4,
		OutputBytes: int64(k)*4 + (int64(len(weights))*indexBits+7)/8,
		Sparsity:    float64(zeros) / float64(len(weights)),
	}
	res.ReconstructionMSE, res.SQNRdB = reconstructionError(weights, out)
	return out, res, nil
}

// ============================================================================
// Shared measurement helpers
// ============================================================================

// reconstructionError measures MSE and SQNR (dB) between original and
// reconstructed tensors. SQNR is +Inf for an exact reconstruction.
func reconstructionError(original, reconstructed []float32) (mse, sqnrDB float64) {
	var signal, noise float64
	for i := range original {
		s := float64(original[i])
		e := s - float64(reconstructed[i])
		signal += s * s
		noise += e * e
	}
	n := float64(len(original))
	mse = noise / n
	if noise == 0 {
		return 0, math.Inf(1)
	}
	if signal == 0 {
		return mse, 0
	}
	return mse, 10 * math.Log10(signal/noise)
}

// ============================================================================
// Pipeline integration: the real (measured) execution path
// ============================================================================

// ExecuteOnWeights runs the pipeline's enabled stages on a REAL weight tensor.
// Every applied stage genuinely transforms the weights (prune/cluster/quantize)
// and the result carries MEASURED sizes and reconstruction errors, marked
// ExecutionMode="measured". Stages without a single-tensor executor
// (distillation, low-rank) are skipped and recorded honestly.
//
// AccuracyLoss in a measured result is the relative RMSE (%) between the
// original and final tensors — a reconstruction-error proxy, not a task
// accuracy claim.
func (p *CompressionPipeline) ExecuteOnWeights(ctx context.Context, modelID string, weights []float32) (*CompressionResult, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	if len(weights) == 0 {
		return nil, fmt.Errorf("edge: ExecuteOnWeights requires a non-empty weight tensor")
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	original := make([]float32, len(weights))
	copy(original, weights)
	current := make([]float32, len(weights))
	copy(current, weights)

	originalBytes := int64(len(weights)) * 4
	compressedBytes := originalBytes
	var measured []MeasuredStageResult
	stagesApplied := make([]string, 0)

	for _, stage := range p.stages {
		if ctx.Err() != nil {
			stage.Status = "skipped"
			continue
		}
		out, res, ok, err := runRealStage(stage, current)
		if err != nil {
			stage.Status = "failed"
			stage.Error = err.Error()
			continue
		}
		if !ok {
			// No real single-tensor executor for this method — honest skip.
			stage.Status = "skipped"
			stage.Error = "no real tensor executor for this method (modeled-only stage)"
			continue
		}
		now := time.Now()
		stage.StartedAt = &now
		stage.InputSize = int64(len(current)) * 4
		current = out
		compressedBytes = res.OutputBytes
		stage.OutputSize = res.OutputBytes
		completed := time.Now()
		stage.CompletedAt = &completed
		stage.Status = "completed"
		measured = append(measured, res)
		stagesApplied = append(stagesApplied, string(stage.Config.Method))
	}

	if len(measured) == 0 {
		return nil, fmt.Errorf("edge: no pipeline stage has a real tensor executor")
	}

	// Final measured quality: original vs fully-transformed tensor.
	finalMSE, finalSQNR := reconstructionError(original, current)
	var signal float64
	for _, w := range original {
		signal += float64(w) * float64(w)
	}
	relRMSEPct := 0.0
	if signal > 0 {
		relRMSEPct = math.Sqrt(finalMSE*float64(len(original))/signal) * 100
	}

	sizeReduction := (1.0 - float64(compressedBytes)/float64(originalBytes)) * 100
	ratio := float64(originalBytes) / math.Max(float64(compressedBytes), 1)

	constraintReport := []string{
		fmt.Sprintf("MEASURED: final reconstruction SQNR %.1f dB (MSE %.3g)", finalSQNR, finalMSE),
	}
	meets := true
	if float64(compressedBytes)/float64(originalBytes) > p.config.TargetSizeRatio {
		constraintReport = append(constraintReport, fmt.Sprintf("FAIL: measured size ratio %.2f > target %.2f",
			float64(compressedBytes)/float64(originalBytes), p.config.TargetSizeRatio))
		meets = false
	} else {
		constraintReport = append(constraintReport, fmt.Sprintf("PASS: measured size ratio %.2f within target %.2f",
			float64(compressedBytes)/float64(originalBytes), p.config.TargetSizeRatio))
	}

	result := &CompressionResult{
		ID:               fmt.Sprintf("comp-%s-%d", modelID, time.Now().Unix()),
		ModelID:          modelID,
		OriginalSize:     originalBytes,
		CompressedSize:   compressedBytes,
		CompressionRatio: ratio,
		SizeReduction:    sizeReduction,
		AccuracyLoss:     relRMSEPct,
		SpeedupFactor:    1.0, // not claimed: speedup needs a real inference benchmark
		StagesApplied:    stagesApplied,
		MeetsConstraints: meets,
		ConstraintReport: constraintReport,
		HardwareTarget:   p.config.HardwareTarget,
		ExecutionMode:    "measured",
		MeasuredStages:   measured,
		CreatedAt:        time.Now().UTC(),
	}
	p.results = append(p.results, result)

	p.logger.WithFields(logrus.Fields{
		"model":        modelID,
		"stages":       stagesApplied,
		"orig_bytes":   originalBytes,
		"comp_bytes":   compressedBytes,
		"sqnr_db":      fmt.Sprintf("%.1f", finalSQNR),
		"rel_rmse_pct": fmt.Sprintf("%.2f", relRMSEPct),
	}).Info("Measured compression pipeline completed (real tensor path)")

	return result, nil
}

// runRealStage dispatches one stage to its real tensor executor. Returns
// ok=false when the method has no real single-tensor implementation.
func runRealStage(stage *CompressionStage, weights []float32) ([]float32, MeasuredStageResult, bool, error) {
	params := stage.Config.Params
	switch stage.Config.Method {
	case MethodPruning, MethodStructuredPruning, MethodChannelPruning:
		sparsity := paramFloat(params, "sparsity_ratio", 0.5)
		out, res, err := MagnitudePrune(weights, sparsity)
		res.Method = stage.Config.Method
		return out, res, true, err
	case MethodWeightSharing:
		k := paramInt(params, "clusters", 32)
		out, res, err := KMeansWeightShare(weights, k, paramInt(params, "iterations", 10))
		res.Method = stage.Config.Method
		return out, res, true, err
	case MethodQuantizationAware:
		out, res, err := QuantizeSymmetric(weights, paramInt(params, "target_bits", 8))
		res.Method = stage.Config.Method
		return out, res, true, err
	case MethodGPTQ, MethodAWQ, MethodGGUF, MethodSqueezeLLM:
		// The real executor here is plain symmetric quantization at the target
		// bit-width; the method-specific calibration refinements remain modeled.
		out, res, err := QuantizeSymmetric(weights, paramInt(params, "bits", 4))
		res.Method = stage.Config.Method
		return out, res, true, err
	default:
		return nil, MeasuredStageResult{}, false, nil
	}
}

func paramFloat(params map[string]interface{}, key string, def float64) float64 {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int:
			return float64(t)
		}
	}
	return def
}

func paramInt(params map[string]interface{}, key string, def int) int {
	if v, ok := params[key]; ok {
		switch t := v.(type) {
		case int:
			return t
		case float64:
			return int(t)
		}
	}
	return def
}
