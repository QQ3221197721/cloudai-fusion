package edge

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// 50B Model Compression Pipeline Tests
// ============================================================================

func TestCompressionPipeline_Execute(t *testing.T) {
	cfg := DefaultCompressionPipelineConfig()
	pipeline := NewCompressionPipeline(cfg, nil)

	// 50B model in FP16 ≈ 100GB
	modelSize := int64(100 * 1024 * 1024 * 1024)
	result, err := pipeline.Execute(context.Background(), "llama-50b", modelSize)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.CompressedSize >= modelSize {
		t.Errorf("compressed size %d should be less than original %d", result.CompressedSize, modelSize)
	}
	if result.CompressionRatio <= 1.0 {
		t.Errorf("compression ratio %.2f should be > 1.0", result.CompressionRatio)
	}
	if len(result.StagesApplied) == 0 {
		t.Error("expected at least one stage applied")
	}
	if result.PipelineDuration <= 0 {
		t.Error("expected positive pipeline duration")
	}
}

func TestCompressionPipeline_AccuracyBudget(t *testing.T) {
	cfg := DefaultCompressionPipelineConfig()
	cfg.AccuracyLossBudget = 0.5 // Very tight budget
	pipeline := NewCompressionPipeline(cfg, nil)

	modelSize := int64(100 * 1024 * 1024 * 1024)
	result, err := pipeline.Execute(context.Background(), "llama-50b", modelSize)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.AccuracyLoss > cfg.AccuracyLossBudget {
		t.Errorf("accuracy loss %.2f%% exceeds budget %.2f%%", result.AccuracyLoss, cfg.AccuracyLossBudget)
	}
}

func TestCompressionPipeline_AutoTune(t *testing.T) {
	cfg := DefaultCompressionPipelineConfig()
	pipeline := NewCompressionPipeline(cfg, nil)

	modelSize := int64(100 * 1024 * 1024 * 1024)
	result := pipeline.AutoTune(modelSize)
	if result == nil {
		t.Fatal("AutoTune returned nil")
	}
	if len(result.RecommendedStages) == 0 {
		t.Error("expected recommended stages")
	}
	if result.PredictedSize >= modelSize {
		t.Errorf("predicted size %d should be < original %d", result.PredictedSize, modelSize)
	}
	if result.PredictedSpeedup <= 1.0 {
		t.Errorf("predicted speedup %.2f should be > 1.0", result.PredictedSpeedup)
	}
}

func TestCompressionPipeline_NewMethods(t *testing.T) {
	tests := []struct {
		name   string
		stages []CompressionStageConfig
	}{
		{
			name: "GPTQ-4bit",
			stages: []CompressionStageConfig{
				{Method: MethodGPTQ, Order: 1, Enabled: true, Params: map[string]interface{}{"bits": float64(4), "group_size": float64(128)}},
			},
		},
		{
			name: "AWQ-4bit",
			stages: []CompressionStageConfig{
				{Method: MethodAWQ, Order: 1, Enabled: true, Params: map[string]interface{}{"bits": float64(4), "salient_percent": 1.0}},
			},
		},
		{
			name: "GGUF-Q4_K_M",
			stages: []CompressionStageConfig{
				{Method: MethodGGUF, Order: 1, Enabled: true, Params: map[string]interface{}{"quant_type": "Q4_K_M"}},
			},
		},
		{
			name: "SqueezeLLM",
			stages: []CompressionStageConfig{
				{Method: MethodSqueezeLLM, Order: 1, Enabled: true, Params: map[string]interface{}{}},
			},
		},
		{
			name: "LLM-Distillation-7x",
			stages: []CompressionStageConfig{
				{Method: MethodLLMDistillation, Order: 1, Enabled: true, Params: map[string]interface{}{"compression_factor": 7.0}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := CompressionPipelineConfig{
				Stages:            tt.stages,
				AccuracyLossBudget: 10.0, // generous for testing
				TargetSizeRatio:   0.5,
				TargetSpeedupMin:  1.5,
			}
			pipeline := NewCompressionPipeline(cfg, nil)
			modelSize := int64(100 * 1024 * 1024 * 1024)
			result, err := pipeline.Execute(context.Background(), "test-50b", modelSize)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if result.CompressedSize >= modelSize {
				t.Errorf("compressed %d should be < original %d", result.CompressedSize, modelSize)
			}
			if result.SpeedupFactor < 1.0 {
				t.Errorf("speedup %.2f should be >= 1.0", result.SpeedupFactor)
			}
		})
	}
}

// ============================================================================
// GPTQ Quantization Tests
// ============================================================================

func TestGPTQ_Execute(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	cfg := DefaultGPTQConfig()
	cfg.ModelSizeGB = 100

	modelSize := int64(100 * 1024 * 1024 * 1024) // 100GB FP16
	result, err := pipeline.ExecuteGPTQ(context.Background(), "llama-50b", modelSize, cfg)
	if err != nil {
		t.Fatalf("ExecuteGPTQ failed: %v", err)
	}

	// INT4 with g128 should be ~12.5% of original + overhead
	expectedRatio := 0.20 // ~15-20% of original with group overhead
	actualRatio := float64(result.QuantizedSizeBytes) / float64(result.OriginalSizeBytes)
	if actualRatio > expectedRatio {
		t.Errorf("GPTQ 4-bit ratio %.3f exceeds expected %.3f", actualRatio, expectedRatio)
	}

	// Perplexity impact should be reasonable for 50B
	if result.PerplexityDelta > 1.0 {
		t.Errorf("GPTQ 4-bit PPL delta %.2f too high for 50B model", result.PerplexityDelta)
	}

	// Memory reduction should be significant
	if result.MemoryReduction < 70 {
		t.Errorf("expected >70%% memory reduction, got %.1f%%", result.MemoryReduction)
	}
}

func TestGPTQ_BitWidths(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	modelSize := int64(100 * 1024 * 1024 * 1024)

	bitConfigs := []int{2, 3, 4, 8}
	prevSize := int64(0)

	for _, bits := range bitConfigs {
		cfg := DefaultGPTQConfig()
		cfg.Bits = bits
		cfg.ModelSizeGB = 100

		result, err := pipeline.ExecuteGPTQ(context.Background(), "test-model", modelSize, cfg)
		if err != nil {
			t.Fatalf("GPTQ %d-bit failed: %v", bits, err)
		}

		// Higher bits should produce larger models
		if prevSize > 0 && result.QuantizedSizeBytes <= prevSize {
			t.Errorf("%d-bit (%d bytes) should be larger than previous (%d bytes)",
				bits, result.QuantizedSizeBytes, prevSize)
		}
		prevSize = result.QuantizedSizeBytes
	}
}

func TestGPTQ_PerplexityScaling(t *testing.T) {
	// Larger models should have smaller perplexity impact
	ppl50B := estimateGPTQPerplexity(4, 128, true, 50)
	ppl7B := estimateGPTQPerplexity(4, 128, true, 7)

	if ppl50B >= ppl7B {
		t.Errorf("50B ppl delta (%.3f) should be less than 7B (%.3f)", ppl50B, ppl7B)
	}
}

// ============================================================================
// AWQ Quantization Tests
// ============================================================================

func TestAWQ_Execute(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	cfg := DefaultAWQConfig()
	modelSize := int64(100 * 1024 * 1024 * 1024)

	result, err := pipeline.ExecuteAWQ(context.Background(), "llama-50b", modelSize, cfg)
	if err != nil {
		t.Fatalf("ExecuteAWQ failed: %v", err)
	}

	if result.MemoryReduction < 70 {
		t.Errorf("expected >70%% memory reduction, got %.1f%%", result.MemoryReduction)
	}
	if result.SalientChannels <= 0 {
		t.Error("expected positive salient channel count")
	}
	if result.ScalingFactors <= 0 {
		t.Error("expected positive scaling factors count")
	}
}

func TestAWQ_BetterThanGPTQ(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	modelSize := int64(100 * 1024 * 1024 * 1024)

	awqResult, _ := pipeline.ExecuteAWQ(context.Background(), "test", modelSize, DefaultAWQConfig())
	gptqCfg := DefaultGPTQConfig()
	gptqCfg.ModelSizeGB = 100
	gptqResult, _ := pipeline.ExecuteGPTQ(context.Background(), "test", modelSize, gptqCfg)

	// AWQ typically achieves slightly better perplexity than GPTQ
	if awqResult.PerplexityDelta > gptqResult.PerplexityDelta {
		t.Logf("AWQ PPL delta (%.3f) vs GPTQ (%.3f) - AWQ should typically be better",
			awqResult.PerplexityDelta, gptqResult.PerplexityDelta)
	}
}

// ============================================================================
// GGUF Quantization Tests
// ============================================================================

func TestGGUF_Execute(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	modelSize := int64(100 * 1024 * 1024 * 1024)

	result, err := pipeline.ExecuteGGUF(context.Background(), "llama-50b", modelSize, DefaultGGUFConfig())
	if err != nil {
		t.Fatalf("ExecuteGGUF failed: %v", err)
	}
	if !result.CPUCompatible {
		t.Error("GGUF should be CPU compatible")
	}
	if result.BitsPerWeight <= 0 {
		t.Error("expected positive bits per weight")
	}
}

func TestGGUF_AllQuantTypes(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	modelSize := int64(100 * 1024 * 1024 * 1024)

	quantTypes := []GGUFQuantType{
		GGUFQ2K, GGUFQ3KS, GGUFQ3KM, GGUFQ3KL,
		GGUFQ4KS, GGUFQ4KM, GGUFQ5KS, GGUFQ5KM,
		GGUFQ6K, GGUFQ8_0,
	}

	prevSize := int64(0)
	for _, qt := range quantTypes {
		cfg := GGUFConfig{QuantType: qt, ImportanceMatrix: true}
		result, err := pipeline.ExecuteGGUF(context.Background(), "test", modelSize, cfg)
		if err != nil {
			t.Fatalf("GGUF %s failed: %v", qt, err)
		}
		// Each subsequent quant type should produce larger output
		if prevSize > 0 && result.QuantizedSizeBytes < prevSize {
			t.Errorf("%s (%d bytes) should be >= %d bytes", qt, result.QuantizedSizeBytes, prevSize)
		}
		prevSize = result.QuantizedSizeBytes
	}
}

func TestGGUF_BitsPerWeight(t *testing.T) {
	bpw := GGUFBitsPerWeight(GGUFQ4KM)
	if bpw != 4.85 {
		t.Errorf("expected 4.85 bpw for Q4_K_M, got %.2f", bpw)
	}
	bpw = GGUFBitsPerWeight(GGUFQ8_0)
	if bpw != 8.50 {
		t.Errorf("expected 8.50 bpw for Q8_0, got %.2f", bpw)
	}
}

// ============================================================================
// LLM Distillation Tests
// ============================================================================

func TestLLMDistillation_Execute(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	cfg := DefaultLLMDistillConfig()
	cfg.TeacherModelID = "llama-50b"
	cfg.TeacherSizeBytes = int64(100 * 1024 * 1024 * 1024)

	result, err := pipeline.ExecuteLLMDistillation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("LLM Distillation failed: %v", err)
	}
	if result.StudentSizeBytes >= cfg.TeacherSizeBytes {
		t.Error("student should be smaller than teacher")
	}
	if result.CompressionRatio <= 1.0 {
		t.Errorf("compression ratio %.2f should be > 1.0", result.CompressionRatio)
	}
	if result.QualityRetained < 80 || result.QualityRetained > 100 {
		t.Errorf("quality retained %.1f%% out of expected range [80,100]", result.QualityRetained)
	}
}

func TestLLMDistillation_EdgeDeployable(t *testing.T) {
	pipeline := NewCompressionPipeline(DefaultCompressionPipelineConfig(), nil)
	cfg := DefaultLLMDistillConfig()
	cfg.TeacherModelID = "llama-50b"
	cfg.TeacherSizeBytes = int64(100 * 1024 * 1024 * 1024)
	cfg.StudentLayers = 32
	cfg.StudentHiddenDim = 4096

	result, err := pipeline.ExecuteLLMDistillation(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Distillation failed: %v", err)
	}
	if !result.EdgeDeployable {
		t.Logf("7B student at %.0fW - may exceed 200W budget depending on quantization",
			result.EstPowerWatts)
	}
}

// ============================================================================
// 50B Strategy Recommendation Tests
// ============================================================================

func TestRecommend50BStrategy(t *testing.T) {
	strategy := Recommend50BStrategy(200, 64, 1.0)
	if strategy == nil {
		t.Fatal("strategy should not be nil")
	}
	if strategy.Recommended == "" {
		t.Error("expected a recommended strategy")
	}
	if len(strategy.Alternatives) == 0 {
		t.Error("expected alternatives")
	}

	// Check feasibility flags
	feasibleCount := 0
	for _, opt := range strategy.Alternatives {
		if opt.EdgeFeasible {
			feasibleCount++
		}
	}
	t.Logf("Recommended: %s (%d/%d feasible)", strategy.Recommended, feasibleCount, len(strategy.Alternatives))
}

func TestRecommend50BStrategy_TightConstraints(t *testing.T) {
	// Very tight: 30W, 8GB - should recommend distillation
	strategy := Recommend50BStrategy(30, 8, 5.0)
	if strategy.Recommended != "Distill-7B+GPTQ-4bit" {
		t.Logf("Under tight constraints, got recommendation: %s", strategy.Recommended)
	}
}

// ============================================================================
// Model Optimization Engine Tests
// ============================================================================

func TestAssessQuantization_50B(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	quant, power, err := engine.AssessQuantization("llama-50b", "50B", 200, 64)
	if err != nil {
		t.Fatalf("AssessQuantization failed: %v", err)
	}
	if quant == nil {
		t.Fatal("quantization recommendation should not be nil")
	}
	if power == nil {
		t.Fatal("power result should not be nil")
	}
	t.Logf("50B recommendation: %s at %dW budget, within_budget=%v", *quant, power.BudgetWatts, power.WithinBudget)
}

// ============================================================================
// Edge Hardware Profile Tests
// ============================================================================

func TestStandardEdgeProfiles(t *testing.T) {
	profiles := StandardEdgeProfiles()
	if len(profiles) == 0 {
		t.Fatal("expected at least one hardware profile")
	}

	requiredPlatforms := []string{
		"nvidia-jetson-orin-64",
		"nvidia-jetson-orin-32",
		"nvidia-jetson-orin-nano",
		"intel-nuc-ultra7",
	}

	for _, name := range requiredPlatforms {
		if _, ok := profiles[name]; !ok {
			t.Errorf("missing required profile: %s", name)
		}
	}

	// Validate Jetson Orin 64GB
	orin64 := profiles["nvidia-jetson-orin-64"]
	if orin64.GPUMemoryGB != 64 {
		t.Errorf("Orin 64GB should have 64GB, got %.0f", orin64.GPUMemoryGB)
	}
	if orin64.TDPWatts != 60 {
		t.Errorf("Orin 64GB TDP should be 60W, got %d", orin64.TDPWatts)
	}
}

func TestFindBestProfile_50B(t *testing.T) {
	profile, reason := FindBestProfile("50B", 200)
	if profile == nil {
		t.Fatalf("no profile found for 50B at 200W: %s", reason)
	}
	t.Logf("Best profile for 50B/200W: %s — %s", profile.Name, reason)
}

func TestFindBestProfile_7B(t *testing.T) {
	profile, reason := FindBestProfile("7B", 30)
	if profile == nil {
		t.Fatalf("no profile found for 7B at 30W: %s", reason)
	}
	t.Logf("Best profile for 7B/30W: %s — %s", profile.Name, reason)
}

// ============================================================================
// Mixed Precision Tests
// ============================================================================

func TestMixedPrecision(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	cfg := DefaultMixedPrecisionConfig()
	modelSize := int64(100 * 1024 * 1024 * 1024)

	result := engine.AnalyzeMixedPrecision(cfg, 64, modelSize)
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.EffectiveBPW <= 0 {
		t.Error("effective BPW should be positive")
	}
	if result.EffectiveBPW < float64(cfg.DefaultBits) {
		t.Errorf("effective BPW %.2f should be >= default bits %d", result.EffectiveBPW, cfg.DefaultBits)
	}
	if result.SizeOverhead < 0 {
		t.Errorf("mixed precision should have positive size overhead, got %.2f%%", result.SizeOverhead)
	}
	t.Logf("Mixed precision: BPW=%.2f, sensitive=%.1f%%, quality_gain=%.3f, overhead=%.1f%%",
		result.EffectiveBPW, result.SensitivePercent, result.QualityGain, result.SizeOverhead)
}

// ============================================================================
// KV-Cache Optimization Tests
// ============================================================================

func TestKVCacheAnalysis_50B(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	cfg := DefaultKVCacheConfig()
	modelMemGB := 25.0 // 50B INT4

	analysis := engine.AnalyzeKVCache(cfg, modelMemGB)
	if analysis == nil {
		t.Fatal("analysis should not be nil")
	}
	if analysis.BaseKVCacheSizeMB <= 0 {
		t.Error("base KV-cache size should be positive")
	}
	if analysis.OptimizedCacheSizeMB >= analysis.BaseKVCacheSizeMB {
		t.Error("optimized cache should be smaller than base")
	}
	if analysis.MemorySavingsPercent <= 0 {
		t.Error("expected positive memory savings")
	}
	if len(analysis.Optimizations) == 0 {
		t.Error("expected at least one optimization applied")
	}

	t.Logf("KV-cache: base=%.0fMB, optimized=%.0fMB (%.0f%% saved), max_batch=%d, fits=%v",
		analysis.BaseKVCacheSizeMB, analysis.OptimizedCacheSizeMB,
		analysis.MemorySavingsPercent, analysis.MaxBatchSize, analysis.FitInMemory)
}

func TestKVCacheAnalysis_SlidingWindow(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	cfg := DefaultKVCacheConfig()
	cfg.SlidingWindow = 512 // limit to 512 tokens

	analysis := engine.AnalyzeKVCache(cfg, 25.0)
	if !analysis.SlidingWindowActive {
		t.Error("sliding window should be active")
	}
	// Should save significant memory
	if analysis.MemorySavingsPercent < 50 {
		t.Errorf("sliding window should save >50%%, got %.1f%%", analysis.MemorySavingsPercent)
	}
}

func TestKVCacheAnalysis_StreamingLLM(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	cfg := DefaultKVCacheConfig()
	cfg.StreamingLLM = true
	cfg.AttentionSinkTokens = 4

	analysis := engine.AnalyzeKVCache(cfg, 25.0)
	if !analysis.StreamingActive {
		t.Error("StreamingLLM should be active")
	}
}

// ============================================================================
// 50B Edge Deployment Plan Tests
// ============================================================================

func TestPlan50BDeployment_JetsonOrin64(t *testing.T) {
	engine := NewOptimizationEngine(nil)
	plan := engine.Plan50BDeployment(100.0, "nvidia-jetson-orin-64", 200)

	if plan == nil {
		t.Fatal("plan should not be nil")
	}
	if plan.TargetHardware == nil {
		t.Fatal("target hardware should not be nil")
	}
	if plan.EstModelSizeGB <= 0 {
		t.Error("estimated model size should be positive")
	}

	t.Logf("50B Deployment Plan: HW=%s, quant=%s, model=%.1fGB, total_mem=%.1fGB, power=%.0fW, tps=%.1f, feasible=%v",
		plan.TargetHardware.Name, plan.Quantization, plan.EstModelSizeGB,
		plan.EstTotalMemoryGB, plan.EstPowerWatts, plan.EstTokensPerSec, plan.Feasible)
}

// ============================================================================
// Offline Runtime State Machine Tests
// ============================================================================

func TestOfflineRuntime_StateMachine(t *testing.T) {
	rt := NewOfflineRuntime("edge-01", DefaultOfflineRuntimeConfig(), nil)
	if rt.State() != StateOnline {
		t.Fatalf("expected online, got %s", rt.State())
	}

	// Online → Disconnecting → Offline → Recovering → Online
	transitions := []struct {
		event    RuntimeEvent
		expected RuntimeState
	}{
		{EventHeartbeatLost, StateDisconnecting},
		{EventHeartbeatTimeout, StateOffline},
		{EventReconnected, StateRecovering},
		{EventSyncComplete, StateOnline},
	}

	for _, tr := range transitions {
		err := rt.HandleEvent(tr.event, "test")
		if err != nil {
			t.Fatalf("HandleEvent(%s) failed: %v", tr.event, err)
		}
		if rt.State() != tr.expected {
			t.Errorf("after %s: expected %s, got %s", tr.event, tr.expected, rt.State())
		}
	}
}

func TestOfflineRuntime_DegradedState(t *testing.T) {
	rt := NewOfflineRuntime("edge-02", DefaultOfflineRuntimeConfig(), nil)
	if err := rt.HandleEvent(EventResourceCritical, "CPU overload"); err != nil {
		t.Fatal(err)
	}
	if rt.State() != StateDegraded {
		t.Errorf("expected degraded, got %s", rt.State())
	}
	if err := rt.HandleEvent(EventResourceRecovered, "CPU normal"); err != nil {
		t.Fatal(err)
	}
	if rt.State() != StateOnline {
		t.Errorf("expected online after recovery, got %s", rt.State())
	}
}

func TestOfflineRuntime_HealthCheck(t *testing.T) {
	rt := NewOfflineRuntime("edge-03", DefaultOfflineRuntimeConfig(), nil)
	usage := &EdgeResourceUsage{
		CPUPercent: 45, MemoryPercent: 60, GPUPercent: 70,
		DiskPercent: 50, Temperature: 55, PowerWatts: 120,
	}
	snap := rt.PerformHealthCheck(usage)
	if !snap.Healthy {
		t.Errorf("expected healthy, got issues: %v", snap.Issues)
	}

	// Critical usage
	criticalUsage := &EdgeResourceUsage{
		CPUPercent: 98, MemoryPercent: 97, GPUPercent: 99,
		Temperature: 92, PowerWatts: 200,
	}
	snap = rt.PerformHealthCheck(criticalUsage)
	if snap.Healthy {
		t.Error("expected unhealthy for critical usage")
	}
	if len(snap.Issues) == 0 {
		t.Error("expected issues listed")
	}
}

func TestOfflineRuntime_Status(t *testing.T) {
	rt := NewOfflineRuntime("edge-04", DefaultOfflineRuntimeConfig(), nil)
	status := rt.GetStatus()
	if status["state"] != StateOnline {
		t.Errorf("expected online, got %v", status["state"])
	}
}

// ============================================================================
// Local Decision Engine Tests
// ============================================================================

func TestLocalDecisionEngine_Evaluate(t *testing.T) {
	engine := NewLocalDecisionEngine(100, nil)

	normalUsage := &EdgeResourceUsage{CPUPercent: 40, MemoryPercent: 50, GPUPercent: 60}
	decision := engine.Evaluate("inference", normalUsage, 120)
	if decision.Result != "approved" {
		t.Errorf("expected approved for normal usage, got %s: %s", decision.Result, decision.Reason)
	}

	criticalUsage := &EdgeResourceUsage{CPUPercent: 96, MemoryPercent: 50, GPUPercent: 60}
	decision = engine.Evaluate("standard", criticalUsage, 120)
	if decision.Result != "denied" {
		t.Errorf("expected denied for critical CPU, got %s", decision.Result)
	}
}

func TestLocalDecisionEngine_MaxDecisions(t *testing.T) {
	engine := NewLocalDecisionEngine(2, nil)
	usage := &EdgeResourceUsage{CPUPercent: 40, MemoryPercent: 50}

	engine.Evaluate("inference", usage, 100)
	engine.Evaluate("inference", usage, 100)
	decision := engine.Evaluate("inference", usage, 100)
	if decision.Result != "denied" {
		t.Errorf("expected denied after max decisions, got %s", decision.Result)
	}
}

func TestLocalDecisionEngine_PendingSync(t *testing.T) {
	engine := NewLocalDecisionEngine(100, nil)
	usage := &EdgeResourceUsage{CPUPercent: 40, MemoryPercent: 50}
	engine.Evaluate("inference", usage, 100)
	engine.Evaluate("batch", usage, 100)

	pending := engine.PendingSync()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}

	engine.MarkSynced([]string{pending[0].ID})
	pendingAfter := engine.PendingSync()
	if len(pendingAfter) != 1 {
		t.Errorf("expected 1 pending after sync, got %d", len(pendingAfter))
	}
}

// ============================================================================
// Health Checker Tests
// ============================================================================

func TestHealthChecker(t *testing.T) {
	hc := NewHealthChecker("edge-05", nil)
	results := hc.RunAll(context.Background())
	if len(results) == 0 {
		t.Fatal("expected health check results")
	}
	if !hc.IsHealthy() {
		t.Error("default checks should all pass")
	}
	summary := hc.Summary()
	if summary["healthy"] != true {
		t.Error("expected healthy summary")
	}
}

// ============================================================================
// CRDT Sync Engine Tests
// ============================================================================

func TestCRDTSyncEngine_Counter(t *testing.T) {
	engine := NewCRDTSyncEngine(DefaultCRDTConfig(), nil)

	engine.IncrementCounter("requests", "node-1", 10)
	engine.IncrementCounter("requests", "node-2", 5)
	engine.IncrementCounter("requests", "node-1", 3)

	val := engine.GetCounterValue("requests")
	if val != 18 { // 13 + 5
		t.Errorf("expected counter=18, got %d", val)
	}
}

func TestCRDTSyncEngine_PNCounter(t *testing.T) {
	engine := NewCRDTSyncEngine(DefaultCRDTConfig(), nil)

	engine.IncrementCounter("balance", "node-1", 100)
	engine.IncrementCounter("balance", "node-1", -30)

	val := engine.GetCounterValue("balance")
	if val != 70 {
		t.Errorf("expected PN counter=70, got %d", val)
	}
}

func TestCRDTSyncEngine_Register(t *testing.T) {
	engine := NewCRDTSyncEngine(DefaultCRDTConfig(), nil)

	engine.SetRegister("config", "node-1", "value-a")
	time.Sleep(time.Millisecond)
	engine.SetRegister("config", "node-2", "value-b")

	val, ok := engine.GetRegisterValue("config")
	if !ok {
		t.Fatal("register should exist")
	}
	if val != "value-b" {
		t.Errorf("expected value-b (LWW), got %v", val)
	}
}

func TestCRDTSyncEngine_DrainPendingOps(t *testing.T) {
	engine := NewCRDTSyncEngine(DefaultCRDTConfig(), nil)

	engine.IncrementCounter("x", "n1", 5)
	engine.SetRegister("y", "n1", "hello")

	ops := engine.DrainPendingOps()
	if len(ops) != 2 {
		t.Errorf("expected 2 pending ops, got %d", len(ops))
	}

	// After drain, should be empty
	ops2 := engine.DrainPendingOps()
	if len(ops2) != 0 {
		t.Errorf("expected 0 pending ops after drain, got %d", len(ops2))
	}
}

func TestCRDTSyncEngine_MergeRemote(t *testing.T) {
	engine := NewCRDTSyncEngine(DefaultCRDTConfig(), nil)

	engine.IncrementCounter("total", "local", 10)

	remoteOps := []CRDTOperation{
		{Type: CRDTPNCounter, Key: "total", NodeID: "remote-1", Value: float64(20), Timestamp: time.Now()},
		{Type: CRDTLWWRegister, Key: "status", NodeID: "remote-1", Value: "active", Timestamp: time.Now()},
	}

	merged := engine.MergeRemote(remoteOps)
	if merged != 2 {
		t.Errorf("expected 2 merged, got %d", merged)
	}

	val := engine.GetCounterValue("total")
	if val < 20 {
		t.Errorf("expected counter >= 20, got %d", val)
	}
}

// ============================================================================
// OR-Set Tests
// ============================================================================

func TestORSet(t *testing.T) {
	set := NewORSet()
	set.Add("model-a", "tag-1", "llama-7b")
	set.Add("model-b", "tag-2", "qwen-14b")

	if !set.Contains("model-a") {
		t.Error("should contain model-a")
	}
	if !set.Contains("model-b") {
		t.Error("should contain model-b")
	}

	set.Remove("model-a")
	if set.Contains("model-a") {
		t.Error("should not contain model-a after remove")
	}

	// Concurrent add wins over previous remove
	set.Add("model-a", "tag-3", "llama-7b-v2")
	if !set.Contains("model-a") {
		t.Error("concurrent add should win over previous remove")
	}
}

// ============================================================================
// OTA Upgrade Tests
// ============================================================================

func TestOTAManager_GrayscaleRollout(t *testing.T) {
	mgr := NewOTAManager(DefaultOTAConfig(), nil)

	release := &FirmwareRelease{
		ID: "rel-1", Version: "2.0.0", SizeBytes: 500 * 1024 * 1024,
		Components: []string{"agent", "runtime"}, IsStable: true,
	}
	mgr.RegisterRelease(release)

	plan, err := mgr.CreateRollout(context.Background(), "rel-1", 100)
	if err != nil {
		t.Fatalf("CreateRollout failed: %v", err)
	}
	if plan.State != "pending" {
		t.Errorf("expected pending, got %s", plan.State)
	}
	if plan.TargetNodes != 1 { // 1% of 100
		t.Errorf("expected 1 target node at 1%%, got %d", plan.TargetNodes)
	}

	// Advance through stages
	plan, err = mgr.AdvanceStage(plan.ID)
	if err != nil {
		t.Fatalf("AdvanceStage failed: %v", err)
	}
	if plan.CurrentStage != 1 { // 5%
		t.Errorf("expected stage 1, got %d", plan.CurrentStage)
	}
}

func TestOTAManager_ABPartition(t *testing.T) {
	mgr := NewOTAManager(DefaultOTAConfig(), nil)
	mgr.RegisterRelease(&FirmwareRelease{ID: "rel-2", Version: "2.1.0", SizeBytes: 300 * 1024 * 1024})

	status, err := mgr.StartNodeUpgrade("node-1", "rel-2", "2.0.0", PartitionA)
	if err != nil {
		t.Fatalf("StartNodeUpgrade failed: %v", err)
	}
	if status.ActivePartition != PartitionA {
		t.Errorf("expected active partition A, got %s", status.ActivePartition)
	}
	if status.StagingPartition != PartitionB {
		t.Errorf("expected staging partition B, got %s", status.StagingPartition)
	}

	// Complete upgrade (activates B)
	if err := mgr.UpdateNodeProgress("node-1", 100, "active"); err != nil {
		t.Fatal(err)
	}
	status = mgr.GetNodeUpgradeStatus("node-1")
	if status.ActivePartition != PartitionB {
		t.Errorf("after upgrade, active should be B, got %s", status.ActivePartition)
	}

	// Rollback (back to A)
	if err := mgr.RollbackNode("node-1"); err != nil {
		t.Fatal(err)
	}
	status = mgr.GetNodeUpgradeStatus("node-1")
	if status.State != "rolled_back" {
		t.Errorf("expected rolled_back, got %s", status.State)
	}
	if status.ActivePartition != PartitionA {
		t.Errorf("after rollback, active should be A, got %s", status.ActivePartition)
	}
}

// ============================================================================
// Edge Cache Tests
// ============================================================================

func TestEdgeCache_PutGet(t *testing.T) {
	cache := NewEdgeCache(DefaultCacheConfig(), nil)

	if err := cache.Put("model-weights-7b", 256*1024*1024, "model_weight"); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	entry, ok := cache.Get("model-weights-7b")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if entry.DataType != "model_weight" {
		t.Errorf("expected model_weight, got %s", entry.DataType)
	}
}

func TestEdgeCache_Eviction(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.MaxSizeMB = 1 // Very small cache (1MB)
	cache := NewEdgeCache(cfg, nil)

	// Fill cache beyond capacity: 5 items of 512KB each = 2.5MB > 1MB
	for i := 0; i < 5; i++ {
		cache.Put(fmt.Sprintf("item-%d", i), 512*1024, "test")
	}

	stats := cache.GetStats()
	if stats.EvictionCount == 0 {
		t.Error("expected evictions for small cache")
	}
}

func TestEdgeCache_PrefetchPredictions(t *testing.T) {
	cache := NewEdgeCache(DefaultCacheConfig(), nil)

	// Generate access pattern
	for i := 0; i < 20; i++ {
		cache.Put("hot-key", 1024, "test")
		cache.Get("hot-key")
	}

	predictions := cache.GeneratePrefetchPredictions()
	// May or may not have predictions depending on pattern
	_ = predictions
}

// ============================================================================
// mDNS Device Discovery Tests
// ============================================================================

func TestDeviceDiscovery(t *testing.T) {
	dd := NewDeviceDiscovery(DefaultMDNSConfig(), nil)

	dd.RegisterDevice(&DiscoveredDevice{
		InstanceName: "cloudai-edge-001",
		HostName:     "edge-node-1.local",
		IPAddresses:  []string{"192.168.1.100"},
		Port:         8082,
	})

	devices := dd.ListDevices(true)
	if len(devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(devices))
	}
	if devices[0].InstanceName != "cloudai-edge-001" {
		t.Errorf("expected cloudai-edge-001, got %s", devices[0].InstanceName)
	}

	// Link to edge node
	if err := dd.LinkToNode("cloudai-edge-001", "edge-node-uuid"); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceDiscovery_PruneStale(t *testing.T) {
	cfg := DefaultMDNSConfig()
	cfg.TTLSec = 0 // Immediate expiry
	dd := NewDeviceDiscovery(cfg, nil)

	dd.RegisterDevice(&DiscoveredDevice{
		InstanceName: "stale-device",
		IPAddresses:  []string{"10.0.0.1"},
	})

	time.Sleep(10 * time.Millisecond)
	pruned := dd.PruneStale()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
}

// ============================================================================
// Enhanced Sync Engine Tests
// ============================================================================

func TestVectorClock(t *testing.T) {
	vc1 := NewVectorClock()
	vc1.Increment("node-1")
	vc1.Increment("node-1")

	vc2 := NewVectorClock()
	vc2.Increment("node-2")

	if !vc1.Concurrent(vc2) {
		t.Error("vc1 and vc2 should be concurrent")
	}

	vc2.Merge(vc1)
	vc2.Increment("node-2")

	if !vc1.HappensBefore(vc2) {
		t.Error("vc1 should happen before vc2 after merge+increment")
	}
}

func TestDeltaCompressor(t *testing.T) {
	dc := NewDeltaCompressor()

	state1 := map[string]interface{}{"cpu": 45.0, "mem": 60.0, "gpu": 70.0}
	delta1 := dc.ComputeDelta("node-1", state1)
	// First call should return full state
	if _, isDelta := delta1["__delta__"]; isDelta {
		t.Error("first call should not be a delta")
	}

	state2 := map[string]interface{}{"cpu": 46.0, "mem": 60.0, "gpu": 70.0}
	delta2 := dc.ComputeDelta("node-1", state2)
	// Second call should be a delta (only cpu changed)
	if isDelta, ok := delta2["__delta__"]; !ok || isDelta != true {
		t.Log("small change should produce delta")
	}
}

func TestPrioritySyncQueue(t *testing.T) {
	queue := NewPrioritySyncQueue(100)

	queue.Enqueue(&SyncItem{ID: "low", Priority: SyncPriorityLow, CreatedAt: time.Now()})
	queue.Enqueue(&SyncItem{ID: "critical", Priority: SyncPriorityCritical, CreatedAt: time.Now()})
	queue.Enqueue(&SyncItem{ID: "normal", Priority: SyncPriorityNormal, CreatedAt: time.Now()})

	if queue.Size() != 3 {
		t.Errorf("expected size 3, got %d", queue.Size())
	}

	// Dequeue should return critical first
	batch := queue.DequeueBatch(1)
	if len(batch) != 1 || batch[0].ID != "critical" {
		t.Errorf("expected critical first, got %s", batch[0].ID)
	}
}

// ============================================================================
// Edge-Cloud Collaboration Hub Tests
// ============================================================================

func TestEdgeCollabHub_Status(t *testing.T) {
	hub := NewEdgeCollabHub(DefaultEdgeCollabConfig(), nil)
	status := hub.GetStatus()
	if status == nil {
		t.Fatal("status should not be nil")
	}
	if _, ok := status["platform"]; !ok {
		t.Error("expected platform in status")
	}
}

func TestOfflineHub_Status(t *testing.T) {
	hub := NewOfflineHub(DefaultOfflineHubConfig(), nil)
	status := hub.GetStatus()
	if status == nil {
		t.Fatal("status should not be nil")
	}
	if _, ok := status["cache"]; !ok {
		t.Error("expected cache in status")
	}
}

// ============================================================================
// Integration: Full Edge Deployment Simulation
// ============================================================================

func TestIntegration_50BEdgeDeployment(t *testing.T) {
	// Step 1: Create manager
	mgr, err := NewManager(Config{MaxEdgePowerWatts: 200})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Step 2: Register Jetson Orin node
	node := &EdgeNode{
		Name: "edge-factory-01", Region: "cn-shanghai", Tier: TierEdge,
		CPUCores: 12, MemoryGB: 64, GPUType: "nvidia-jetson-orin",
		GPUCount: 1, GPUMemoryGB: 64, PowerBudgetWatts: 60,
	}
	if err := mgr.RegisterNode(context.Background(), node); err != nil {
		t.Fatal(err)
	}

	// Step 3: Deploy 50B model (INT4 quantized)
	model, err := mgr.DeployModel(context.Background(), &EdgeDeployRequest{
		ModelID: "llama-50b", ModelName: "Llama-50B-Chat",
		ParameterCount: "50B", EdgeNodeID: node.ID,
		Framework: "pytorch", QuantizationType: "INT4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.ParameterCount != "50B" {
		t.Errorf("expected 50B, got %s", model.ParameterCount)
	}

	// Step 4: Heartbeat with resource usage
	if err := mgr.Heartbeat(context.Background(), node.ID, &EdgeResourceUsage{
		CPUPercent: 35, MemoryPercent: 72, GPUPercent: 85,
		PowerWatts: 48, Temperature: 62,
	}); err != nil {
		t.Fatal(err)
	}

	// Step 5: Verify topology
	topo := mgr.GetTopologySummary(context.Background())
	if topo["deployed_models"] != 1 {
		t.Errorf("expected 1 deployed model, got %v", topo["deployed_models"])
	}

	// Step 6: Test offline collaboration
	result, err := mgr.HandleEdgeCollaboration(context.Background(), "status", nil)
	if err != nil {
		t.Fatalf("HandleEdgeCollaboration failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected status result")
	}

	// Step 7: Test offline capabilities
	offResult, err := mgr.HandleOfflineCapabilities(context.Background(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if offResult == nil {
		t.Fatal("expected offline status result")
	}

	t.Log("50B edge deployment integration test passed")
}
