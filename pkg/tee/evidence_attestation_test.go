package tee

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func newTestTEEEngine(t testing.TB, sgxURL, sgxKey string) *EvidenceAttestationEngine {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return NewEvidenceAttestationEngine(priv, sgxURL, sgxKey)
}

func TestTEEEngine_FallbackToSimulation(t *testing.T) {
	e := newTestTEEEngine(t, "", "") // no SGX → immediate simulation fallback
	result, err := e.Attestate(context.Background())
	if err != nil {
		t.Fatalf("Attestate: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success via simulation")
	}
	foundSim := false
	for _, a := range result.Attempts {
		if a.Provider == ProviderSim && a.Succeeded {
			foundSim = true
		}
	}
	if !foundSim {
		t.Fatalf("expected software simulation attempt to succeed")
	}
	if result.Receipt == nil || !result.Receipt.Verify() {
		t.Fatalf("receipt missing or invalid signature")
	}
}

func TestTEEEngine_RecordsAllProviders(t *testing.T) {
	e := newTestTEEEngine(t, "", "")
	result, _ := e.Attestate(context.Background())
	if len(result.Attempts) < 3 {
		t.Fatalf("expected at least 3 providers attempted, got %d", len(result.Attempts))
	}
	// Ensure we logged SEV and TZ attempts as failures.
	hasSEVFail := false
	hasTZFail := false
	for _, a := range result.Attempts {
		if a.Provider == ProviderSEV && !a.Succeeded {
			hasSEVFail = true
		}
		if a.Provider == ProviderTZ && !a.Succeeded {
			hasTZFail = true
		}
	}
	if !hasSEVFail || !hasTZFail {
		t.Fatalf("expected failed attempts recorded for SEV/TZ")
	}
}

func TestTEEEngine_WithSGXClientConfigured(t *testing.T) {
	// Configure with fake credentials; they'll fail but be tried first.
	e := newTestTEEEngine(t, "http://fake-sgx-url", "fake-key")
	result, err := e.Attestate(context.Background())
	if err != nil {
		t.Fatalf("Attestate: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected fallback success even if SGX fails")
	}
	// SGX should be first attempt.
	first := result.Attempts[0]
	if first.Provider != ProviderSGX {
		t.Fatalf("expected SGX first provider, got %s", first.Provider)
	}
}

func TestTEEEngine_TrustScoresRecorded(t *testing.T) {
	e := newTestTEEEngine(t, "", "")
	result, _ := e.Attestate(context.Background())
	trustByProvider := make(map[ProviderKind]float64)
	for _, a := range result.Attempts {
		trustByProvider[a.Provider] = a.TrustScore
	}
	t.Logf("Trust scores: SGX=%.3f SEV=%.3f TZ=%.3f SIM=%.3f",
		trustByProvider[ProviderSGX], trustByProvider[ProviderSEV],
		trustByProvider[ProviderTZ], trustByProvider[ProviderSim])
	if trustByProvider[ProviderSGX] <= trustByProvider[ProviderSim] {
		t.Fatalf("SGX trust score (%.3f) should exceed SIM score (%.3f)", trustByProvider[ProviderSGX], trustByProvider[ProviderSim])
	}
	if !(trustByProvider[ProviderTZ] > trustByProvider[ProviderSim]) {
		t.Fatalf("TrustZone trust score (%.3f) should be higher than SIM (%.3f)", trustByProvider[ProviderTZ], trustByProvider[ProviderSim])
	}
}

func TestTEEEngine_ListProviders(t *testing.T) {
	e := newTestTEEEngine(t, "http://url", "key")
	providers := e.ListProviders()
	if len(providers) == 0 {
		t.Fatalf("expected at least one provider listed")
	}
}

func BenchmarkTEEEngine_Attestate(b *testing.B) {
	e := newTestTEEEngine(b, "", "")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.Attestate(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
