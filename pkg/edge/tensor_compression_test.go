package edge

import (
	"context"
	"math"
	"math/rand"
	"testing"
)

// synthWeights generates a deterministic pseudo-normal weight tensor (fixed
// seed) so every assertion below is reproducible in CI.
func synthWeights(n int, seed int64) []float32 {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test data, not crypto
	w := make([]float32, n)
	for i := range w {
		w[i] = float32(rng.NormFloat64() * 0.02) // typical NN init scale
	}
	return w
}

// TestMagnitudePrune_RealSparsity proves pruning REALLY zeroes weights: the
// measured sparsity matches the request and survivors are exactly the
// largest-magnitude entries.
func TestMagnitudePrune_RealSparsity(t *testing.T) {
	w := synthWeights(10000, 42)
	out, res, err := MagnitudePrune(w, 0.5)
	if err != nil {
		t.Fatalf("MagnitudePrune: %v", err)
	}
	if len(out) != len(w) {
		t.Fatalf("output length %d != input %d", len(out), len(w))
	}
	// Count actual zeros — the measured result must match reality.
	zeros := 0
	for _, v := range out {
		if v == 0 {
			zeros++
		}
	}
	gotSparsity := float64(zeros) / float64(len(out))
	if math.Abs(gotSparsity-res.Sparsity) > 1e-9 {
		t.Fatalf("reported sparsity %.4f != counted %.4f", res.Sparsity, gotSparsity)
	}
	if gotSparsity < 0.5 || gotSparsity > 0.55 {
		t.Fatalf("sparsity %.4f not in [0.50, 0.55]", gotSparsity)
	}
	// Every surviving weight must outrank every pruned weight in magnitude.
	minKept, maxPruned := math.MaxFloat64, 0.0
	for i, v := range out {
		if v != 0 {
			if a := math.Abs(float64(v)); a < minKept {
				minKept = a
			}
		} else if a := math.Abs(float64(w[i])); a > maxPruned {
			maxPruned = a
		}
	}
	if minKept < maxPruned {
		t.Fatalf("pruning kept a smaller weight (%.6g) than one it removed (%.6g)", minKept, maxPruned)
	}
	// Sparse encoding must be measurably smaller than dense.
	if res.OutputBytes >= res.InputBytes {
		t.Fatalf("sparse size %d not smaller than dense %d", res.OutputBytes, res.InputBytes)
	}
}

// TestQuantizeSymmetric_ErrorBounds proves int8 quantization REALLY rounds
// weights and its measured error respects theory: max per-weight error ≤
// scale/2 and SQNR is in the range real 8-bit quantization produces.
func TestQuantizeSymmetric_ErrorBounds(t *testing.T) {
	w := synthWeights(10000, 7)
	out, res, err := QuantizeSymmetric(w, 8)
	if err != nil {
		t.Fatalf("QuantizeSymmetric: %v", err)
	}
	// Recompute the scale exactly as the implementation defines it.
	maxAbs := 0.0
	for _, v := range w {
		if a := math.Abs(float64(v)); a > maxAbs {
			maxAbs = a
		}
	}
	scale := maxAbs / 127.0
	for i := range w {
		if e := math.Abs(float64(w[i]) - float64(out[i])); e > scale/2+1e-12 {
			t.Fatalf("weight %d error %.6g exceeds theoretical bound scale/2=%.6g", i, e, scale/2)
		}
	}
	// 8-bit quantization of a well-spread tensor: SQNR must be substantial.
	// (Uniform-input theory gives ~6.02*8=48dB; Gaussian data with peak scaling
	// lands lower — anything above 30dB proves real quantization happened,
	// anything below proves a bug.)
	if res.SQNRdB < 30 || res.SQNRdB > 60 {
		t.Fatalf("int8 SQNR %.1f dB outside plausible [30,60] range", res.SQNRdB)
	}
	// Packed size: 10000 int8 + 4B scale.
	if want := int64(10000 + 4); res.OutputBytes != want {
		t.Fatalf("packed size %d != expected %d", res.OutputBytes, want)
	}
	// 4-bit must be smaller and noisier than 8-bit — a relationship only a real
	// implementation exhibits.
	_, res4, err := QuantizeSymmetric(w, 4)
	if err != nil {
		t.Fatalf("QuantizeSymmetric(4): %v", err)
	}
	if res4.OutputBytes >= res.OutputBytes {
		t.Fatalf("4-bit size %d not smaller than 8-bit %d", res4.OutputBytes, res.OutputBytes)
	}
	if res4.SQNRdB >= res.SQNRdB {
		t.Fatalf("4-bit SQNR %.1f not worse than 8-bit %.1f", res4.SQNRdB, res.SQNRdB)
	}
}

// TestKMeansWeightShare_RealClustering proves weight sharing REALLY clusters:
// the output contains at most k distinct values and reconstruction error
// decreases as k grows.
func TestKMeansWeightShare_RealClustering(t *testing.T) {
	w := synthWeights(5000, 99)
	out, res, err := KMeansWeightShare(w, 16, 20)
	if err != nil {
		t.Fatalf("KMeansWeightShare: %v", err)
	}
	distinct := make(map[float32]struct{})
	for _, v := range out {
		distinct[v] = struct{}{}
	}
	if len(distinct) > 16 {
		t.Fatalf("weight sharing produced %d distinct values, want <= 16", len(distinct))
	}
	if res.OutputBytes >= res.InputBytes {
		t.Fatalf("shared encoding %d not smaller than dense %d", res.OutputBytes, res.InputBytes)
	}
	// More clusters => lower error (monotonicity of a real k-means).
	_, res256, err := KMeansWeightShare(w, 256, 20)
	if err != nil {
		t.Fatalf("KMeansWeightShare(256): %v", err)
	}
	if res256.ReconstructionMSE >= res.ReconstructionMSE {
		t.Fatalf("k=256 MSE %.6g not lower than k=16 MSE %.6g", res256.ReconstructionMSE, res.ReconstructionMSE)
	}
}

// TestExecuteOnWeights_EndToEnd is the E2E proof that the pipeline has a REAL
// data path: default stages transform an actual tensor, the result is marked
// "measured", the compressed size is genuinely smaller, and the measured error
// is physically plausible — none of which a config-driven estimator could fake.
func TestExecuteOnWeights_EndToEnd(t *testing.T) {
	cfg := CompressionPipelineConfig{
		Stages: []CompressionStageConfig{
			{Method: MethodStructuredPruning, Order: 1, Enabled: true,
				Params: map[string]interface{}{"sparsity_ratio": 0.5}},
			{Method: MethodQuantizationAware, Order: 2, Enabled: true,
				Params: map[string]interface{}{"target_bits": 8}},
		},
		AccuracyLossBudget: 10.0,
		TargetSizeRatio:    0.5,
	}
	p := NewCompressionPipeline(cfg, nil)
	w := synthWeights(20000, 2026)

	res, err := p.ExecuteOnWeights(context.Background(), "e2e-model", w)
	if err != nil {
		t.Fatalf("ExecuteOnWeights: %v", err)
	}
	if res.ExecutionMode != "measured" {
		t.Fatalf("ExecutionMode = %q, want \"measured\"", res.ExecutionMode)
	}
	if len(res.MeasuredStages) != 2 {
		t.Fatalf("measured stages = %d, want 2 (prune + quantize)", len(res.MeasuredStages))
	}
	if res.CompressedSize >= res.OriginalSize {
		t.Fatalf("compressed %d not smaller than original %d", res.CompressedSize, res.OriginalSize)
	}
	// Real prune(0.5)+int8: final encoding is int8-packed => ~25% of float32.
	ratio := float64(res.CompressedSize) / float64(res.OriginalSize)
	if ratio > 0.5 {
		t.Fatalf("measured size ratio %.3f exceeds 0.5", ratio)
	}
	// The reconstruction-error proxy must be positive (real transformation
	// loses information) and bounded (the pipeline is not garbage).
	if res.AccuracyLoss <= 0 || res.AccuracyLoss > 100 {
		t.Fatalf("relative RMSE %.3f%% outside (0,100]", res.AccuracyLoss)
	}
	// The modeled path must still work and be labeled honestly.
	modeled, err := p.Execute(context.Background(), "modeled-model", 100*1024*1024)
	if err != nil {
		t.Fatalf("Execute (modeled): %v", err)
	}
	if modeled.ExecutionMode != "modeled" {
		t.Fatalf("modeled ExecutionMode = %q, want \"modeled\"", modeled.ExecutionMode)
	}
}

// TestExecuteOnWeights_HonestSkip verifies stages without a real tensor
// executor are skipped with an honest reason instead of fabricating numbers.
func TestExecuteOnWeights_HonestSkip(t *testing.T) {
	cfg := CompressionPipelineConfig{
		Stages: []CompressionStageConfig{
			{Method: MethodKnowledgeDistillation, Order: 1, Enabled: true},
		},
		AccuracyLossBudget: 5.0,
		TargetSizeRatio:    0.5,
	}
	p := NewCompressionPipeline(cfg, nil)
	_, err := p.ExecuteOnWeights(context.Background(), "skip-model", synthWeights(100, 1))
	if err == nil {
		t.Fatal("expected error when no stage has a real executor, got nil")
	}
}

// TestExecuteOnWeights_EmptyTensor verifies input validation.
func TestExecuteOnWeights_EmptyTensor(t *testing.T) {
	p := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	if _, err := p.ExecuteOnWeights(context.Background(), "m", nil); err == nil {
		t.Fatal("expected error for empty tensor, got nil")
	}
}
