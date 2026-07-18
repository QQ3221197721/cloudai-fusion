package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestCloud_ProviderCapabilityAndPricingProvenance proves the multi-cloud layer
// is now honest: a provider with credentials reports real, one without reports
// stub, and price lookups carry a truthful provenance tag plus a verifiable
// receipt (feeding the FinOps moat).
func TestCloud_ProviderCapabilityAndPricingProvenance(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x4d}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})

	m, err := NewManager(ManagerConfig{Providers: []config.CloudProviderConfig{
		{Name: "aliyun-real", Type: "aliyun", Region: "cn-hangzhou", AccessKeyID: "AKID", AccessKeySecret: "SECRET"},
		{Name: "aliyun-stub", Type: "aliyun", Region: "cn-hangzhou"},
	}})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	m.SetEvidenceRecorder(ledger)

	// Capability must honestly reflect real vs stub per provider.
	modes := map[string]capability.Mode{}
	for _, b := range capability.Snapshot() {
		if strings.HasPrefix(b.Component, "cloud.provider.") {
			modes[b.Component] = b.Mode
		}
	}
	if modes["cloud.provider.aliyun-real"] != capability.ModeReal {
		t.Fatalf("provider with credentials must report real, got %v", modes["cloud.provider.aliyun-real"])
	}
	if modes["cloud.provider.aliyun-stub"] != capability.ModeSimulated {
		t.Fatalf("provider without credentials must report simulated, got %v", modes["cloud.provider.aliyun-stub"])
	}

	// Pricing provenance: honestly static-table (not a live billing API).
	_, prov, err := m.GetGPUPricingWithProvenance(context.Background(), "aliyun-real", "A100")
	if err != nil {
		t.Fatalf("pricing: %v", err)
	}
	if prov.Real || prov.Source != "static-table" {
		t.Fatalf("price provenance must be honestly static-table/not-real, got %+v", prov)
	}

	all, _ := ledger.Store().All(context.Background())
	var rec *evidence.Evidence
	for _, e := range all {
		if e.Action == "cloud.pricing" && e.Subject == "aliyun-real/A100" {
			rec = e
		}
	}
	if rec == nil {
		t.Fatal("expected a cloud.pricing provenance receipt")
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["provider_real"] != true {
		t.Fatalf("provider_real must be true for the credentialed provider, got %v", payload["provider_real"])
	}
	var sawStatic bool
	for _, b := range rec.Backends {
		if b.Component == "cloud.pricing" && b.Mode == "simulated" && b.Driver == "static-table" {
			sawStatic = true
		}
	}
	if !sawStatic {
		t.Fatalf("pricing receipt must honestly record static-table source, got %+v", rec.Backends)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("pricing evidence chain must verify: %+v", rep)
	}
}

// TestCloud_ProviderIsReal is a focused check on the credential detection.
func TestCloud_ProviderIsReal(t *testing.T) {
	real, err := NewAliyunProvider(config.CloudProviderConfig{
		Name: "r", Type: "aliyun", Region: "cn-hangzhou", AccessKeyID: "AKID", AccessKeySecret: "SECRET",
	})
	if err != nil {
		t.Fatalf("real provider: %v", err)
	}
	stub, err := NewAliyunProvider(config.CloudProviderConfig{Name: "s", Type: "aliyun", Region: "cn-hangzhou"})
	if err != nil {
		t.Fatalf("stub provider: %v", err)
	}
	if !ProviderIsReal(real) {
		t.Fatal("provider with credentials must be real")
	}
	if ProviderIsReal(stub) {
		t.Fatal("provider without credentials must be stub")
	}
}
