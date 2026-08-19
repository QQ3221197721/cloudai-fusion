package scheduler

import (
	"strings"
	"testing"
)

// TestQueueAwareSchemaLayout verifies the v2-queue-aware contract matches
// ai/scheduler/env_queue_aware.py `_build_obs()` exactly:
// per-node 9 features × N nodes + 5 workload features, in the same order.
func TestQueueAwareSchemaLayout(t *testing.T) {
	s := NewQueueAwareSchema(5)

	if s.Version != "v2-queue-aware" {
		t.Fatalf("version = %q, want v2-queue-aware", s.Version)
	}
	if s.FeaturesPerNode != 9 {
		t.Fatalf("FeaturesPerNode = %d, want 9", s.FeaturesPerNode)
	}
	if s.WorkloadFeatures != 5 {
		t.Fatalf("WorkloadFeatures = %d, want 5", s.WorkloadFeatures)
	}
	if s.ObsDim != 5*9+5 {
		t.Fatalf("ObsDim = %d, want %d", s.ObsDim, 5*9+5)
	}
	if s.ActionSpaceSize != 5 {
		t.Fatalf("ActionSpaceSize = %d, want 5 (discrete node selection)", s.ActionSpaceSize)
	}
	if len(s.FeatureNames) != s.ObsDim {
		t.Fatalf("len(FeatureNames) = %d, want ObsDim %d", len(s.FeatureNames), s.ObsDim)
	}

	// Per-node block order must mirror the Python env exactly.
	wantFirstNode := []string{
		"node0.gpu_util", "node0.mem_util", "node0.cpu_util", "node0.free_gpus",
		"node0.cost_per_hour", "node0.nvlink_score", "node0.queued_jobs",
		"node0.avg_wait_time", "node0.cluster_pressure",
	}
	for i, want := range wantFirstNode {
		if s.FeatureNames[i] != want {
			t.Fatalf("FeatureNames[%d] = %q, want %q", i, s.FeatureNames[i], want)
		}
	}

	// Workload tail order must mirror the Python env exactly.
	wantTail := []string{"gpus_needed", "priority", "job_type", "duration", "deadline_pressure"}
	tail := s.FeatureNames[len(s.FeatureNames)-5:]
	for i, want := range wantTail {
		if tail[i] != want {
			t.Fatalf("workload[%d] = %q, want %q", i, tail[i], want)
		}
	}
}

func TestSchemaValidateObs(t *testing.T) {
	s := NewQueueAwareSchema(3)

	good := make([]float64, s.ObsDim) // all zeros is valid (idle cluster)
	if err := s.ValidateObs(good); err != nil {
		t.Fatalf("ValidateObs(zeros) = %v, want nil", err)
	}
	for i := range good {
		good[i] = 0.5
	}
	if err := s.ValidateObs(good); err != nil {
		t.Fatalf("ValidateObs(0.5s) = %v, want nil", err)
	}

	badLen := make([]float64, s.ObsDim-1)
	if err := s.ValidateObs(badLen); err == nil {
		t.Fatal("ValidateObs(short vec) = nil, want length error")
	}

	badRange := make([]float64, s.ObsDim)
	badRange[RLNodeGPUUtil] = 1.5 // un-normalized percent leaked in
	err := s.ValidateObs(badRange)
	if err == nil || !strings.Contains(err.Error(), "outside [0,1]") {
		t.Fatalf("ValidateObs(1.5) = %v, want range error", err)
	}
}

func TestSchemaSlicesAndIndex(t *testing.T) {
	s := NewQueueAwareSchema(4)

	obs := make([]float64, s.ObsDim)
	// Mark node2.free_gpus feature
	idx, err := s.FeatureIndex(2, RLNodeFreeGPUs)
	if err != nil {
		t.Fatalf("FeatureIndex: %v", err)
	}
	if idx != 2*9+RLNodeFreeGPUs {
		t.Fatalf("FeatureIndex = %d, want %d", idx, 2*9+RLNodeFreeGPUs)
	}
	obs[idx] = 0.75

	node, err := s.NodeSlice(obs, 2)
	if err != nil {
		t.Fatalf("NodeSlice: %v", err)
	}
	if node[RLNodeFreeGPUs] != 0.75 {
		t.Fatalf("node[RLNodeFreeGPUs] = %f, want 0.75", node[RLNodeFreeGPUs])
	}

	if _, err := s.NodeSlice(obs, 4); err == nil {
		t.Fatal("NodeSlice(out of range) = nil error, want error")
	}

	wl, err := s.WorkloadSlice(obs)
	if err != nil {
		t.Fatalf("WorkloadSlice: %v", err)
	}
	if len(wl) != 5 {
		t.Fatalf("len(WorkloadSlice) = %d, want 5", len(wl))
	}
}

func TestSchemaJSONRenders(t *testing.T) {
	s := NewQueueAwareSchema(2)
	out, err := s.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	for _, want := range []string{"v2-queue-aware", "gpu_util", "deadline_pressure", "obs_dim"} {
		if !strings.Contains(out, want) {
			t.Fatalf("JSON output missing %q", want)
		}
	}
}
