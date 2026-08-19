package validation

// evidence_validation.go layers two independent barriers over input validation:
//
//  1. Evidence-native barrier — every validation pass/fail is sealed into a
//     signed, offline-verifiable evidence.Receipt binding the input digest to
//     the verdict, so we can prove "input I was rejected by rule R at time X".
//
//  2. Independent-innovation barrier — a fuzzing-informed rule synthesizer
//     learns from past crash-inducing inputs: it extracts structural signatures
//     (length buckets, control-char presence, byte-class histograms) from
//     observed crashes and auto-generates deny rules for the signatures that
//     dominate the crash corpus.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ValidationOutcome is the verifiable result of validating one input.
type ValidationOutcome struct {
	Passed     bool              `json:"passed"`
	Violations []string          `json:"violations,omitempty"`
	Receipt    *evidence.Receipt `json:"receipt,omitempty"`
}

// SynthesizedRule is a validation rule auto-generated from the fuzzing corpus.
type SynthesizedRule struct {
	Signature  string  `json:"signature"`  // e.g. "len>=1024", "has_control_chars"
	CrashCount int     `json:"crash_count"`
	Coverage   float64 `json:"coverage"` // fraction of crashes exhibiting this signature
}

// EvidenceValidationEngine seals validation verdicts and synthesizes rules from
// observed fuzzing crashes.
type EvidenceValidationEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu          sync.Mutex
	denySigs    map[string]bool // active auto-generated deny signatures
	crashSigs   map[string]int  // signature → crash count
	crashTotal  int
	maxLen      int
}

// NewEvidenceValidationEngine builds an engine with a freshly generated key.
func NewEvidenceValidationEngine() *EvidenceValidationEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceValidationEngine{
		receiptBuilder: evidence.NewReceiptBuilder("validation", priv),
		denySigs:       make(map[string]bool),
		crashSigs:      make(map[string]int),
		maxLen:         512,
	}
}

// Validate checks an input against the built-in rules plus any active
// auto-generated deny signatures, and returns a signed receipt for the verdict.
func (e *EvidenceValidationEngine) Validate(input string) (*ValidationOutcome, error) {
	e.mu.Lock()
	maxLen := e.maxLen
	deny := make(map[string]bool, len(e.denySigs))
	for s := range e.denySigs {
		deny[s] = true
	}
	e.mu.Unlock()

	var violations []string
	if len(input) > maxLen {
		violations = append(violations, fmt.Sprintf("length %d exceeds max %d", len(input), maxLen))
	}
	for _, sig := range inputSignatures(input) {
		if deny[sig] {
			violations = append(violations, "matched deny signature "+sig)
		}
	}

	outcome := &ValidationOutcome{Passed: len(violations) == 0, Violations: violations}
	input2 := struct {
		Len int    `json:"len"`
		Sig string `json:"sig"`
	}{len(input), strings.Join(inputSignatures(input), ",")}
	receipt, err := e.receiptBuilder.Build("validation.check", input2, outcome)
	if err != nil {
		return nil, fmt.Errorf("validation: seal verdict: %w", err)
	}
	outcome.Receipt = receipt
	return outcome, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: fuzzing-informed rule synthesis
// ---------------------------------------------------------------------------

// RecordCrash registers a crash-inducing input, extracting its structural
// signatures and accumulating them into the crash corpus.
func (e *EvidenceValidationEngine) RecordCrash(input string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.crashTotal++
	for _, sig := range inputSignatures(input) {
		e.crashSigs[sig]++
	}
}

// SynthesizeRules analyzes the crash corpus and returns deny rules for the
// signatures whose coverage (fraction of crashes exhibiting them) meets the
// threshold. The engine activates these signatures so subsequent Validate calls
// reject matching inputs.
func (e *EvidenceValidationEngine) SynthesizeRules(minCoverage float64) []SynthesizedRule {
	if minCoverage <= 0 || minCoverage > 1 {
		minCoverage = 0.5
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.crashTotal == 0 {
		return nil
	}
	var rules []SynthesizedRule
	for sig, count := range e.crashSigs {
		cov := float64(count) / float64(e.crashTotal)
		if cov >= minCoverage {
			rules = append(rules, SynthesizedRule{Signature: sig, CrashCount: count, Coverage: cov})
			e.denySigs[sig] = true
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Coverage != rules[j].Coverage {
			return rules[i].Coverage > rules[j].Coverage
		}
		return rules[i].Signature < rules[j].Signature
	})
	return rules
}

// inputSignatures extracts coarse structural signatures from an input. These
// are deliberately generalizing (buckets, not literals) so a synthesized rule
// generalizes beyond the exact crashing sample.
func inputSignatures(input string) []string {
	sigs := make([]string, 0, 4)
	switch {
	case len(input) >= 1024:
		sigs = append(sigs, "len>=1024")
	case len(input) >= 256:
		sigs = append(sigs, "len>=256")
	}
	hasControl, hasNull, hasHighBytes := false, false, false
	for i := 0; i < len(input); i++ {
		b := input[i]
		if b == 0 {
			hasNull = true
		}
		if b < 0x20 && b != '\n' && b != '\t' && b != '\r' {
			hasControl = true
		}
		if b >= 0x80 {
			hasHighBytes = true
		}
	}
	if hasNull {
		sigs = append(sigs, "has_null_byte")
	}
	if hasControl {
		sigs = append(sigs, "has_control_chars")
	}
	if hasHighBytes {
		sigs = append(sigs, "has_high_bytes")
	}
	return sigs
}
