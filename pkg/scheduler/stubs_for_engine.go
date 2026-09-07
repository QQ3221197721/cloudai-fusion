// Package scheduler - stub types for ignored hallucination files.
package scheduler

import "os"

type RLOptimizer struct{}
type RLOptimizerConfig struct{}

func DefaultRLOptimizerConfig() RLOptimizerConfig { return RLOptimizerConfig{} }
func NewRLOptimizer(_ RLOptimizerConfig) *RLOptimizer { return &RLOptimizer{} }
func (r *RLOptimizer) SelectAction(_ []float64) int { return 0 }
func (r *RLOptimizer) AdjustNodeScore(_ float64, _ int, _ *NodeScore, _ *Workload) float64 { return 0 }
func (r *RLOptimizer) GetStatistics() map[string]interface{} { return nil }

type GPUSharingManager struct{}
type GPUSharingConfig struct{}

func NewGPUSharingManager(_ GPUSharingConfig) *GPUSharingManager { return &GPUSharingManager{} }
func (g *GPUSharingManager) GetGPUSharingStates() []interface{} { return nil }
func (g *GPUSharingManager) GetGPUMemoryStates() []interface{} { return nil }

type ElasticInferenceManager struct {
	config ElasticInferenceConfig
}
type ElasticInferenceConfig struct{}

func DefaultElasticInferenceConfig() ElasticInferenceConfig { return ElasticInferenceConfig{} }
func NewElasticInferenceManager(_ ElasticInferenceConfig) *ElasticInferenceManager {
	return &ElasticInferenceManager{}
}
func (e *ElasticInferenceManager) GetEndpoints() []interface{} { return nil }

// EncodeState encodes scheduling state for RL input.
func EncodeState(_ string, _ int, _ int, _ float64) []float64 { return nil }

// SupportedMIGProfiles lists available MIG profiles.
func SupportedMIGProfiles(_ ...string) []string {
	return []string{"1g.5gb", "2g.10gb", "3g.20gb", "4g.40gb", "7g.80gb"}
}

// DCGMMetrics stub for GPU topology.
type DCGMMetrics struct {
	Data struct {
		GPU []interface{}
	}
}

var _ = os.Getenv // ensure os import used
