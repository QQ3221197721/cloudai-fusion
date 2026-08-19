package validation

import "testing"

// TestValidate_ProducesVerifiableReceipt proves each validation verdict is sealed
// into a signed, offline-verifiable receipt with accurate pass/fail outcomes.
func TestValidate_ProducesVerifiableReceipt(t *testing.T) {
	engine := NewEvidenceValidationEngine()

	passed, err := engine.Validate("hello")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if passed.Receipt == nil || !passed.Receipt.Verify() {
		t.Fatal("validation must produce a verifiable receipt")
	}
	if !passed.Passed {
		t.Fatalf("short input should pass: %+v", passed)
	}

	failed, err := engine.Validate(makeLongString(1024))
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if failed.Receipt == nil || !failed.Receipt.Verify() {
		t.Fatal("validation must produce a verifiable receipt")
	}
	if failed.Passed {
		t.Fatalf("long input should fail: %+v", failed)
	}
}

// TestSynthesizeRules_AutoGeneratesDenyRules verifies that when crashes are
// dominated by null bytes or long inputs, SynthesizeRules activates deny rules
// and subsequent Validate calls reject those patterns.
func TestSynthesizeRules_AutoGeneratesDenyRules(t *testing.T) {
	engine := NewEvidenceValidationEngine()

	// Build a crash corpus where 75% have null bytes, 60% are very long.
	for i := 0; i < 80; i++ {
		engine.RecordCrash("bad\x00payload") // has_null_byte → 80% coverage
	}
	for i := 0; i < 70; i++ {
		engine.RecordCrash(makeLongString(2048)) // len>=1024 → ~70% coverage
	}

	rules := engine.SynthesizeRules(0.5)
	if len(rules) == 0 {
		t.Fatal("expected synthesis to activate at least one rule")
	}

	// Now validate: null-byte payloads should be rejected.
	outcome, err := engine.Validate("test\x00bad")
	if err != nil {
		t.Fatalf("validate after synthesize: %v", err)
	}
	if outcome.Passed {
		t.Fatalf("should reject null bytes after rule synthesis: %+v", outcome)
	}
}

// TestSynthesizeRules_NoRuleWithoutDominantSig ensures no rules when no sig
// dominates the crash corpus.
func TestSynthesizeRules_NoRuleWithoutDominantSig(t *testing.T) {
	engine := NewEvidenceValidationEngine()

	// Spread crashes across three unrelated signatures evenly.
	for i := 0; i < 30; i++ {
		engine.RecordCrash("abc\x01def") // has_control_chars (0x01)
		engine.RecordCrash(makeLongString(513)) // len>=256 but not >=1024
		engine.RecordCrash("high" + makeHighBytesString(10))
	}
	// Each sig covers ~33%, below the 50% threshold.
	rules := engine.SynthesizeRules(0.5)
	if len(rules) != 0 {
		t.Fatalf("expected no rules, got %d: %+v", len(rules), rules)
	}
}

func makeLongString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func makeHighBytesString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 0x90
	}
	return string(b)
}
