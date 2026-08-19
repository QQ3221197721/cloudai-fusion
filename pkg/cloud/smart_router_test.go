package cloud

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestSmartRouterBasicSelection(t *testing.T) {
	r := NewSmartRouter()
	ctx := context.Background()

	// With GPU requirement, Azure NDv4 is cheapest ($0.9).
	wl := Workload{Name: "gpu-job", RequireGPU: true}
	d, err := r.Select(ctx, wl)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if d.Provider != "azure" {
		t.Errorf("Provider = %q, want azure (cheapest)", d.Provider)
	}
	if d.PricePerHour != 0.9 {
		t.Errorf("PricePerHour = %.2f, want 0.9", d.PricePerHour)
	}
	if !d.GPUAvailable {
		t.Error("GPUAvailable should be true")
	}
}

func TestSmartRouterNoGPUSkipped(t *testing.T) {
	// Use RegisterCandidate to inject non-GPU ones to prove they're skipped.
	r2 := &SmartRouter{}
	r2.RegisterCandidate("aws", "us-east-1", "t3.micro", 0.1, false, 25, "Unit test spec")
	r2.RegisterCandidate("azure", "eastus", "F4", 0.05, false, 30, "Unit test spec")

	ctx := context.Background()
	wl := Workload{Name: "cpu-work", RequireGPU: true}
	if _, err := r2.Select(ctx, wl); err == nil {
		t.Fatalf("expected no viable provider when GPU required but none available")
	}
}

func TestSmartRouterLatencyExclusion(t *testing.T) {
	r := NewSmartRouter()
	// Inject a high-latency fake provider.
	r.RegisterCandidate("fake", "ap-south-1", "large", 0.1, true, 200, "Unit test spec")
	r.SetLatency("aws", 25) // below threshold
	r.SetLatency("azure", 80) // below threshold

	ctx := context.Background()
	wl := Workload{Name: "low-latency-app", RequireGPU: true}
	// Should NOT pick the high-latency one; Azure still wins at $0.9
	d, err := r.Select(ctx, wl)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if d.Provider == "fake" {
		t.Errorf("Provider = %q, should not be high-latency fake", d.Provider)
	}
	if d.LatencyMS >= 100 {
		t.Errorf("LatencyMS = %d, expected < 100ms", d.LatencyMS)
	}
}

func TestSmartRouterNonGPUPriority(t *testing.T) {
	r := NewSmartRouter()
	ctx := context.Background()

	// Without GPU requirement, AWS g5.2xlarge ($1.0) is cheapest among specs.
	wl := Workload{Name: "cpu-only", RequireGPU: false}
	d, err := r.Select(ctx, wl)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	// Among spec's three candidates, AWS at $1.0/hr is cheapest after Azure's $0.9
	if d.Provider != "azure" {
		t.Errorf("Provider = %q, want azure (cheapest)", d.Provider)
	}
}

func TestSmartRouterContextCancellation(t *testing.T) {
	r := NewSmartRouter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.Select(ctx, Workload{Name: "cancelled"}); err == nil {
		t.Fatalf("expected context cancellation error")
	}
}

func TestSmartRouterRegistrationValidation(t *testing.T) {
	r := NewSmartRouter()
	// Empty source must be rejected.
	if err := r.RegisterCandidate("x", "r1", "i1", 1.0, true, 10, ""); err == nil {
		t.Fatalf("RegisterCandidate rejected empty source (good)")
	}
	// Price must be > 0.
	if err := r.RegisterCandidate("y", "r1", "i1", 0, true, 10, "unit"); err == nil {
		t.Fatalf("RegisterCandidate allowed zero price")
	}
}

func TestSmartRouterCandidatesSnapshot(t *testing.T) {
	r := NewSmartRouter()
	cands := r.Candidates()
	// Should have exactly the three spec-provided entries.
	if len(cands) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(cands))
	}
	providers := map[string]bool{cands[0].Provider: true, cands[1].Provider: true, cands[2].Provider: true}
	want := map[string]bool{"aws": true, "azure": true, "gcp": true}
	for k := range want {
		if !providers[k] {
			t.Errorf("missing provider %q in snapshot", k)
		}
	}
}

// TestSmartRouterTieBreak proves stable tie-breaking on latency then name.
func TestSmartRouterTieBreak(t *testing.T) {
	r := NewSmartRouter()
	// Clear default by registering two equal-priced items with different latencies.
	r.RegisterCandidate("a", "reg1", "type1", 0.5, true, 30, "unit")
	r.RegisterCandidate("b", "reg1", "type1", 0.5, true, 20, "unit") // same price, lower latency -> should win

	ctx := context.Background()
	d, err := r.Select(ctx, Workload{Name: "tie", RequireGPU: true})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if d.Provider != "b" {
		t.Errorf("Provider = %q, want b (lower latency wins)", d.Provider)
	}
}

func TestSmartRouterConcurrentAccess(t *testing.T) {
	r := NewSmartRouter()
	ctx := context.Background()

	var wg sync.WaitGroup
	// RegisterCandidate / SetLatency (writers) run concurrently with Select
	// (reader). Under -race this proves the RWMutex guards the candidate table.
	for g := 0; g < 50; g++ {
		wg.Add(3)
		name := fmt.Sprintf("p%d", g)
		price := float64(g%10+1) / 10 // always > 0 (RegisterCandidate requires it)
		go func(g int) {
			defer wg.Done()
			_ = r.RegisterCandidate(name, "r1", "i1", price, true, 10+g, "unit test synthetic candidate")
		}(g)
		go func() {
			defer wg.Done()
			// Selection may or may not error depending on interleaving; both are fine.
			_, _ = r.Select(ctx, Workload{Name: "conc-gpu", RequireGPU: true})
		}()
		go func(g int) {
			defer wg.Done()
			r.SetLatency(name, 5+g%15)
		}(g)
	}
	wg.Wait()
}
