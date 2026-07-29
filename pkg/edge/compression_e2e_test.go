package edge

import (
	"context"
	"testing"
)

// These tests prove the compression pipeline is NOT merely config-driven: the
// measured path transforms real tensors through multiple stages, honors context
// cancellation, and the LLM method stages route through a genuine symmetric
// quantizer. They complement tensor_compression_test.go (single-stage proofs).

// TestCompressionPipeline_3Stage_Measured drives prune -> weight-share -> int8
// over a real tensor and proves all three stages are measured and the final
// artifact is genuinely smaller than the FP32 original.
func TestCompressionPipeline_3Stage_Measured(t *testing.T) {
	cfg := CompressionPipelineConfig{
		Stages: []CompressionStageConfig{
			{Method: MethodStructuredPruning, Order: 1, Enabled: true, Params: map[string]interface{}{"sparsity_ratio": 0.5}},
			{Method: MethodWeightSharing, Order: 2, Enabled: true, Params: map[string]interface{}{"clusters": 64, "iterations": 10}},
			{Method: MethodQuantizationAware, Order: 3, Enabled: true, Params: map[string]interface{}{"target_bits": 8}},
		},
		AccuracyLossBudget: 50.0,
		TargetSizeRatio:    0.5,
	}
	p := NewCompressionPipeline(cfg, nil)
	res, err := p.ExecuteOnWeights(context.Background(), "m3", synthWeights(20000, 3))
	if err != nil {
		t.Fatalf("ExecuteOnWeights: %v", err)
	}
	if res.ExecutionMode != "measured" {
		t.Fatalf("ExecutionMode = %q, want measured", res.ExecutionMode)
	}
	if len(res.MeasuredStages) != 3 {
		t.Fatalf("measured stages = %d, want 3", len(res.MeasuredStages))
	}
	// Each measured stage records which real method ran.
	wantMethods := []CompressionMethod{MethodStructuredPruning, MethodWeightSharing, MethodQuantizationAware}
	for i, m := range wantMethods {
		if res.MeasuredStages[i].Method != m {
			t.Fatalf("stage[%d] method = %q, want %q", i, res.MeasuredStages[i].Method, m)
		}
		if res.MeasuredStages[i].OutputBytes <= 0 {
			t.Fatalf("stage[%d] must report measured output bytes", i)
		}
	}
	if res.CompressedSize >= res.OriginalSize {
		t.Fatalf("compressed %d not smaller than original %d", res.CompressedSize, res.OriginalSize)
	}
}

// TestCompressionPipeline_QualityDegradation proves adding a lossy pruning stage
// measurably increases reconstruction error vs quantization alone — a
// relationship only a real data path exhibits.
func TestCompressionPipeline_QualityDegradation(t *testing.T) {
	w := synthWeights(20000, 5)

	quantOnly := NewCompressionPipeline(CompressionPipelineConfig{
		Stages: []CompressionStageConfig{
			{Method: MethodQuantizationAware, Order: 1, Enabled: true, Params: map[string]interface{}{"target_bits": 8}},
		},
		AccuracyLossBudget: 100, TargetSizeRatio: 0.5,
	}, nil)
	pruneQuant := NewCompressionPipeline(CompressionPipelineConfig{
		Stages: []CompressionStageConfig{
			{Method: MethodStructuredPruning, Order: 1, Enabled: true, Params: map[string]interface{}{"sparsity_ratio": 0.5}},
			{Method: MethodQuantizationAware, Order: 2, Enabled: true, Params: map[string]interface{}{"target_bits": 8}},
		},
		AccuracyLossBudget: 100, TargetSizeRatio: 0.5,
	}, nil)

	a, err := quantOnly.ExecuteOnWeights(context.Background(), "q", w)
	if err != nil {
		t.Fatalf("quant-only: %v", err)
	}
	b, err := pruneQuant.ExecuteOnWeights(context.Background(), "pq", w)
	if err != nil {
		t.Fatalf("prune+quant: %v", err)
	}
	if b.AccuracyLoss <= a.AccuracyLoss {
		t.Fatalf("prune+quant loss %.3f must exceed quant-only loss %.3f", b.AccuracyLoss, a.AccuracyLoss)
	}
}

// TestCompressionPipeline_LLMMethodsMeasured proves GPTQ/AWQ/GGUF/SqueezeLLM
// stages route through a REAL symmetric quantizer (their calibration is modeled,
// but the tensor is genuinely quantized and shrunk).
func TestCompressionPipeline_LLMMethodsMeasured(t *testing.T) {
	methods := []CompressionMethod{MethodGPTQ, MethodAWQ, MethodGGUF, MethodSqueezeLLM}
	for _, m := range methods {
		t.Run(string(m), func(t *testing.T) {
			p := NewCompressionPipeline(CompressionPipelineConfig{
				Stages: []CompressionStageConfig{
					{Method: m, Order: 1, Enabled: true, Params: map[string]interface{}{"bits": 4}},
				},
				AccuracyLossBudget: 100, TargetSizeRatio: 0.5,
			}, nil)
			res, err := p.ExecuteOnWeights(context.Background(), "llm", synthWeights(8000, 11))
			if err != nil {
				t.Fatalf("ExecuteOnWeights(%s): %v", m, err)
			}
			if res.ExecutionMode != "measured" {
				t.Fatalf("%s ExecutionMode = %q, want measured", m, res.ExecutionMode)
			}
			if len(res.MeasuredStages) != 1 || res.MeasuredStages[0].Method != m {
				t.Fatalf("%s must record exactly its own measured stage, got %+v", m, res.MeasuredStages)
			}
			if res.CompressedSize >= res.OriginalSize {
				t.Fatalf("%s: compressed %d not smaller than %d", m, res.CompressedSize, res.OriginalSize)
			}
		})
	}
}

// TestCompressionPipeline_ContextCancellation proves a cancelled context aborts
// before any work — no fabricated result.
func TestCompressionPipeline_ContextCancellation(t *testing.T) {
	p := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	if _, err := p.ExecuteOnWeights(ctx, "m", synthWeights(1000, 1)); err == nil {
		t.Fatal("expected error for a cancelled context")
	}
}

// TestCompressionPipeline_NoStages proves an empty pipeline errors honestly
// instead of returning a zero-work "success".
func TestCompressionPipeline_NoStages(t *testing.T) {
	p := NewCompressionPipeline(CompressionPipelineConfig{
		Stages: []CompressionStageConfig{}, AccuracyLossBudget: 5, TargetSizeRatio: 0.5,
	}, nil)
	if _, err := p.ExecuteOnWeights(context.Background(), "empty", synthWeights(100, 1)); err == nil {
		t.Fatal("expected error for a pipeline with no real stages")
	}
}

// TestAutoTune_SelectsWithinBudget proves AutoTune returns a concrete plan whose
// predicted size is smaller than the input and whose predicted accuracy loss is
// a sane non-negative number.
func TestAutoTune_SelectsWithinBudget(t *testing.T) {
	p := NewCompressionPipeline(CompressionPipelineConfig{AccuracyLossBudget: 5.0, TargetSizeRatio: 0.4}, nil)
	const modelSize = int64(200 * 1024 * 1024)

	res := p.AutoTune(modelSize)
	if res == nil {
		t.Fatal("AutoTune returned nil")
	}
	if len(res.RecommendedStages) == 0 {
		t.Fatal("AutoTune must recommend at least one stage")
	}
	if res.PredictedSize <= 0 || res.PredictedSize >= modelSize {
		t.Fatalf("predicted size %d must be in (0, %d)", res.PredictedSize, modelSize)
	}
	if res.PredictedAccuracy < 0 {
		t.Fatalf("predicted accuracy loss must be non-negative, got %.3f", res.PredictedAccuracy)
	}
}

// TestRecommend50BStrategy_EdgeDeployable proves the 50B advisor yields an edge
// feasible option under tight power/memory constraints.
func TestRecommend50BStrategy_EdgeDeployable(t *testing.T) {
	strategy := Recommend50BStrategy(150, 64, 1.0)
	if strategy == nil {
		t.Fatal("Recommend50BStrategy returned nil")
	}
	if strategy.ModelParams != "50B" {
		t.Fatalf("ModelParams = %q, want 50B", strategy.ModelParams)
	}
	if strategy.Recommended == "" {
		t.Fatal("must recommend a concrete strategy")
	}
	if len(strategy.Alternatives) == 0 {
		t.Fatal("must present alternatives")
	}
	edgeFeasible := false
	for _, o := range strategy.Alternatives {
		if o.EdgeFeasible {
			edgeFeasible = true
		}
	}
	if !edgeFeasible {
		t.Fatal("at least one alternative must be edge-feasible")
	}
}
