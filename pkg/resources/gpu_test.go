package resources_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/resources"
)

func TestGPUCollector_CollectGPUMetrics(t *testing.T) {
	collector := resources.NewGPUCollector()
	if collector == nil {
		t.Fatal("NewGPUCollector returned nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metrics, err := collector.CollectGPUMetrics(ctx)
	if err != nil {
		t.Logf("CollectGPUMetrics returned error (expected if no nvidia-smi): %v", err)
		return
	}

	if metrics == nil || len(metrics) == 0 {
		t.Skip("Skipping - no GPUs detected in this environment")
	}

	for i, m := range metrics {
		if m.ID < 0 {
			t.Errorf("Invalid GPU ID at index %d: %d", i, m.ID)
		}
		if m.State != "ready" && m.State != "" {
			t.Logf("Unexpected state for GPU %d: %s", m.ID, m.State)
		}
	}
}

func TestGPUCollector_ParseNvidiaSMI(t *testing.T) {
	collector := resources.NewGPUCollector()

	sampleOutput := `Index, Name, Total Memory (MB), Used Memory (MB), Free Memory (MB), GPU Utilization (%), Memory Utilization (%), Power Usage (W), Temperature (C), Fan Speed (%)
0, NVIDIA Tesla V100, 32768, 15360, 17408, 45, 60, 150, 75, 85`

	metrics, err := collector.ParseNvidiaSMI(sampleOutput)
	if err != nil {
		t.Fatalf("Failed to parse sample output: %v", err)
	}

	if len(metrics) == 0 {
		t.Error("Expected at least one metric from parsed output")
	}

	if metrics[0].Name != "NVIDIA Tesla V100" {
		t.Errorf("Expected name 'NVIDIA Tesla V100', got '%s'", metrics[0].Name)
	}
	if metrics[0].Utility > 100 || metrics[0].Utility < 0 {
		t.Errorf("Invalid utility percentage: %f", metrics[0].Utility)
	}
}

func TestMIGTopology_Discovery(t *testing.T) {
	collector := resources.NewGPUCollector()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	topologies, err := collector.DiscoverMIGTopology(ctx, []int{0})
	if err != nil {
		t.Logf("DiscoverMIGTopology returned error (expected if MIG disabled): %v", err)
		return
	}

	if topologies == nil {
		t.Error("Expected non-nil topologies slice")
	}

	for _, topo := range topologies {
		if topo.GPUID < 0 {
			t.Error("Invalid GPU ID in topology")
		}
		// Enabled can be true/false depending on hardware configuration
	}
}
