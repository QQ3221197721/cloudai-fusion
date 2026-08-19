package redteam

// bmoat_redteam_bench_test.go - Performance Moat Benchmarks (Red Team Chain)
// Implements the user-required "optimize to surpass" strategy:
//   - Technique lookup latency (index vs linear scan) at 100 / 1000 techniques
//   - Attack chain orchestration overhead (Engine.Run on StaticPlanner)
//   - Evidence ledger record signing + storage latency
//   - Chain hash computation (incremental O(n) vs naive O(n^2))
//   - Large library query throughput (100 / 1000 techniques)
// All benchmarks use -benchtime=5x as per user requirement.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Technique Query Benchmarks (Inverted Index vs Linear Scan)
// ============================================================================

// generateTestTechniques produces a deterministic slice of test techniques.
func generateTestTechniques(count int) []Technique {
	techs := make([]Technique, count)
	for i := 0; i < count; i++ {
		tactic := "Initial Access"
		if i%10 == 0 {
			tactic = "Execution"
		} else if i%10 == 1 {
			tactic = "Persistence"
		} else if i%10 == 2 {
			tactic = "Privilege Escalation"
		} else if i%10 == 3 {
			tactic = "Defense Evasion"
		} else if i%10 == 4 {
			tactic = "Credential Access"
		} else if i%10 == 5 {
			tactic = "Discovery"
		} else if i%10 == 6 {
			tactic = "Lateral Movement"
		} else if i%10 == 7 {
			tactic = "Collection"
		} else if i%10 == 8 {
			tactic = "Command & Control"
		} else {
			tactic = "Impact"
		}

		dataSources := []string{"endpoint_detection", "network_traffic"}
		if i%5 == 0 {
			dataSources = append(dataSources, "email_gateway_logs")
		}

		techs[i] = Technique{
			ID:         fmt.Sprintf("T%04d", 1000+i),
			Name:       fmt.Sprintf("Attack Technique %d", i),
			Tactic:     tactic,
			DataSources: dataSources,
		}
	}
	return techs
}

// BenchmarkTechniqueIndex_ByID_100Tech measures O(1) lookup by TID at 100 techniques.
func BenchmarkTechniqueIndex_ByID_100Tech(b *testing.B) {
	techs := generateTestTechniques(100)
	idx := NewTechniqueIndex(techs)
	id := "T1059" // arbitrary existing ID

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.ByID(id)
	}
}

// BenchmarkTechniqueIndex_ByID_1000Tech measures O(1) lookup at 1000 techniques.
func BenchmarkTechniqueIndex_ByID_1000Tech(b *testing.B) {
	techs := generateTestTechniques(1000)
	idx := NewTechniqueIndex(techs)
	id := "T1059"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.ByID(id)
	}
}

// BenchmarkTechniqueIndex_ByTactic_100Tech measures O(k) retrieval at 100 techniques.
func BenchmarkTechniqueIndex_ByTactic_100Tech(b *testing.B) {
	techs := generateTestTechniques(100)
	idx := NewTechniqueIndex(techs)
	tactic := "Initial Access"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := idx.ByTactic(tactic)
		_ = len(matches)
	}
}

// BenchmarkTechniqueIndex_ByTactic_1000Tech measures O(k) retrieval at 1000 techniques.
func BenchmarkTechniqueIndex_ByTactic_1000Tech(b *testing.B) {
	techs := generateTestTechniques(1000)
	idx := NewTechniqueIndex(techs)
	tactic := "Initial Access"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := idx.ByTactic(tactic)
		_ = len(matches)
	}
}

// BenchmarkTechniqueLinearScan_ByID_100Tech is the naive O(N) baseline for 100 techniques.
func BenchmarkTechniqueLinearScan_ByID_100Tech(b *testing.B) {
	techs := generateTestTechniques(100)
	id := "T1059"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var found bool
		for _, t := range techs {
			if t.ID == id {
				found = true
				break
			}
		}
		_ = found
	}
}

// BenchmarkTechniqueLinearScan_ByID_1000Tech is the naive O(N) baseline for 1000 techniques.
func BenchmarkTechniqueLinearScan_ByID_1000Tech(b *testing.B) {
	techs := generateTestTechniques(1000)
	id := "T1059"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var found bool
		for _, t := range techs {
			if t.ID == id {
				found = true
				break
			}
		}
		_ = found
	}
}

// BenchmarkTechniqueLinearScan_ByTactic_100Tech is the naive O(N) baseline for tactic lookup at 100 techniques.
func BenchmarkTechniqueLinearScan_ByTactic_100Tech(b *testing.B) {
	techs := generateTestTechniques(100)
	tactic := "Initial Access"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = LinearScanByTactic(techs, tactic)
	}
}

// BenchmarkTechniqueLinearScan_ByTactic_1000Tech is the naive O(N) baseline for tactic lookup at 1000 techniques.
func BenchmarkTechniqueLinearScan_ByTactic_1000Tech(b *testing.B) {
	techs := generateTestTechniques(1000)
	tactic := "Initial Access"

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = LinearScanByTactic(techs, tactic)
	}
}

// ============================================================================
// Chain Orchestration Overhead (Engine.Run with StaticPlanner)
// ============================================================================

// BenchmarkEngineRun_SingleAction measures end-to-end engagement orchestration for 1 action.
func BenchmarkEngineRun_SingleAction(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	newLedger := func() *evidence.Ledger {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			return nil
		}
		l, err := evidence.NewLedger(evidence.LedgerConfig{
			Store:  evidence.NewMemoryStore(),
			Signer: signer,
		})
		if err != nil {
			return nil
		}
		return l
	}

	scope := Scope{
		Targets: []Target{{Kind: TargetHost, Value: "bench.local"}},
		MaxRiskTier: RiskReadOnly,
	}

	action := Action{
		ID:      "action-1",
		Technique: "T1190",
		Tool:    "bench",
		Target:  "bench.local",
		RiskTier: RiskReadOnly,
	}
	planner := StaticPlanner{Actions: []Action{action}}
	executor := DryRunExecutor{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ledger := newLedger()
		if ledger == nil {
			b.Fatal("failed to create ledger")
		}
		recorder := ledger
		mgr := NewManager(recorder, logger)
		e, err := mgr.Create(ctx, scope, "test")
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		engine := NewEngine(mgr, planner, executor, logger)
		_, err = engine.Run(ctx, e.ID)
		if err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkEngineRun_MultiAction measures orchestration for 10 actions.
func BenchmarkEngineRun_MultiAction(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	newLedger := func() *evidence.Ledger {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			return nil
		}
		l, err := evidence.NewLedger(evidence.LedgerConfig{
			Store:  evidence.NewMemoryStore(),
			Signer: signer,
		})
		if err != nil {
			return nil
		}
		return l
	}

	scope := Scope{
		Targets: []Target{{Kind: TargetHost, Value: "bench.local"}},
		MaxRiskTier: RiskReadOnly,
	}

	actions := make([]Action, 10)
	for i := range actions {
		actions[i] = Action{
			ID: fmt.Sprintf("action-%d", i),
			Technique: fmt.Sprintf("T%04d", 1190+i),
			Tool: "bench",
			Target: "bench.local",
			RiskTier: RiskReadOnly,
		}
	}
	planner := StaticPlanner{Actions: actions}
	executor := DryRunExecutor{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ledger := newLedger()
		if ledger == nil {
			b.Fatal("failed to create ledger")
		}
		recorder := ledger
		mgr := NewManager(recorder, logger)
		e, err := mgr.Create(ctx, scope, "test")
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		engine := NewEngine(mgr, planner, executor, logger)
		_, err = engine.Run(ctx, e.ID)
		if err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// ============================================================================
// Evidence Ledger Recording & Signing
// ============================================================================

// BenchmarkEvidenceRecord_CreateAndSign measures creating+signing a single evidence receipt.
func BenchmarkEvidenceRecord_CreateAndSign(b *testing.B) {
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}
	store := evidence.NewMemoryStore()
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		b.Fatalf("new_ledger: %v", err)
	}

	input := evidence.RecordInput{
		Actor: "redteam",
		Action: "redteam.action.authorized",
		Subject: "scope-test",
		Input: map[string]any{"engagement_id": "eng-1", "technique": "T1190"},
		Output: map[string]any{"allowed": true},
		Payload: map[string]any{"engagement_id": "eng-1", "action_id": "act-1"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ledger.Record(context.Background(), input)
		if err != nil {
			b.Fatalf("record: %v", err)
		}
	}
}

// BenchmarkEvidenceVerifyChain measures full-chain verification after N records.
func BenchmarkEvidenceVerifyChain(b *testing.B) {
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("keygen: %v", err)
	}
	store := evidence.NewMemoryStore()
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		b.Fatalf("new_ledger: %v", err)
	}

	// Pre-populate N records in each iteration
	numRecords := 50

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Reset() // clear
		for j := 0; j < numRecords; j++ {
			input := evidence.RecordInput{
				Actor: "redteam",
				Action: "redteam.scope.grant",
				Subject: fmt.Sprintf("engagement-%d", j),
				Input: map[string]any{}, Output: map[string]any{},
				Payload: map[string]any{"idx": j},
			}
			_, err := ledger.Record(context.Background(), input)
			if err != nil {
				b.Fatalf("record: %v", err)
			}
		}
		all, _ := ledger.Store().All(context.Background())
		_, _ = evidence.VerifyChain(all, ledger.Signer().PublicKey())
	}
}

// ============================================================================
// Incremental Chain Hash vs Naive Recompute
// ============================================================================

// buildRandomRecords generates JSON record bytes of roughly equal size.
func buildRandomRecords(count int, seed int64) [][]byte {
	records := make([][]byte, count)
	for i := 0; i < count; i++ {
		obj := map[string]any{"id": i, "seed": seed, "data": fmt.Sprintf("record-%d", i)}
		raw, _ := json.Marshal(obj)
		records[i] = raw
	}
	return records
}

// BenchmarkIncrementalChainHash_Append_N measures O(len(record)) per-append.
func BenchmarkIncrementalChainHash_Append_100Records(b *testing.B) {
	records := buildRandomRecords(100, 12345)
	h := NewIncrementalChainHasher()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Reset()
		for _, raw := range records {
			h.Append(raw)
		}
		_ = h.Digest()
	}
}

// BenchmarkIncrementalChainHash_Append_1000Records measures scaling at 1000 records.
func BenchmarkIncrementalChainHash_Append_1000Records(b *testing.B) {
	records := buildRandomRecords(1000, 12345)
	h := NewIncrementalChainHasher()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Reset()
		for _, raw := range records {
			h.Append(raw)
		}
		_ = h.Digest()
	}
}

// BenchmarkNaiveRecompute_Chain_100Records is the O(n^2) naive baseline.
func BenchmarkNaiveRecompute_Chain_100Records(b *testing.B) {
	records := buildRandomRecords(100, 12345)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := BuildChain(records)
		_ = hash
	}
}

// BenchmarkNaiveRecompute_Chain_1000Records is the O(n^2) naive baseline at 1000 records.
func BenchmarkNaiveRecompute_Chain_1000Records(b *testing.B) {
	records := buildRandomRecords(1000, 12345)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := BuildChain(records)
		_ = hash
	}
}
