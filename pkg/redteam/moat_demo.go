package redteam

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// moat_demo.go produces the platform's flagship, reproducible proof: a REAL
// red-team engagement — created under a signed scope, every action routed through
// the authorization gate, tool executions and findings recorded as signed,
// hash-chained receipts, then the engagement sealed/completed — exported as a
// portable bundle that a THIRD PARTY can verify OFFLINE against a pinned public
// key, with zero access to the running platform. Tampering with any receipt
// breaks verification.
//
// This is the concrete M5 evidence for L13 (Verifiable Control Plane): not a
// hand-written record set, but the actual engagement lifecycle, verifiable by the
// same core that backs `cafctl verify`.

// VerifiableEngagementDemo is the self-contained artifact set an auditor receives.
type VerifiableEngagementDemo struct {
	EngagementID string `json:"engagement_id"`
	ReceiptCount int    `json:"receipt_count"`
	FindingCount int    `json:"finding_count"`
	// BundleJSON is the exported evidence chain (GET /api/v1/evidence/export shape).
	BundleJSON []byte `json:"-"`
	// PublicKeyPEM is the pinned verifying key an auditor checks the chain against.
	PublicKeyPEM []byte `json:"-"`
}

// demoSeed returns a deterministic 32-byte Ed25519 seed so the demo is
// reproducible (same signing identity every run).
func demoSeed() []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = 0x5c
	}
	return s
}

// RunVerifiableEngagementDemo runs a real, deterministic engagement to completion
// on a fresh in-memory ledger and returns the exported chain plus the pinned
// public key. The engagement uses the built-in BenchTool so it needs no external
// tool, network, or cluster and is safe to run in CI.
func RunVerifiableEngagementDemo(ctx context.Context) (*VerifiableEngagementDemo, error) {
	signer, err := evidence.NewSignerFromSeed(demoSeed())
	if err != nil {
		return nil, fmt.Errorf("redteam demo: signer: %w", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:  evidence.NewMemoryStore(),
		Signer: signer,
	})
	if err != nil {
		return nil, fmt.Errorf("redteam demo: ledger: %w", err)
	}

	reg := NewToolRegistry()
	reg.Register(NewBenchTool("bench", "T1190", "T1071"))

	mgr := NewManager(ledger, nil)
	e, err := mgr.Create(ctx, benchScope(), "moat-demo")
	if err != nil {
		return nil, fmt.Errorf("redteam demo: create engagement: %w", err)
	}

	exec := NewToolExecutor(e.ID, reg, ledger, nil)
	actions := []Action{
		{ID: e.ID + "-a1", Technique: "T1190", Tool: "bench", Target: "bench.local", RiskTier: RiskReadOnly},
		{ID: e.ID + "-a2", Technique: "T1071", Tool: "bench", Target: "bench.local", RiskTier: RiskReadOnly},
	}
	res, err := NewEngine(mgr, StaticPlanner{Actions: actions}, exec, nil).Run(ctx, e.ID)
	if err != nil {
		return nil, fmt.Errorf("redteam demo: run engagement: %w", err)
	}

	all, err := ledger.Store().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("redteam demo: read chain: %w", err)
	}
	bundle, err := ledger.Export(ctx)
	if err != nil {
		return nil, fmt.Errorf("redteam demo: export chain: %w", err)
	}
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		return nil, fmt.Errorf("redteam demo: marshal bundle: %w", err)
	}
	pubPEM, err := evidence.MarshalPublicKeyPEM(signer.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("redteam demo: marshal pubkey: %w", err)
	}

	return &VerifiableEngagementDemo{
		EngagementID: e.ID,
		ReceiptCount: len(all),
		FindingCount: res.Findings,
		BundleJSON:   bundleJSON,
		PublicKeyPEM: pubPEM,
	}, nil
}
