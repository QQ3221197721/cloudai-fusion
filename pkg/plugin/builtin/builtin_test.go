package builtin

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

func TestGPUResourceFilterPlugin_Pass(t *testing.T) {
	p := NewGPUResourceFilterPlugin()
	w := &plugin.WorkloadInfo{GPUCount: 2}
	node := &plugin.NodeInfo{GPUFree: 4}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Errorf("node with 4 free GPUs should pass for 2 GPU request: %s", r.Reason)
	}
}

func TestGPUResourceFilterPlugin_Fail(t *testing.T) {
	p := NewGPUResourceFilterPlugin()
	w := &plugin.WorkloadInfo{GPUCount: 4}
	node := &plugin.NodeInfo{GPUFree: 2}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if r.IsSuccess() {
		t.Error("node with 2 free GPUs should fail for 4 GPU request")
	}
	if r.Code != plugin.Unschedulable {
		t.Errorf("code = %d, want Unschedulable", r.Code)
	}
}

func TestGPUResourceFilterPlugin_ZeroGPU(t *testing.T) {
	p := NewGPUResourceFilterPlugin()
	w := &plugin.WorkloadInfo{GPUCount: 0}
	node := &plugin.NodeInfo{GPUFree: 0}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Error("0 GPU request should always pass")
	}
}

func TestTaintTolerationFilterPlugin_NoTaints(t *testing.T) {
	p := NewTaintTolerationFilterPlugin()
	w := &plugin.WorkloadInfo{}
	node := &plugin.NodeInfo{Taints: nil}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Error("node with no taints should pass")
	}
}

func TestTaintTolerationFilterPlugin_WithTaints(t *testing.T) {
	p := NewTaintTolerationFilterPlugin()
	w := &plugin.WorkloadInfo{} // no tolerations
	node := &plugin.NodeInfo{Taints: []string{"gpu-only:NoSchedule"}}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if r.IsSuccess() {
		t.Error("tainted node should reject workload without tolerations")
	}
}

func TestTaintTolerationFilterPlugin_WithTolerations(t *testing.T) {
	p := NewTaintTolerationFilterPlugin()
	w := &plugin.WorkloadInfo{Labels: map[string]string{"tolerations": "gpu-only"}}
	node := &plugin.NodeInfo{Taints: []string{"gpu-only:NoSchedule"}}
	r := p.Filter(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Error("workload with tolerations should pass tainted node")
	}
}

func TestDefaultScorePlugin(t *testing.T) {
	p := NewDefaultScorePlugin()
	w := &plugin.WorkloadInfo{}
	node := &plugin.NodeInfo{
		GPUUtilization:   30.0,
		MemoryTotalBytes: 64 * 1024 * 1024 * 1024,
		MemoryFreeBytes:  32 * 1024 * 1024 * 1024,
	}
	score, r := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Fatalf("Score failed: %s", r.Reason)
	}
	if score < 0 || score > 100 {
		t.Errorf("score = %d, should be in [0, 100]", score)
	}
	if p.ScoreWeight() != 1 {
		t.Errorf("weight = %d, want 1", p.ScoreWeight())
	}
}

func TestDefaultScorePlugin_HighUtil(t *testing.T) {
	p := NewDefaultScorePlugin()
	w := &plugin.WorkloadInfo{}
	highUtil := &plugin.NodeInfo{GPUUtilization: 95.0, MemoryTotalBytes: 1, MemoryFreeBytes: 0}
	lowUtil := &plugin.NodeInfo{GPUUtilization: 10.0, MemoryTotalBytes: 100, MemoryFreeBytes: 80}
	scoreHigh, _ := p.Score(context.Background(), plugin.NewCycleState(), w, highUtil)
	scoreLow, _ := p.Score(context.Background(), plugin.NewCycleState(), w, lowUtil)
	if scoreLow <= scoreHigh {
		t.Errorf("low util node (%d) should score higher than high util node (%d)", scoreLow, scoreHigh)
	}
}

func TestCostAwareScorePlugin(t *testing.T) {
	p := NewCostAwareScorePlugin()
	w := &plugin.WorkloadInfo{}
	node := &plugin.NodeInfo{CostPerHour: 5.0}
	score, r := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Fatalf("Score failed: %s", r.Reason)
	}
	if score < 0 || score > 100 {
		t.Errorf("score = %d, should be in [0, 100]", score)
	}
}

func TestCostAwareScorePlugin_ZeroCost(t *testing.T) {
	p := NewCostAwareScorePlugin()
	w := &plugin.WorkloadInfo{}
	node := &plugin.NodeInfo{CostPerHour: 0}
	score, _ := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if score != 80 {
		t.Errorf("zero cost should give neutral score 80, got %d", score)
	}
}

func TestCostAwareScorePlugin_SpotBonus(t *testing.T) {
	p := NewCostAwareScorePlugin()
	w := &plugin.WorkloadInfo{}
	onDemand := &plugin.NodeInfo{CostPerHour: 10.0, IsSpot: false}
	spot := &plugin.NodeInfo{CostPerHour: 10.0, IsSpot: true}
	scoreOD, _ := p.Score(context.Background(), plugin.NewCycleState(), w, onDemand)
	scoreSP, _ := p.Score(context.Background(), plugin.NewCycleState(), w, spot)
	if scoreSP <= scoreOD {
		t.Errorf("spot (%d) should score higher than on-demand (%d)", scoreSP, scoreOD)
	}
}

func TestCostAwareScorePlugin_MaxCostConstraint(t *testing.T) {
	p := NewCostAwareScorePlugin()
	w := &plugin.WorkloadInfo{MaxCostPerHour: 5.0}
	node := &plugin.NodeInfo{CostPerHour: 10.0}
	score, _ := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if score != 0 {
		t.Errorf("over-budget node should score 0, got %d", score)
	}
}

func TestTopologyAwareScorePlugin_NoNVLink(t *testing.T) {
	p := NewTopologyAwareScorePlugin()
	w := &plugin.WorkloadInfo{RequireNVLink: false}
	node := &plugin.NodeInfo{TopologyScore: 0.9}
	score, r := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Fatalf("Score failed: %s", r.Reason)
	}
	if score != 50 {
		t.Errorf("non-nvlink workload should get neutral 50, got %d", score)
	}
}

func TestTopologyAwareScorePlugin_WithNVLink(t *testing.T) {
	p := NewTopologyAwareScorePlugin()
	w := &plugin.WorkloadInfo{RequireNVLink: true}
	node := &plugin.NodeInfo{TopologyScore: 0.8}
	score, r := p.Score(context.Background(), plugin.NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Fatalf("Score failed: %s", r.Reason)
	}
	if score != 80 {
		t.Errorf("topology 0.8 should give 80, got %d", score)
	}
	if p.ScoreWeight() != 2 {
		t.Errorf("topology weight = %d, want 2", p.ScoreWeight())
	}
}

func TestRegisterDefaults(t *testing.T) {
	reg := plugin.NewRegistry()
	err := RegisterDefaults(reg)
	if err != nil {
		t.Fatalf("RegisterDefaults: %v", err)
	}

	names := reg.Names()
	if len(names) != 5 {
		t.Errorf("expected 5 defaults, got %d: %v", len(names), names)
	}

	expected := map[string]bool{
		"gpu-resource-filter":     true,
		"taint-toleration-filter": true,
		"default-score":           true,
		"cost-aware-score":        true,
		"topology-aware-score":    true,
	}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected plugin: %s", n)
		}
	}
}

func TestRegisterDefaults_DuplicateErrors(t *testing.T) {
	reg := plugin.NewRegistry()
	RegisterDefaults(reg)
	err := RegisterDefaults(reg)
	if err == nil {
		t.Error("double RegisterDefaults should error (duplicates)")
	}
}

func TestFactories(t *testing.T) {
	factories := []struct {
		name    string
		factory func() (plugin.Plugin, error)
	}{
		{"GPUResourceFilter", GPUResourceFilterFactory},
		{"TaintTolerationFilter", TaintTolerationFilterFactory},
		{"DefaultScore", DefaultScoreFactory},
		{"CostAwareScore", CostAwareScoreFactory},
		{"TopologyAwareScore", TopologyAwareScoreFactory},
	}
	for _, f := range factories {
		p, err := f.factory()
		if err != nil {
			t.Errorf("%s factory: %v", f.name, err)
			continue
		}
		if p == nil {
			t.Errorf("%s factory returned nil", f.name)
		}
	}
}
