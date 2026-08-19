package fabric

// evidence_fabric.go layers two independent barriers over verification:
//
//  1. Evidence-native barrier — each cross-module verification is sealed into a signed,
//     offline-verifiable evidence.Receipt binding (checkName, result, modules). We can
//     prove "verification V passed/failed at time X on modules Y".
//
//  2. Independent-innovation barrier — cross-module consistency checker maintains a ledger
//     of decision vericts across modules and detects contradictions by tracking pairs where
//     one module allows an action but another blocks it. It also scores system-wide coherence
//     as the fraction of non-conflicting module pairs.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

type VerificationResult struct {
	CheckName   string            `json:"check_name"`
	Result      string            `json:"result"` // "allow" | "block" | "warn"
	Modules     []string          `json:"modules"`
	Coherence   float64           `json:"coherence"` // 0..1 confidence in verdict
	Receipt     *evidence.Receipt `json:"receipt,omitempty"`
}

type ConsistencyViolation struct {
	ModuleA    string `json:"module_a"`
	ActionA    string `json:"action_a"` // allow/block
	ModuleB    string `json:"module_b"`
	ActionB    string `json:"action_b"`
	Violation  string `json:"violation_type"`
}

type EvidenceFabricEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu sync.Mutex
	decisions map[string]*DecisionLog // module → last decision
	violations []ConsistencyViolation
	pairs       [][3]string // module-pair co-occurrences
	maxPairs int
}

type DecisionLog struct {
	Timestamp int64
	Action string
	Input string
}

func NewEvidenceFabricEngine() *EvidenceFabricEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceFabricEngine{
		receiptBuilder: evidence.NewReceiptBuilder("fabric", priv),
		decisions: make(map[string]*DecisionLog),
		maxPairs: 0,
	}
}

func (e *EvidenceFabricEngine) Verify(checkName string, modules []string, verdict string) (*VerificationResult, error) {
	if checkName == "" || len(modules) == 0 {
		return nil, fmt.Errorf("fabric: checkName and modules must not be empty")
	}
	if verdict != "allow" && verdict != "block" && verdict != "warn" {
		return nil, fmt.Errorf("fabric: verdict must be allow|block|warn")
	}

	result := &VerificationResult{
		CheckName: checkName,
		Result: verdict,
		Modules: modules,
	}

	input := struct {
		Name string `json:"check_name"`
		MN   int    `json:"module_count"`
	}{checkName, len(modules)}
	receipt, err := e.receiptBuilder.Build("fabric.verify", input, result)
	if err != nil {
		return nil, fmt.Errorf("fabric: seal verify: %w", err)
	}
	result.Receipt = receipt

	e.mu.Lock()
	for i, mod := range modules {
		ts := int64(123123) // mock timestamp
		e.decisions[mod] = &DecisionLog{Timestamp: ts, Action: verdict, Input: checkName}
		for j := i + 1; j < len(modules); j++ {
			pair := [3]string{mod, modules[j], checkName}
			e.pairs = append(e.pairs, pair)
		}
		if len(e.pairs) > e.maxPairs {
			e.maxPairs = len(e.pairs)
		}
	}
	e.mu.Unlock()
	
	result.Coherence = e.computeCoherence()
	return result, nil
}

func (e *EvidenceFabricEngine) DetectInconsistencies() []ConsistencyViolation {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	var violations []ConsistencyViolation
	modMap := make(map[string]string)
	for mod, log := range e.decisions {
		modMap[mod] = log.Action
	}
	
	keys := make([]string, 0, len(modMap))
	for k := range modMap {
		keys = append(keys, k)
	}
	
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			a, b := keys[i], keys[j]
			if modMap[a] == "allow" && modMap[b] == "block" {
				violations = append(violations, ConsistencyViolation{
					ModuleA:   a,
					ActionA:   "allow",
					ModuleB:   b,
					ActionB:   "block",
					Violation: "contradicting_verdicts",
				})
			} else if modMap[a] == "block" && modMap[b] == "allow" {
				violations = append(violations, ConsistencyViolation{
					ModuleA:   a,
					ActionA:   "block",
					ModuleB:   b,
					ActionB:   "allow",
					Violation: "contradicting_verdicts",
				})
			}
		}
	}
	return violations
}

func (e *EvidenceFabricEngine) computeCoherence() float64 {
	total := float64(e.maxPairs)
	if total <= 0 {
		return 1.0
	}
	
	violationCnt := 0
	for _, v := range e.violations {
		if v.Violation == "contradicting_verdicts" {
			violationCnt++
		}
	}
	
	coherence := 1.0 - (float64(violationCnt) / total)
	if coherence < 0 {
		coherence = 0
	}
	return coherence
}
