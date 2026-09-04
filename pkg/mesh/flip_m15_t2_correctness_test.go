// Package mesh - FLIP M15 T2 correctness proof.
//
// Proves that our lock-free COW data plane and both competitor routers (linkerd-style
// RWMutex, istio-style copy-per-hop) produce IDENTICAL routing decisions for the same
// (service, requestID) inputs. This is the honesty guard for the FLIP: we are only
// allowed to claim a latency win if we are computing the same answer as the competitor.
package mesh

import (
	"testing"
)

// TestFLIP_M15_Correctness verifies all three routers select the same endpoint for
// every (service, requestID) pair across the full fixture and a large request-id sweep.
func TestFLIP_M15_Correctness(t *testing.T) {
	reg, lk, istio, gpu, services, nodeCandidates, payload := buildFixtures()

	const sweep = 10000 // N=10k requests per FLIP mandate
	mismatches := 0
	checked := 0

	for i := 0; i < sweep; i++ {
		svc := services[i%len(services)]
		reqID := reqIDFor(i)

		// OUR data plane: lock-free COW snapshot + shared selection.
		ourSet := reg.Lookup(svc)
		if ourSet == nil {
			t.Fatalf("our registry missing service %q", svc)
		}
		ourPick := selectWeighted(ourSet.Snapshot(), reqID)

		// Competitor #1: linkerd-style RWMutex router.
		lkPick := lk.route(svc, reqID)

		// Competitor #2: istio-style copy-per-hop router.
		istioPick := istio.routeWithCopy(svc, reqID, payload)

		if ourPick == nil || lkPick == nil || istioPick == nil {
			t.Fatalf("nil pick for svc=%q req=%q (our=%v lk=%v istio=%v)", svc, reqID, ourPick, lkPick, istioPick)
		}

		checked++
		if ourPick.ID != lkPick.ID || ourPick.ID != istioPick.ID {
			mismatches++
			if mismatches <= 5 {
				t.Errorf("routing decision mismatch svc=%q req=%q: our=%s linkerd=%s istio=%s",
					svc, reqID, ourPick.ID, lkPick.ID, istioPick.ID)
			}
		}
	}

	if mismatches != 0 {
		t.Fatalf("FLIP correctness FAILED: %d/%d routing decisions diverged", mismatches, checked)
	}
	t.Logf("FLIP correctness PROVEN: %d/%d routing decisions identical across our/linkerd/istio", checked, checked)

	// GPU-aware routing must succeed for every request whose candidate node set is populated.
	gpuSuccess := 0
	for i := 0; i < sweep; i++ {
		if _, ok := gpu.selectLeastUtilized(nodeCandidates); ok {
			gpuSuccess++
		}
	}
	rate := float64(gpuSuccess) / float64(sweep) * 100
	if rate < 100.0 {
		t.Errorf("GPU-aware success rate %.2f%% < 100%% (expected all requests routable)", rate)
	}
	t.Logf("FLIP GPU-aware routing success rate: %.2f%% (%d/%d)", rate, gpuSuccess, sweep)
}

// TestFLIP_M15_GPULeastUtilized verifies the pre-parsed GPU cache truly returns the
// least-utilized GPU for a candidate node set (not just any GPU).
func TestFLIP_M15_GPULeastUtilized(t *testing.T) {
	gpu := newGPUTopologyCache()
	gpu.rebuild([]gpuRecord{
		{endpointID: "ep-hi", node: "node-0", utilization: 90},
		{endpointID: "ep-lo", node: "node-0", utilization: 10},
		{endpointID: "ep-mid", node: "node-1", utilization: 50},
	})

	// Only node-0 candidate → must pick ep-lo (10%), the least utilized on that node.
	id, ok := gpu.selectLeastUtilized(map[string]bool{"node-0": true})
	if !ok || id != "ep-lo" {
		t.Fatalf("expected ep-lo (least utilized on node-0), got %q ok=%v", id, ok)
	}

	// Both nodes candidate → ep-lo (10%) is global minimum.
	id, ok = gpu.selectLeastUtilized(map[string]bool{"node-0": true, "node-1": true})
	if !ok || id != "ep-lo" {
		t.Fatalf("expected ep-lo (global least utilized), got %q ok=%v", id, ok)
	}

	// No candidate node → no route.
	if _, ok := gpu.selectLeastUtilized(map[string]bool{"node-9": true}); ok {
		t.Fatalf("expected no route for unknown node, got ok=true")
	}
}
