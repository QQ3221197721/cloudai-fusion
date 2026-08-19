// Package scheduler — rl_schema.go defines the unified observation feature
// contract shared between the Go scheduler and the Python RL environment.
//
// Week 1 §1.0 identified FOUR mutually incompatible RL stacks (Python-A
// 65-dim obs / 3-dim continuous action, Go-A hard-coded 50-dim input / 8-dim
// discrete output, etc.). This schema is the single source of truth that both
// sides align to: it mirrors ai/scheduler/env_queue_aware.py `_build_obs()`
// feature-for-feature so a Python-trained policy (tabular Q or ONNX export)
// can be served by Go with zero re-encoding drift.
//
// Contract (v2-queue-aware):
//
//	observation = [per-node block (9 features) × num_nodes] || [workload (5 features)]
//	per-node    = gpu_util, mem_util, cpu_util, free_gpus, cost_per_hour,
//	              nvlink_score, queued_jobs, avg_wait_time, cluster_pressure
//	workload    = gpus_needed, priority, job_type, duration, deadline_pressure
//	action      = Discrete(num_nodes) — direct node selection
//
// All features are normalized to [0,1] by the Python environment with FIXED
// ranges (no per-sample min-max — Week 1 §1.1.2 showed that breaks
// Markov-ness). The Go side MUST reproduce the same normalization constants
// when assembling observations from live cluster state.
package scheduler

import (
	"encoding/json"
	"fmt"
)

// Schema version identifiers. Bump the minor version when adding trailing
// features (backwards compatible), the major version when reordering or
// removing features (breaking).
const (
	RLSchemaVersionQueueAware = "v2-queue-aware"
	rlFeaturesPerNode         = 9
	rlWorkloadFeatures        = 5
)

// Per-node feature semantic indices (block offset within each node's 9-dim slice).
const (
	RLNodeGPUUtil         = 0 // gpu_util         /100
	RLNodeMemUtil         = 1 // mem_util         /100
	RLNodeCPUUtil         = 2 // cpu_util         /100
	RLNodeFreeGPUs        = 3 // free_gpus_ratio  /maxGPUsPerNode
	RLNodeCostPerHour     = 4 // cost_norm        /120 $/hr (whole node)
	RLNodeNVLinkScore     = 5 // nvlink_score     already [0,1] (P2P graph)
	RLNodeQueuedJobs      = 6 // queued_jobs_norm /maxPendingJobs
	RLNodeAvgWaitTime     = 7 // avg_wait_norm    /24h
	RLNodeClusterPressure = 8 // queue_depth/(N*10), clipped to [0,1]
)

// Workload feature semantic indices (offset = numNodes*9).
const (
	RLWorkloadGPUsNeeded       = 0 // gpus_needed        /8
	RLWorkloadPriority         = 1 // priority           /100
	RLWorkloadJobType          = 2 // job_type           /2 (0=train,1=infer,2=finetune)
	RLWorkloadDuration         = 3 // estimated_duration /10h
	RLWorkloadDeadlinePressure = 4 // deadline_pressure  already [0,1]
)

// RLFeatureSchema defines the unified observation feature contract
// (Go ↔ Python dual-end alignment). It documents each feature's meaning,
// normalization denominator, and layout so both runtimes can validate
// observation vectors and detect contract drift before it silently
// corrupts trained policies.
type RLFeatureSchema struct {
	Version           string   `json:"version"`
	NumNodes          int      `json:"num_nodes"`
	FeaturesPerNode   int      `json:"features_per_node"`
	WorkloadFeatures  int      `json:"workload_features"`
	MaxGPUsPerNode    int      `json:"max_gpus_per_node"`
	MaxPendingJobs    int      `json:"max_pending_jobs"`
	ActionSpaceSize   int      `json:"action_space_size"` // = num_nodes (discrete node selection)
	ObsDim            int      `json:"obs_dim"`           // = num_nodes*9 + 5
	FeatureNames      []string `json:"feature_names"`     // full expanded layout, in order
	NormalizationNote string   `json:"normalization_note"`
}

// NewQueueAwareSchema builds the v2-queue-aware feature schema for a cluster
// with numNodes schedulable nodes. Default capacity constants match the
// Python environment defaults (max_gpus_per_node=8, max_pending_jobs=50);
// override them via NewQueueAwareSchemaWithCapacity when the live cluster
// differs.
func NewQueueAwareSchema(numNodes int) *RLFeatureSchema {
	return NewQueueAwareSchemaWithCapacity(numNodes, 8, 50)
}

// NewQueueAwareSchemaWithCapacity is the fully-parameterized constructor.
func NewQueueAwareSchemaWithCapacity(numNodes, maxGPUsPerNode, maxPendingJobs int) *RLFeatureSchema {
	if numNodes <= 0 {
		numNodes = 1
	}
	if maxGPUsPerNode <= 0 {
		maxGPUsPerNode = 8
	}
	if maxPendingJobs <= 0 {
		maxPendingJobs = 50
	}

	s := &RLFeatureSchema{
		Version:          RLSchemaVersionQueueAware,
		NumNodes:         numNodes,
		FeaturesPerNode:  rlFeaturesPerNode,
		WorkloadFeatures: rlWorkloadFeatures,
		MaxGPUsPerNode:   maxGPUsPerNode,
		MaxPendingJobs:   maxPendingJobs,
		ActionSpaceSize:  numNodes,
		ObsDim:           numNodes*rlFeaturesPerNode + rlWorkloadFeatures,
	}
	s.FeatureNames = s.expandFeatureNames()
	s.NormalizationNote = "all features normalized to [0,1] with FIXED ranges " +
		"(per-sample min-max is forbidden — Week 1 §1.1.2 Markov-ness violation)"
	return s
}

// expandFeatureNames builds the full per-node + workload layout in
// observation order, mirroring Python _build_obs().
func (s *RLFeatureSchema) expandFeatureNames() []string {
	perNode := []string{
		"gpu_util", "mem_util", "cpu_util", "free_gpus",
		"cost_per_hour", "nvlink_score", "queued_jobs",
		"avg_wait_time", "cluster_pressure",
	}
	workload := []string{
		"gpus_needed", "priority", "job_type", "duration", "deadline_pressure",
	}

	names := make([]string, 0, s.ObsDim)
	for n := 0; n < s.NumNodes; n++ {
		for _, f := range perNode {
			names = append(names, fmt.Sprintf("node%d.%s", n, f))
		}
	}
	names = append(names, workload...)
	return names
}

// ValidateObs checks that an observation vector conforms to this schema:
// correct length and all values within [0,1] (the Python env guarantees
// clipping; a violation here means the Go-side encoder drifted).
func (s *RLFeatureSchema) ValidateObs(obs []float64) error {
	if len(obs) != s.ObsDim {
		return fmt.Errorf("rl_schema: obs length %d != expected %d (version %s, %d nodes)",
			len(obs), s.ObsDim, s.Version, s.NumNodes)
	}
	for i, v := range obs {
		if v < 0.0 || v > 1.0 {
			name := "<oob>"
			if i < len(s.FeatureNames) {
				name = s.FeatureNames[i]
			}
			return fmt.Errorf("rl_schema: obs[%d]=%f (%s) outside [0,1] — feature not normalized", i, v, name)
		}
	}
	return nil
}

// NodeSlice returns the 9 per-node features for nodeIdx from a flattened
// observation vector (no copy semantics change: it is a sub-slice view).
func (s *RLFeatureSchema) NodeSlice(obs []float64, nodeIdx int) ([]float64, error) {
	if nodeIdx < 0 || nodeIdx >= s.NumNodes {
		return nil, fmt.Errorf("rl_schema: nodeIdx %d out of range [0,%d)", nodeIdx, s.NumNodes)
	}
	start := nodeIdx * s.FeaturesPerNode
	return obs[start : start+s.FeaturesPerNode], nil
}

// WorkloadSlice returns the trailing 5 workload features from a flattened
// observation vector.
func (s *RLFeatureSchema) WorkloadSlice(obs []float64) ([]float64, error) {
	if len(obs) != s.ObsDim {
		return nil, fmt.Errorf("rl_schema: obs length %d != expected %d", len(obs), s.ObsDim)
	}
	return obs[s.NumNodes*s.FeaturesPerNode:], nil
}

// FeatureIndex maps a node index + per-node feature constant (e.g.
// RLNodeFreeGPUs) to its absolute index in the flattened observation.
func (s *RLFeatureSchema) FeatureIndex(nodeIdx, nodeFeature int) (int, error) {
	if nodeIdx < 0 || nodeIdx >= s.NumNodes {
		return 0, fmt.Errorf("rl_schema: nodeIdx %d out of range", nodeIdx)
	}
	if nodeFeature < 0 || nodeFeature >= s.FeaturesPerNode {
		return 0, fmt.Errorf("rl_schema: nodeFeature %d out of range", nodeFeature)
	}
	return nodeIdx*s.FeaturesPerNode + nodeFeature, nil
}

// JSON renders the schema for the shared contract document / contract tests
// (Python side loads and asserts equality with its own layout).
func (s *RLFeatureSchema) JSON() (string, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("rl_schema: marshal: %w", err)
	}
	return string(b), nil
}
