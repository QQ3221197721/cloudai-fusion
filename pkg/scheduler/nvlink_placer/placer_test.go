package scheduler

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// TestNVLinkPlacementBasic tests basic placement functionality
func TestNVLinkPlacementBasic(t *testing.T) {
	// Setup mock topology with NVLink connectivity
	topo := &NodeGPUTopology{
		HasNVLink: true,
		NVLinks: []scheduler.NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, LinkType: "NML", BandwidthGB: 600.0},
			{GPU1Index: 2, GPU2Index: 3, LinkType: "NML", BandwidthGB: 600.0},
		},
	}
	
	// Create discoverer wrapper
	discoverer := NewMockDiscoverer(topo)
	
	// Initialize placer
	placer := NewPlacer(discoverer)
	
	// Request placement for 4-GPU workload requiring NVLink
	result, err := placer.Place(context.Background(), WorkloadRequest{
		GPUCount:      4,
		RequireNVLink: true,
		MinBandwidth:  600.0, // Gbps (NVLink 3.0 theoretical max)
	})
	
	if err != nil {
		t.Fatalf("placement failed: %v", err)
	}
	
	// Verify result
	if !result.Fit {
		t.Errorf("expected fit=true, got false")
	}
	
	if result.Toposcore < 70 {
		t.Errorf("expected score >= 70, got %.2f", result.Toposcore)
	}
	
	if len(result.Reasons) == 0 {
		t.Error("expected non-empty reasons list")
	}
}

// TestNVLinkFilterPass simulates NVLink requirement + NVLink node scenario
func TestNVLinkFilterPass(t *testing.T) {
	// Mock topology with full NVLink mesh
	topo := &NodeGPUTopology{
		HasNVLink:   true,
		HasNVSwitch: true,
		NVLinks: []scheduler.NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, LinkType: "NVS", BandwidthGB: 600.0},
			{GPU1Index: 0, GPU2Index: 2, LinkType: "NVS", BandwidthGB: 600.0},
			{GPU1Index: 0, GPU2Index: 3, LinkType: "NVS", BandwidthGB: 600.0},
			{GPU1Index: 1, GPU2Index: 2, LinkType: "NVS", BandwidthGB: 600.0},
			{GPU1Index: 1, GPU2Index: 3, LinkType: "NVS", BandwidthGB: 600.0},
			{GPU1Index: 2, GPU2Index: 3, LinkType: "NVS", BandwidthGB: 600.0},
		},
	}
	
	discoverer := NewMockDiscoverer(topo)
	placer := NewPlacer(discoverer)
	
	result, err := placer.Place(context.Background(), WorkloadRequest{
		GPUCount:         4,
		RequireNVLink:    true,
		MinBandwidth:     600.0,
		PreferSameNode:   true,
		GPUAffinityGroup: "training-job-123",
	})
	
	if err != nil {
		t.Fatalf("placement failed: %v", err)
	}
	
	if !result.Fit {
		t.Errorf("expected fit=true for full mesh, got false")
	}
	
	if result.Toposcore < 95 {
		t.Errorf("expected high score for full mesh, got %.2f", result.Toposcore)
	}
}

// TestNVLinkFallbackNeutral handles case when topology unavailable
func TestNVLinkFallbackNeutral(t *testing.T) {
	// Create discoverer that returns error
	brokenDiscoverer := &BrokenTopologyReader{}
	
	placer := NewPlacer(brokenDiscoverer)
	
	result, err := placer.Place(context.Background(), WorkloadRequest{
		GPUCount:      8,
		RequireNVLink: true,
	})
	
	// Should not fail - graceful degradation to neutral baseline
	if err != nil {
		t.Errorf("expected no error for broken discoverer, got %v", err)
	}
	
	if result.Toposcore != 50.0 {
		t.Errorf("expected neutral score=50.0, got %.2f", result.Toposcore)
	}
	
	if result.Fit {
		t.Error("expected fit=false for unknown topology")
	}
}
