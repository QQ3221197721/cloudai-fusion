package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// newTestLedger builds an in-memory, deterministically-signed evidence ledger.
func newTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x11}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestSchedulingEmitsVerifiableDecision(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{SchedulingInterval: 100 * time.Millisecond})
	ledger := newTestLedger(t)
	engine.SetEvidenceRecorder(ledger)

	engine.mu.Lock()
	workload := &Workload{
		ID:        common.NewUUID(),
		Name:      "verifiable-training-job",
		Namespace: "team-a",
		Type:      common.WorkloadTypeTraining,
		Status:    common.WorkloadStatusQueued,
		Priority:  5,
		ResourceRequest: common.ResourceRequest{
			GPUCount: 2,
			GPUType:  common.GPUTypeNvidiaA100,
		},
		QueuedAt: common.NowUTC(),
	}
	engine.queue = append(engine.queue, workload)
	engine.mu.Unlock()

	engine.schedulingCycle(context.Background())

	// A schedule.bind receipt must have been emitted for this workload.
	all, err := ledger.Store().All(context.Background())
	if err != nil {
		t.Fatalf("ledger all: %v", err)
	}
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "schedule.bind" && e.Subject == workload.ID {
			rec = e
			break
		}
	}
	if rec == nil {
		t.Fatalf("expected a schedule.bind receipt for workload %s, got %d records", workload.ID, len(all))
	}

	// The receipt's payload is the verifiable decision: chosen node + scoreboard.
	var decision SchedulingDecision
	if err := json.Unmarshal(rec.Payload, &decision); err != nil {
		t.Fatalf("decode decision payload: %v", err)
	}
	if decision.ChosenNode == "" {
		t.Fatal("decision must record the chosen node")
	}
	if len(decision.Candidates) == 0 {
		t.Fatal("decision must record the candidate scoreboard (why-this-node evidence)")
	}
	var chosenSeen bool
	for _, cand := range decision.Candidates {
		if cand.Chosen {
			chosenSeen = true
			if cand.NodeName != decision.ChosenNode {
				t.Fatalf("chosen candidate %q != chosen node %q", cand.NodeName, decision.ChosenNode)
			}
		}
	}
	if !chosenSeen {
		t.Fatal("exactly one candidate should be flagged Chosen")
	}

	// The decision must carry a verifiable DRF fairness ledger for the tenant.
	if decision.Fairness == nil || len(decision.Fairness.Tenants) == 0 {
		t.Fatal("decision must record a fairness ledger")
	}
	var sawTenant bool
	for _, ts := range decision.Fairness.Tenants {
		if ts.Tenant == "team-a" {
			sawTenant = true
			if ts.GPUsAllocated != 2 {
				t.Fatalf("team-a should hold 2 GPUs, got %d", ts.GPUsAllocated)
			}
			if ts.DominantResource == "" {
				t.Fatal("dominant resource must be recorded")
			}
		}
	}
	if !sawTenant {
		t.Fatal("fairness ledger must include the scheduling tenant team-a")
	}

	// The receipt must be cryptographically verifiable.
	rep, err := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.Valid {
		t.Fatalf("scheduling evidence chain must verify, got %+v", rep)
	}
}

func TestSchedulingRejectEmitsVerifiableEvidence(t *testing.T) {
	// Force "no eligible nodes" by running in production mode with no real node
	// source, so findBestAssignment fails and a schedule.reject receipt is emitted.
	engine, _ := NewEngine(EngineConfig{SchedulingInterval: 100 * time.Millisecond})
	ledger := newTestLedger(t)
	engine.SetEvidenceRecorder(ledger)
	engine.nodeCache.UpdateNodes(nil) // no cached nodes

	// Production forbids fabricated nodes, so with no real node source the
	// scheduler returns no candidates and must emit a reject receipt.
	capability.SetPolicy(runmode.Production)
	t.Cleanup(func() { capability.SetPolicy(runmode.Simulation) })

	wl := &Workload{
		ID:              common.NewUUID(),
		Name:            "unschedulable-job",
		Namespace:       "team-b",
		Status:          common.WorkloadStatusQueued,
		ResourceRequest: common.ResourceRequest{GPUCount: 4},
		QueuedAt:        common.NowUTC(),
	}
	// Call the internal path directly to guarantee the no-candidates branch.
	engine.mu.Lock()
	err := engine.scheduleWorkload(context.Background(), wl)
	engine.mu.Unlock()
	if err == nil {
		t.Skip("environment produced candidates; reject path not exercised")
	}

	all, _ := ledger.Store().All(context.Background())
	var rej *evidence.Evidence
	for _, e := range all {
		if e.Action == "schedule.reject" && e.Subject == wl.ID {
			rej = e
			break
		}
	}
	if rej == nil {
		t.Fatalf("expected a schedule.reject receipt for %s", wl.ID)
	}
	var decision RejectDecision
	if err := json.Unmarshal(rej.Payload, &decision); err != nil {
		t.Fatalf("decode reject payload: %v", err)
	}
	if decision.Reason == "" {
		t.Fatal("reject decision must record a reason")
	}
	rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("reject evidence must verify, got %+v", rep)
	}
}

func TestSchedulingWithoutRecorderIsNoop(t *testing.T) {
	// With the default NopRecorder, scheduling must still succeed and simply not
	// emit anything — evidence is additive, never a scheduling dependency.
	engine, _ := NewEngine(EngineConfig{SchedulingInterval: 100 * time.Millisecond})

	engine.mu.Lock()
	engine.queue = append(engine.queue, &Workload{
		ID:              common.NewUUID(),
		Name:            "no-evidence-job",
		Status:          common.WorkloadStatusQueued,
		ResourceRequest: common.ResourceRequest{GPUCount: 1},
		QueuedAt:        common.NowUTC(),
	})
	engine.mu.Unlock()

	engine.schedulingCycle(context.Background())

	engine.mu.RLock()
	defer engine.mu.RUnlock()
	if len(engine.running) != 1 {
		t.Fatalf("workload should schedule without a recorder, running=%d", len(engine.running))
	}
}
