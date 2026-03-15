// Package edge provides model optimization for edge deployment.
// Implements quantization assessment, power budget calculation,
// inference proxying (edge→cloud fallback), and version management.
package edge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// QuantizationType represents model quantization format
type QuantizationType string

const (
	QuantFP32   QuantizationType = "FP32"
	QuantFP16   QuantizationType = "FP16"
	QuantINT8   QuantizationType = "INT8"
	QuantINT4   QuantizationType = "INT4"
	QuantNF4    QuantizationType = "NF4"
	QuantGPTQ4  QuantizationType = "GPTQ4"  // GPTQ 4-bit
	QuantAWQ4   QuantizationType = "AWQ4"   // AWQ 4-bit
	QuantGGUF4  QuantizationType = "GGUF_Q4_K_M" // GGUF Q4_K_M
)

// ModelVersion tracks deployed model versions
type ModelVersion struct {
	VersionID      string            `json:"version_id"`
	ModelID        string            `json:"model_id"`
	ModelName      string            `json:"model_name"`
	SemanticVersion string           `json:"semantic_version"` // e.g., "1.2.3"
	ParameterCount string            `json:"parameter_count"`
	Quantization   QuantizationType  `json:"quantization"`
	ChecksumSHA256 string            `json:"checksum_sha256"`
	SizeBytes      int64             `json:"size_bytes"`
	CreatedAt      time.Time         `json:"created_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// PowerBudgetResult contains power consumption analysis
type PowerBudgetResult struct {
	ModelID              string  `json:"model_id"`
	EstimatedPowerWatts  float64 `json:"estimated_power_watts"`
	PeakPowerWatts       float64 `json:"peak_power_watts"`
	AvgPowerWatts        float64 `json:"avg_power_watts"`
	PowerPerInferenceW   float64 `json:"power_per_inference_watts"`
	WithinBudget         bool    `json:"within_budget"`
	BudgetWatts          int     `json:"budget_watts"`
	OptimizationSuggestions []string `json:"optimization_suggestions,omitempty"`
}

// InferenceProxy handles edge-to-cloud fallback
type InferenceProxy struct {
	cloudEndpoint string
	httpClient    *http.Client
	apiKey        string
	mu            sync.RWMutex
	requestCount  int64
	fallbackCount int64
	lastFallback  time.Time
}

// NewInferenceProxy creates a proxy for cloud fallback
func NewInferenceProxy(cloudEndpoint, apiKey string, timeout time.Duration) *InferenceProxy {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &InferenceProxy{
		cloudEndpoint: cloudEndpoint,
		apiKey:        apiKey,
		httpClient:    &http.Client{Timeout: timeout},
	}
}

// ProxyInference sends inference request to cloud when edge cannot handle
func (p *InferenceProxy) ProxyInference(ctx context.Context, modelID string, input interface{}) (interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.requestCount++
	p.mu.Unlock()

	// Check if cloud endpoint is configured
	if p.cloudEndpoint == "" {
		return nil, fmt.Errorf("cloud endpoint not configured for fallback")
	}

	// Prepare request
	payload := map[string]interface{}{
		"model_id": modelID,
		"input":    input,
		"source":   "edge_fallback",
	}

	jsonData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", p.cloudEndpoint+"/api/v1/inference", strings.NewReader(string(jsonData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.mu.Lock()
		p.fallbackCount++
		p.lastFallback = time.Now().UTC()
		p.mu.Unlock()
		return nil, fmt.Errorf("cloud fallback failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cloud returned HTTP %d", resp.StatusCode)
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode cloud response: %w", err)
	}

	return result, nil
}

// GetStats returns proxy statistics
func (p *InferenceProxy) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"total_requests":  p.requestCount,
		"fallback_count":  p.fallbackCount,
		"fallback_rate":   float64(p.fallbackCount) / float64(p.requestCount) * 100,
		"last_fallback":   p.lastFallback,
		"cloud_endpoint":  p.cloudEndpoint,
	}
}

// ============================================================================
// Model Optimization Engine
// ============================================================================

// OptimizationEngine assesses and optimizes models for edge deployment
type OptimizationEngine struct {
	logger *logrus.Logger
	mu     sync.RWMutex
}

// NewOptimizationEngine creates a new optimization engine
func NewOptimizationEngine(logger *logrus.Logger) *OptimizationEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &OptimizationEngine{
		logger: logger,
	}
}

// AssessQuantization recommends optimal quantization based on constraints
func (e *OptimizationEngine) AssessQuantization(modelName, paramCount string, powerBudgetWatts int, memoryGB float64) (*QuantizationType, *PowerBudgetResult, error) {
	// Parse parameter count
	params, err := parseParamCount(paramCount)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid parameter count: %w", err)
	}

	// Estimate power for different quantization levels
	estimates := e.estimatePowerConsumption(params, modelName)

	var recommended QuantizationType
	var withinBudget bool

	// Find most accurate quantization within budget
	for _, est := range estimates {
		if est.EstimatedPowerWatts <= float64(powerBudgetWatts) && est.AvgPowerWatts*memoryGB <= float64(powerBudgetWatts) {
			recommended = est.Quantization
			withinBudget = true
			break
		}
	}

	if recommended == "" {
		// Fallback to most aggressive quantization
		recommended = QuantINT4
		withinBudget = false
	}

	result := &PowerBudgetResult{
		ModelID:              modelName,
		EstimatedPowerWatts:  estimates[0].EstimatedPowerWatts,
		PeakPowerWatts:       estimates[0].PeakPowerWatts,
		AvgPowerWatts:        estimates[0].AvgPowerWatts,
		PowerPerInferenceW:   estimates[0].PowerPerInferenceW,
		WithinBudget:         withinBudget,
		BudgetWatts:          powerBudgetWatts,
	}

	if !withinBudget {
		result.OptimizationSuggestions = append(result.OptimizationSuggestions,
			fmt.Sprintf("Consider reducing parameters from %s to fit %dW budget", paramCount, powerBudgetWatts),
			"Enable dynamic voltage/frequency scaling",
			"Use batched inference to improve efficiency",
		)
	}

	return &recommended, result, nil
}

type powerEstimate struct {
	Quantization      QuantizationType
	EstimatedPowerWatts float64
	PeakPowerWatts    float64
	AvgPowerWatts     float64
	PowerPerInferenceW float64
}

func (e *OptimizationEngine) estimatePowerConsumption(paramsBillion float64, modelName string) []powerEstimate {
	// Base power estimates vary by model architecture
	baseMultiplier := getArchMultiplier(modelName)

	estimates := []powerEstimate{
		{
			Quantization:      QuantFP32,
			EstimatedPowerWatts: paramsBillion * baseMultiplier * 1.0,
			PeakPowerWatts:    paramsBillion * baseMultiplier * 1.5,
			AvgPowerWatts:     paramsBillion * baseMultiplier * 0.8,
			PowerPerInferenceW: paramsBillion * baseMultiplier * 0.01,
		},
		{
			Quantization:      QuantFP16,
			EstimatedPowerWatts: paramsBillion * baseMultiplier * 0.6,
			PeakPowerWatts:    paramsBillion * baseMultiplier * 0.9,
			AvgPowerWatts:     paramsBillion * baseMultiplier * 0.5,
			PowerPerInferenceW: paramsBillion * baseMultiplier * 0.006,
		},
		{
			Quantization:      QuantINT8,
			EstimatedPowerWatts: paramsBillion * baseMultiplier * 0.35,
			PeakPowerWatts:    paramsBillion * baseMultiplier * 0.5,
			AvgPowerWatts:     paramsBillion * baseMultiplier * 0.3,
			PowerPerInferenceW: paramsBillion * baseMultiplier * 0.003,
		},
		{
			Quantization:      QuantINT4,
			EstimatedPowerWatts: paramsBillion * baseMultiplier * 0.2,
			PeakPowerWatts:    paramsBillion * baseMultiplier * 0.3,
			AvgPowerWatts:     paramsBillion * baseMultiplier * 0.15,
			PowerPerInferenceW: paramsBillion * baseMultiplier * 0.0015,
		},
	}

	return estimates
}

func getArchMultiplier(modelName string) float64 {
	name := modelNameLower(modelName)
	switch {
	case contains(name, "transformer"), contains(name, "bert"), contains(name, "llama"):
		return 1.2 // Transformer models are more compute-intensive
	case contains(name, "cnn"), contains(name, "resnet"), contains(name, "vit"):
		return 1.0
	case contains(name, "rnn"), contains(name, "lstm"):
		return 0.8
	default:
		return 1.0
	}
}

func parseParamCount(paramCount string) (float64, error) {
	paramCount = strings.ToLower(strings.TrimSpace(paramCount))
	
	var multiplier float64 = 1.0
	if strings.HasSuffix(paramCount, "b") {
		multiplier = 1e9
		paramCount = strings.TrimSuffix(paramCount, "b")
	} else if strings.HasSuffix(paramCount, "m") {
		multiplier = 1e6
		paramCount = strings.TrimSuffix(paramCount, "m")
	}

	var value float64
	if _, err := fmt.Sscanf(paramCount, "%f", &value); err != nil {
		return 0, err
	}

	return value * multiplier / 1e9, nil // Return in billions
}

func modelNameLower(name string) string {
	return strings.ToLower(name)
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// CalculateChecksum computes SHA256 checksum of model data
func CalculateChecksum(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// CreateModelVersion creates a new model version with metadata
func CreateModelVersion(modelID, modelName, semanticVersion, paramCount string, quant QuantizationType, sizeBytes int64) *ModelVersion {
	return &ModelVersion{
		VersionID:       fmt.Sprintf("v-%s-%s", modelID, semanticVersion),
		ModelID:         modelID,
		ModelName:       modelName,
		SemanticVersion: semanticVersion,
		ParameterCount:  paramCount,
		Quantization:    quant,
		SizeBytes:       sizeBytes,
		CreatedAt:       time.Now().UTC(),
		Metadata:        make(map[string]string),
	}
}

// VersionManager manages deployed model versions
type VersionManager struct {
	versions map[string][]*ModelVersion // model_id -> versions
	current  map[string]string          // model_id -> current_version_id
	mu       sync.RWMutex
	logger   *logrus.Logger
}

// NewVersionManager creates a new version manager
func NewVersionManager(logger *logrus.Logger) *VersionManager {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &VersionManager{
		versions: make(map[string][]*ModelVersion),
		current:  make(map[string]string),
		logger:   logger,
	}
}

// RegisterVersion adds a new model version
func (m *VersionManager) RegisterVersion(version *ModelVersion) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.versions[version.ModelID] = append(m.versions[version.ModelID], version)
	m.logger.WithFields(logrus.Fields{
		"model":   version.ModelName,
		"version": version.SemanticVersion,
		"quant":   version.Quantization,
	}).Debug("Registered model version")
}

// SetCurrent sets the active version for a model
func (m *VersionManager) SetCurrent(modelID, versionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify version exists
	found := false
	for _, v := range m.versions[modelID] {
		if v.VersionID == versionID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("version %s not found for model %s", versionID, modelID)
	}

	m.current[modelID] = versionID
	m.logger.WithFields(logrus.Fields{
		"model":   modelID,
		"version": versionID,
	}).Info("Set current model version")

	return nil
}

// GetCurrent returns the current version for a model
func (m *VersionManager) GetCurrent(modelID string) (*ModelVersion, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	versionID, ok := m.current[modelID]
	if !ok {
		return nil, fmt.Errorf("no current version set for model %s", modelID)
	}

	for _, v := range m.versions[modelID] {
		if v.VersionID == versionID {
			return v, nil
		}
	}

	return nil, fmt.Errorf("current version %s not found", versionID)
}

// ListVersions returns all versions for a model
func (m *VersionManager) ListVersions(modelID string) []*ModelVersion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	versions := m.versions[modelID]
	result := make([]*ModelVersion, len(versions))
	copy(result, versions)
	return result
}

// Rollback reverts to a previous version
func (m *VersionManager) Rollback(modelID, targetVersionID string) error {
	// Verify target version exists
	versions := m.ListVersions(modelID)
	found := false
	for _, v := range versions {
		if v.VersionID == targetVersionID {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("target version %s not found", targetVersionID)
	}

	return m.SetCurrent(modelID, targetVersionID)
}

// ============================================================================
// 50B Model Edge Hardware Profiles
// ============================================================================

// EdgeHardwareProfile describes an edge hardware platform's capabilities.
type EdgeHardwareProfile struct {
	Name              string  `json:"name"`
	GPUType           string  `json:"gpu_type"`
	GPUMemoryGB       float64 `json:"gpu_memory_gb"`
	CPUArch           string  `json:"cpu_arch"`
	SystemMemoryGB    float64 `json:"system_memory_gb"`
	TDPWatts          int     `json:"tdp_watts"`
	MaxModelParams    string  `json:"max_model_params"`       // at INT4, e.g., "70B"
	SupportedRuntimes []string `json:"supported_runtimes"`
	ComputeCapability string  `json:"compute_capability"`     // CUDA CC for NVIDIA
	INT4TOPS          float64 `json:"int4_tops"`              // INT4 compute throughput
	INT8TOPS          float64 `json:"int8_tops"`
	FP16TFLOPS        float64 `json:"fp16_tflops"`
	MemoryBandwidthGB float64 `json:"memory_bandwidth_gbps"` // GB/s
	PCIeGen           int     `json:"pcie_gen"`
	Notes             string  `json:"notes"`
}

// StandardEdgeProfiles returns hardware profiles for common edge platforms.
func StandardEdgeProfiles() map[string]*EdgeHardwareProfile {
	return map[string]*EdgeHardwareProfile{
		"nvidia-jetson-orin-64": {
			Name: "NVIDIA Jetson AGX Orin 64GB", GPUType: "nvidia-jetson-orin",
			GPUMemoryGB: 64, CPUArch: "arm64", SystemMemoryGB: 64, TDPWatts: 60,
			MaxModelParams: "50B",
			SupportedRuntimes: []string{"tensorrt", "onnxruntime", "tflite"},
			ComputeCapability: "8.7", INT4TOPS: 275, INT8TOPS: 138, FP16TFLOPS: 69,
			MemoryBandwidthGB: 204.8, PCIeGen: 0, // integrated
			Notes: "Flagship edge AI: supports 50B INT4 with KV-cache offload",
		},
		"nvidia-jetson-orin-32": {
			Name: "NVIDIA Jetson AGX Orin 32GB", GPUType: "nvidia-jetson-orin",
			GPUMemoryGB: 32, CPUArch: "arm64", SystemMemoryGB: 32, TDPWatts: 40,
			MaxModelParams: "20B",
			SupportedRuntimes: []string{"tensorrt", "onnxruntime"},
			ComputeCapability: "8.7", INT4TOPS: 200, INT8TOPS: 100, FP16TFLOPS: 50,
			MemoryBandwidthGB: 204.8, PCIeGen: 0,
			Notes: "13B INT4 comfortable, 20B tight with 4-bit + KV-cache pruning",
		},
		"nvidia-jetson-orin-nano": {
			Name: "NVIDIA Jetson Orin Nano 8GB", GPUType: "nvidia-jetson-orin-nano",
			GPUMemoryGB: 8, CPUArch: "arm64", SystemMemoryGB: 8, TDPWatts: 15,
			MaxModelParams: "7B",
			SupportedRuntimes: []string{"tensorrt", "onnxruntime", "ncnn"},
			ComputeCapability: "8.7", INT4TOPS: 67, INT8TOPS: 34, FP16TFLOPS: 17,
			MemoryBandwidthGB: 68, PCIeGen: 0,
			Notes: "7B INT4 with KV-cache quantization",
		},
		"intel-nuc-ultra7": {
			Name: "Intel NUC Ultra 7 165H", GPUType: "intel-arc",
			GPUMemoryGB: 32, CPUArch: "x86_64", SystemMemoryGB: 32, TDPWatts: 65,
			MaxModelParams: "13B",
			SupportedRuntimes: []string{"openvino", "onnxruntime"},
			INT4TOPS: 0, INT8TOPS: 33, FP16TFLOPS: 16,
			MemoryBandwidthGB: 89.6, PCIeGen: 5,
			Notes: "OpenVINO optimized, good for 7B-13B with GGUF",
		},
		"rockchip-rk3588": {
			Name: "Rockchip RK3588 (Mali G610)", GPUType: "mali-g610",
			GPUMemoryGB: 0, CPUArch: "arm64", SystemMemoryGB: 16, TDPWatts: 10,
			MaxModelParams: "3B",
			SupportedRuntimes: []string{"ncnn", "mnn", "tflite"},
			INT4TOPS: 6, INT8TOPS: 3, FP16TFLOPS: 1.5,
			MemoryBandwidthGB: 25.6, PCIeGen: 0,
			Notes: "Low-power IoT edge, up to 3B with GGUF Q4",
		},
		"hailo-8l": {
			Name: "Hailo-8L AI Accelerator", GPUType: "hailo-8l",
			GPUMemoryGB: 0, CPUArch: "arm64", SystemMemoryGB: 8, TDPWatts: 5,
			MaxModelParams: "1B",
			SupportedRuntimes: []string{"hailo-rt"},
			INT4TOPS: 13, INT8TOPS: 13, FP16TFLOPS: 0,
			MemoryBandwidthGB: 12.8, PCIeGen: 3,
			Notes: "Dedicated NPU for vision/NLP edge inference <5W",
		},
	}
}

// FindBestProfile selects the optimal hardware profile for a given model.
func FindBestProfile(paramCount string, powerBudgetW int) (*EdgeHardwareProfile, string) {
	params, err := parseParamCount(paramCount)
	if err != nil {
		return nil, "unable to parse parameter count"
	}

	// Estimate INT4 memory requirement: params * 4 bits / 8 = params/2 bytes
	// Plus KV-cache overhead: ~20% for 2048 context
	modelMemGB := params * 0.5 * 1.2 // INT4 + KV-cache overhead

	profiles := StandardEdgeProfiles()
	var best *EdgeHardwareProfile
	var bestReason string

	for _, p := range profiles {
		if float64(p.TDPWatts) > float64(powerBudgetW) {
			continue
		}
		totalMem := p.GPUMemoryGB
		if totalMem == 0 {
			totalMem = p.SystemMemoryGB
		}
		if modelMemGB > totalMem {
			continue
		}
		if best == nil || p.INT8TOPS > best.INT8TOPS {
			best = p
			bestReason = fmt.Sprintf("%s fits %.1fGB model in %.0fGB memory at %dW",
				p.Name, modelMemGB, totalMem, p.TDPWatts)
		}
	}

	if best == nil {
		return nil, fmt.Sprintf("no edge hardware fits %.1fB params at %dW budget", params, powerBudgetW)
	}
	return best, bestReason
}

// ============================================================================
// Layer-Wise Mixed Precision Quantization
// ============================================================================

// LayerQuantConfig configures per-layer quantization for sensitive layers.
type LayerQuantConfig struct {
	LayerPattern   string           `json:"layer_pattern"`   // regex for layer name
	Quantization   QuantizationType `json:"quantization"`
	Reason         string           `json:"reason"`
}

// MixedPrecisionConfig configures layer-wise mixed precision quantization.
type MixedPrecisionConfig struct {
	DefaultBits    int                `json:"default_bits"`      // e.g., 4
	SensitiveLayers []LayerQuantConfig `json:"sensitive_layers"` // layers needing higher precision
	EmbeddingBits  int                `json:"embedding_bits"`    // typically 8 for embeddings
	LMHeadBits     int                `json:"lm_head_bits"`      // typically 8 for output head
	AttentionQKBits int               `json:"attention_qk_bits"` // Q/K projections
}

// DefaultMixedPrecisionConfig returns optimized mixed-precision for 50B LLMs.
// Key insight: certain layers (embeddings, attention Q/K, lm_head) are more
// sensitive to quantization and benefit from higher precision.
func DefaultMixedPrecisionConfig() MixedPrecisionConfig {
	return MixedPrecisionConfig{
		DefaultBits:    4,
		EmbeddingBits:  8,
		LMHeadBits:     8,
		AttentionQKBits: 8,
		SensitiveLayers: []LayerQuantConfig{
			{LayerPattern: "embed_tokens", Quantization: QuantINT8, Reason: "embeddings are quantization-sensitive"},
			{LayerPattern: "lm_head", Quantization: QuantINT8, Reason: "output head affects token probabilities directly"},
			{LayerPattern: "layers.0.*", Quantization: QuantINT8, Reason: "first transformer layer impacts all subsequent layers"},
			{LayerPattern: "layers.*.self_attn.q_proj", Quantization: QuantINT8, Reason: "Q projection sensitive to quantization error"},
			{LayerPattern: "layers.*.self_attn.k_proj", Quantization: QuantINT8, Reason: "K projection affects attention patterns"},
		},
	}
}

// MixedPrecisionResult contains the analysis of mixed-precision quantization.
type MixedPrecisionResult struct {
	LayerConfigs     map[string]int `json:"layer_configs"`       // layer_name -> bits
	EffectiveBPW     float64        `json:"effective_bpw"`       // effective bits per weight
	TotalSizeBytes   int64          `json:"total_size_bytes"`
	SensitivePercent float64        `json:"sensitive_layer_percent"` // % layers at higher precision
	QualityGain      float64        `json:"quality_gain_vs_uniform"` // PPL improvement vs uniform quant
	SizeOverhead     float64        `json:"size_overhead_percent"`   // extra size vs uniform quant
}

// AnalyzeMixedPrecision plans per-layer quantization for optimal quality/size tradeoff.
func (e *OptimizationEngine) AnalyzeMixedPrecision(cfg MixedPrecisionConfig, totalLayers int, modelSizeBytes int64) *MixedPrecisionResult {
	layerConfigs := make(map[string]int)
	sensitiveCount := 0

	// Assign bits to each layer
	for i := 0; i < totalLayers; i++ {
		layerName := fmt.Sprintf("layers.%d", i)
		bits := cfg.DefaultBits

		// Check if this is a sensitive layer
		for _, sl := range cfg.SensitiveLayers {
			matched := false
			if sl.LayerPattern == fmt.Sprintf("layers.%d.*", i) {
				matched = true
			}
			if matched {
				switch sl.Quantization {
				case QuantINT8:
					bits = 8
				case QuantFP16:
					bits = 16
				default:
					bits = 8
				}
				sensitiveCount++
			}
		}
		layerConfigs[layerName] = bits
	}

	// Special layers
	layerConfigs["embed_tokens"] = cfg.EmbeddingBits
	layerConfigs["lm_head"] = cfg.LMHeadBits

	// Calculate effective BPW
	totalBits := 0
	for _, bits := range layerConfigs {
		totalBits += bits
	}
	effBPW := float64(totalBits) / float64(len(layerConfigs))

	// Size calculation
	uniformSize := int64(float64(modelSizeBytes) * float64(cfg.DefaultBits) / 32.0)
	mixedSize := int64(float64(modelSizeBytes) * effBPW / 32.0)

	// Quality gain: ~0.2 PPL improvement for mixed vs uniform 4-bit
	qualityGain := (effBPW - float64(cfg.DefaultBits)) * 0.15

	return &MixedPrecisionResult{
		LayerConfigs:     layerConfigs,
		EffectiveBPW:     effBPW,
		TotalSizeBytes:   mixedSize,
		SensitivePercent: float64(sensitiveCount) / float64(totalLayers) * 100,
		QualityGain:      qualityGain,
		SizeOverhead:     float64(mixedSize-uniformSize) / float64(uniformSize) * 100,
	}
}

// ============================================================================
// KV-Cache Optimization for Edge Inference
// ============================================================================

// KVCacheConfig configures KV-cache optimization for memory-constrained edge devices.
type KVCacheConfig struct {
	MaxSeqLen           int     `json:"max_seq_len"`           // context length
	NumLayers           int     `json:"num_layers"`
	NumHeads            int     `json:"num_heads"`
	HeadDim             int     `json:"head_dim"`
	NumKVHeads          int     `json:"num_kv_heads"`          // GQA: grouped query attention heads
	CachePrecision      string  `json:"cache_precision"`       // FP16, INT8, INT4
	PagedAttention      bool    `json:"paged_attention"`       // vLLM-style paged KV-cache
	PageSize            int     `json:"page_size"`             // tokens per page (16 default)
	SlidingWindow       int     `json:"sliding_window"`        // 0 = full attention
	CacheCompression    bool    `json:"cache_compression"`     // quantize KV-cache to INT8/INT4
	CacheBits           int     `json:"cache_bits"`            // quantization bits for cache (4 or 8)
	StreamingLLM        bool    `json:"streaming_llm"`         // StreamingLLM attention sink
	AttentionSinkTokens int     `json:"attention_sink_tokens"` // initial tokens to keep (4)
	AvailableMemoryGB   float64 `json:"available_memory_gb"`
}

// DefaultKVCacheConfig returns production config for 50B model on Jetson Orin.
func DefaultKVCacheConfig() KVCacheConfig {
	return KVCacheConfig{
		MaxSeqLen:           2048,
		NumLayers:           64,   // 50B model typical
		NumHeads:            64,
		HeadDim:             128,
		NumKVHeads:          8,    // GQA 8:1 ratio
		CachePrecision:      "INT8",
		PagedAttention:      true,
		PageSize:            16,
		SlidingWindow:       0,
		CacheCompression:    true,
		CacheBits:           8,
		StreamingLLM:        false,
		AttentionSinkTokens: 4,
		AvailableMemoryGB:   64,
	}
}

// KVCacheAnalysis contains the analysis of KV-cache memory requirements.
type KVCacheAnalysis struct {
	BaseKVCacheSizeMB    float64 `json:"base_kv_cache_size_mb"`    // FP16 full KV-cache
	OptimizedCacheSizeMB float64 `json:"optimized_cache_size_mb"` // after optimizations
	MemorySavingsPercent float64 `json:"memory_savings_percent"`
	MaxBatchSize         int     `json:"max_batch_size"`           // given memory budget
	MaxSeqLen            int     `json:"max_seq_len"`              // achievable context length
	GQAReduction         float64 `json:"gqa_reduction_factor"`     // reduction from GQA
	QuantReduction       float64 `json:"quant_reduction_factor"`   // reduction from cache quantization
	SlidingWindowActive  bool    `json:"sliding_window_active"`
	PagedActive          bool    `json:"paged_attention_active"`
	StreamingActive      bool    `json:"streaming_llm_active"`
	Optimizations        []string `json:"optimizations_applied"`
	FitInMemory          bool    `json:"fits_in_memory"`
	Notes                string  `json:"notes"`
}

// AnalyzeKVCache computes KV-cache memory requirements and recommends optimizations.
//
// KV-cache size formula:
//
//	Base: 2 * num_layers * num_kv_heads * head_dim * seq_len * bytes_per_element * batch_size
//	      (factor 2 for both K and V)
//
// Optimizations applied:
//  1. GQA (Grouped Query Attention): reduces KV heads (e.g., 64→8 = 8x reduction)
//  2. Cache quantization: INT8 = 2x reduction, INT4 = 4x reduction vs FP16
//  3. Paged attention: eliminates fragmentation, enables dynamic allocation
//  4. Sliding window: limits cache to window_size tokens
//  5. StreamingLLM: keeps only sink + recent tokens, enables infinite context
func (e *OptimizationEngine) AnalyzeKVCache(cfg KVCacheConfig, modelMemoryGB float64) *KVCacheAnalysis {
	// Base KV-cache size in FP16 (bytes)
	// 2 (K+V) * layers * kv_heads * head_dim * seq_len * 2 (FP16 bytes)
	baseBytesPerToken := float64(2 * cfg.NumLayers * cfg.NumKVHeads * cfg.HeadDim * 2) // FP16
	baseKVBytes := baseBytesPerToken * float64(cfg.MaxSeqLen)
	baseKVMB := baseKVBytes / 1024 / 1024

	optimizedBytes := baseKVBytes
	optimizations := make([]string, 0)

	// GQA reduction
	gqaFactor := 1.0
	if cfg.NumKVHeads < cfg.NumHeads {
		gqaFactor = float64(cfg.NumKVHeads) / float64(cfg.NumHeads)
		optimizations = append(optimizations,
			fmt.Sprintf("GQA %d:%d reduces KV-cache by %.0fx",
				cfg.NumHeads, cfg.NumKVHeads, 1.0/gqaFactor))
		// GQA is already factored into base calculation via NumKVHeads
	}

	// Cache quantization
	quantFactor := 1.0
	if cfg.CacheCompression && cfg.CacheBits > 0 && cfg.CacheBits < 16 {
		quantFactor = float64(cfg.CacheBits) / 16.0
		optimizedBytes *= quantFactor
		optimizations = append(optimizations,
			fmt.Sprintf("KV-cache quantization FP16→INT%d (%.1fx reduction)",
				cfg.CacheBits, 1.0/quantFactor))
	}

	// Sliding window
	slidingActive := false
	if cfg.SlidingWindow > 0 && cfg.SlidingWindow < cfg.MaxSeqLen {
		windowFactor := float64(cfg.SlidingWindow) / float64(cfg.MaxSeqLen)
		optimizedBytes *= windowFactor
		slidingActive = true
		optimizations = append(optimizations,
			fmt.Sprintf("Sliding window %d tokens (%.1fx reduction)",
				cfg.SlidingWindow, 1.0/windowFactor))
	}

	// StreamingLLM
	streamingActive := false
	if cfg.StreamingLLM && cfg.AttentionSinkTokens > 0 {
		effectiveTokens := cfg.AttentionSinkTokens + cfg.MaxSeqLen/4 // sink + recent quarter
		streamFactor := float64(effectiveTokens) / float64(cfg.MaxSeqLen)
		optimizedBytes *= streamFactor
		streamingActive = true
		optimizations = append(optimizations,
			fmt.Sprintf("StreamingLLM: %d sink + %d recent tokens",
				cfg.AttentionSinkTokens, cfg.MaxSeqLen/4))
	}

	if cfg.PagedAttention {
		optimizations = append(optimizations,
			fmt.Sprintf("Paged attention (page_size=%d): eliminates memory fragmentation",
				cfg.PageSize))
	}

	optimizedMB := optimizedBytes / 1024 / 1024

	// Calculate max batch size given memory budget
	availForKV := (cfg.AvailableMemoryGB - modelMemoryGB) * 1024 // MB available
	maxBatch := 1
	if optimizedMB > 0 {
		maxBatch = int(availForKV / optimizedMB)
		if maxBatch < 1 {
			maxBatch = 1
		}
	}

	fitsInMemory := (modelMemoryGB + optimizedMB/1024) <= cfg.AvailableMemoryGB

	notes := ""
	if !fitsInMemory {
		notes = fmt.Sprintf("WARNING: model (%.1fGB) + KV-cache (%.1fGB) exceeds %.1fGB memory",
			modelMemoryGB, optimizedMB/1024, cfg.AvailableMemoryGB)
	}

	return &KVCacheAnalysis{
		BaseKVCacheSizeMB:    baseKVMB,
		OptimizedCacheSizeMB: optimizedMB,
		MemorySavingsPercent: (1.0 - optimizedMB/baseKVMB) * 100,
		MaxBatchSize:         maxBatch,
		MaxSeqLen:            cfg.MaxSeqLen,
		GQAReduction:         gqaFactor,
		QuantReduction:       quantFactor,
		SlidingWindowActive:  slidingActive,
		PagedActive:          cfg.PagedAttention,
		StreamingActive:      streamingActive,
		Optimizations:        optimizations,
		FitInMemory:          fitsInMemory,
		Notes:                notes,
	}
}

// ============================================================================
// 50B Model Edge Deployment Planner
// ============================================================================

// EdgeDeploymentPlan is a comprehensive plan for deploying a 50B model to edge.
type EdgeDeploymentPlan struct {
	ModelParams         string              `json:"model_params"`
	TargetHardware      *EdgeHardwareProfile `json:"target_hardware"`
	Quantization        QuantizationType    `json:"quantization"`
	MixedPrecision      *MixedPrecisionResult `json:"mixed_precision,omitempty"`
	KVCacheAnalysis     *KVCacheAnalysis    `json:"kv_cache_analysis"`
	EstModelSizeGB      float64             `json:"est_model_size_gb"`
	EstTotalMemoryGB    float64             `json:"est_total_memory_gb"`
	EstPowerWatts       float64             `json:"est_power_watts"`
	EstTokensPerSec     float64             `json:"est_tokens_per_sec"`
	Feasible            bool                `json:"feasible"`
	Bottleneck          string              `json:"bottleneck"`
	Recommendations     []string            `json:"recommendations"`
}

// Plan50BDeployment creates a comprehensive deployment plan for a 50B model.
func (e *OptimizationEngine) Plan50BDeployment(modelSizeGB float64, targetHW string, powerBudgetW int) *EdgeDeploymentPlan {
	profiles := StandardEdgeProfiles()
	hw, ok := profiles[targetHW]
	if !ok {
		hw = profiles["nvidia-jetson-orin-64"]
	}

	// Default to INT4 quantization for 50B
	quant := QuantINT4
	quantSizeGB := modelSizeGB * 4.0 / 16.0 // FP16 to INT4

	// KV-cache analysis
	kvCfg := DefaultKVCacheConfig()
	kvCfg.AvailableMemoryGB = hw.GPUMemoryGB
	if hw.GPUMemoryGB == 0 {
		kvCfg.AvailableMemoryGB = hw.SystemMemoryGB
	}
	kvAnalysis := e.AnalyzeKVCache(kvCfg, quantSizeGB)

	totalMemGB := quantSizeGB + kvAnalysis.OptimizedCacheSizeMB/1024

	// Power estimate
	estPower := float64(hw.TDPWatts) * 0.8 // typical load

	// Token generation speed (memory-bandwidth-bound for LLMs)
	// tokens/sec ≈ memory_bandwidth / (2 * model_params_bytes_per_token)
	bytesPerToken := quantSizeGB * 1024 * 1024 * 1024 / 64 // rough per-layer
	estTPS := hw.MemoryBandwidthGB * 1024 * 1024 * 1024 / bytesPerToken
	if estTPS > 50 {
		estTPS = 50 // practical cap
	}
	if estTPS < 1 {
		estTPS = 1
	}

	feasible := totalMemGB <= kvCfg.AvailableMemoryGB && estPower <= float64(powerBudgetW)

	bottleneck := "none"
	if !feasible {
		if totalMemGB > kvCfg.AvailableMemoryGB {
			bottleneck = fmt.Sprintf("memory: need %.1fGB, have %.1fGB", totalMemGB, kvCfg.AvailableMemoryGB)
		} else {
			bottleneck = fmt.Sprintf("power: need %.0fW, budget %dW", estPower, powerBudgetW)
		}
	}

	recommendations := make([]string, 0)
	if !kvAnalysis.FitInMemory {
		recommendations = append(recommendations,
			"Enable KV-cache INT4 quantization to reduce memory footprint",
			"Consider sliding window attention to limit context length",
			"Use distillation to reduce model to 13B before quantization",
		)
	}
	if estTPS < 5 {
		recommendations = append(recommendations,
			"Enable speculative decoding for 2-3x token generation speedup",
			"Consider flash attention for reduced memory bandwidth",
		)
	}

	return &EdgeDeploymentPlan{
		ModelParams:      "50B",
		TargetHardware:   hw,
		Quantization:     quant,
		KVCacheAnalysis:  kvAnalysis,
		EstModelSizeGB:   quantSizeGB,
		EstTotalMemoryGB: totalMemGB,
		EstPowerWatts:    estPower,
		EstTokensPerSec:  estTPS,
		Feasible:         feasible,
		Bottleneck:       bottleneck,
		Recommendations:  recommendations,
	}
}
