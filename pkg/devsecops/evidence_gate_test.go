package devsecops

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func newTestGate(t *testing.T, maxCritical int) *EvidenceSecurityGate {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceSecurityGate(priv, maxCritical)
}

func TestEvaluateGate_BlocksOnCritical(t *testing.T) {
	gate := newTestGate(t, 0) // zero critical tolerated

	findings := []EvidenceFinding{
		{ID: "F1", Severity: "critical", Category: "cve", Package: "lodash", Version: "4.17.10"},
		{ID: "F2", Severity: "high", Category: "cve", Package: "axios", Version: "0.21.0"},
	}
	res, err := gate.EvaluateGate("artifact-1", findings)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Passed {
		t.Errorf("gate should block on critical finding")
	}
	if res.Blocked != 1 {
		t.Errorf("expected Blocked=1, got %d", res.Blocked)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Errorf("gate receipt must verify")
	}
}

func TestEvaluateGate_PassesWithinThreshold(t *testing.T) {
	gate := newTestGate(t, 2)
	findings := []EvidenceFinding{
		{ID: "F1", Severity: "critical", Category: "cve", Package: "lodash"},
		{ID: "F2", Severity: "medium", Category: "misconfig"},
	}
	res, err := gate.EvaluateGate("artifact-2", findings)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !res.Passed {
		t.Errorf("gate should pass: 1 critical within maxCritical=2")
	}
	if res.Blocked != 0 {
		t.Errorf("expected Blocked=0, got %d", res.Blocked)
	}
}

func TestSuggestFixes_MatchesKnownCVEPatterns(t *testing.T) {
	eng := NewAutoRemediationEngine()
	findings := []EvidenceFinding{
		{ID: "F1", Severity: "critical", Category: "cve", Package: "lodash"},
		{ID: "F2", Severity: "high", Category: "cve", Package: "log4j-core"},
		{ID: "F3", Severity: "high", Category: "cve", Package: "unknown-pkg-xyz"},
		{ID: "F4", Severity: "medium", Category: "secret"},
		{ID: "F5", Severity: "low", Category: "misconfig"},
	}
	sugs := eng.SuggestFixes(findings)
	if len(sugs) != len(findings) {
		t.Fatalf("expected %d suggestions, got %d", len(findings), len(sugs))
	}

	byID := map[string]RemediationSuggestion{}
	for _, s := range sugs {
		byID[s.FindingID] = s
	}

	if s := byID["F1"]; s.Action != "upgrade" || s.FixVersion != "4.17.22" || !s.AutoFixable {
		t.Errorf("lodash: expected upgrade→4.17.22 auto-fixable, got %+v", s)
	}
	if s := byID["F2"]; s.Action != "upgrade" || s.FixVersion != "2.17.1" || !s.AutoFixable {
		t.Errorf("log4j-core: expected upgrade→2.17.1, got %+v", s)
	}
	if s := byID["F3"]; s.Action != "upgrade" || s.AutoFixable {
		t.Errorf("unknown package: expected non-auto-fixable upgrade, got %+v", s)
	}
	if s := byID["F4"]; s.Action != "remove" {
		t.Errorf("secret: expected remove, got %+v", s)
	}
	if s := byID["F5"]; s.Action != "configure" {
		t.Errorf("misconfig: expected configure, got %+v", s)
	}
}

func BenchmarkEvaluateGate(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	gate := NewEvidenceSecurityGate(priv, 0)
	findings := []EvidenceFinding{
		{ID: "F1", Severity: "critical", Category: "cve", Package: "lodash"},
		{ID: "F2", Severity: "high", Category: "cve", Package: "log4j"},
		{ID: "F3", Severity: "medium", Category: "misconfig"},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = gate.EvaluateGate("artifact", findings)
	}
}
