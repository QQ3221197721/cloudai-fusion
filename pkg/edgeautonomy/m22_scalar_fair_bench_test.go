package edgeautonomy

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hyperjumptech/grule-rule-engine/ast"
	"github.com/hyperjumptech/grule-rule-engine/builder"
	"github.com/hyperjumptech/grule-rule-engine/engine"
	grulepkg "github.com/hyperjumptech/grule-rule-engine/pkg"
)

// ============================================================================
// M22 FAIR HEAD-TO-HEAD: EdgeAutonomy native compiled rules vs REAL grule
// ============================================================================
//
// GOAL: A genuine, apples-to-apples T2 comparison where REAL grule actually
// parses and executes rules — no fallback, no fakery.
//
// ROOT-CAUSE OF PRIOR FAILURE (now fixed):
//   GRULE rule names must be SIMPLENAME tokens (grulev3.g4 line 16-18, 250:
//   `SIMPLENAME : ISC IC*` where IC excludes '-'). Names like "scalar-rule-0"
//   were lexed as subtraction ⇒ "got N error(s) in grl the script".
//   Fixed by using underscore names: "Rule_0".
//
// DESIGN FOR PROVABLE CORRECTNESS:
//   We evaluate a FLAT scalar fact (no arrays/nesting — GRL-parseable).
//   Rules partition CPUUtil into N DISJOINT bands, so EXACTLY ONE rule fires
//   for any fact ⇒ both engines MUST produce the identical Action string.
//   A shared []scalarRuleSpec generates BOTH engines' rule sets, guaranteeing
//   semantic equivalence by construction.
//
// FAIRNESS:
//   grule's cost = reflection-based fact binding + GRL AST interpretation per
//   eval. Our engine = compiled Go predicates over the same fact and the same
//   scalar rule set. Same inputs, same outputs, head-to-head latency.

// ScalarFact is a pure flat scalar fact both engines evaluate identically.
type ScalarFact struct {
	CPUUtil      float64 // 0-100 %
	MemUtil      float64 // 0-100 %
	GPUCache     float64 // 0-100 %
	NetLatencyMs float64 // milliseconds
	QueueDepth   float64 // pending requests
	TempCelsius  float64 // node temperature
	Action       string  // decision output written by the matching rule
}

// NewScalarFact returns a representative fact. CPUUtil=75.5 lands in exactly
// one band for any N ⇒ deterministic single match.
func NewScalarFact() *ScalarFact {
	return &ScalarFact{
		CPUUtil:      75.5,
		MemUtil:      68.2,
		GPUCache:     82.3,
		NetLatencyMs: 45.0,
		QueueDepth:   12,
		TempCelsius:  65.0,
	}
}

// scalarRuleSpec is the SINGLE SOURCE OF TRUTH for a rule. Both the native
// engine and the grule GRL text are generated from the same specs, so the two
// rule sets are equivalent by construction.
type scalarRuleSpec struct {
	name     string
	salience int
	lowCPU   float64 // matches when lowCPU <= CPUUtil < highCPU
	highCPU  float64
	action   string
}

// makeScalarRuleSpecs builds N disjoint-band rules partitioning CPUUtil 0..100.
// Disjoint bands guarantee at most one rule matches any fact.
func makeScalarRuleSpecs(n int) []scalarRuleSpec {
	specs := make([]scalarRuleSpec, 0, n)
	step := 100.0 / float64(n)
	actions := []string{"SCALE_UP", "SCALE_DOWN", "RESTART", "MIGRATE", "EVICT"}
	for i := 0; i < n; i++ {
		specs = append(specs, scalarRuleSpec{
			name:     fmt.Sprintf("Rule_%d", i), // SIMPLENAME-safe (no hyphens)
			salience: n - i,
			lowCPU:   float64(i) * step,
			highCPU:  float64(i+1) * step,
			action:   actions[i%len(actions)],
		})
	}
	return specs
}

// ============================================================================
// EdgeAutonomy native compiled rule engine
// ============================================================================

type nativeRule struct {
	lowCPU  float64
	highCPU float64
	action  string
}

type nativeScalarEngine struct {
	rules []nativeRule
}

func newNativeScalarEngine(specs []scalarRuleSpec) *nativeScalarEngine {
	rules := make([]nativeRule, len(specs))
	for i, s := range specs {
		rules[i] = nativeRule{lowCPU: s.lowCPU, highCPU: s.highCPU, action: s.action}
	}
	return &nativeScalarEngine{rules: rules}
}

// Evaluate applies every rule (checks all N conditions, matching grule's cycle)
// and returns the Action of the single matching band.
func (e *nativeScalarEngine) Evaluate(fact *ScalarFact) string {
	action := ""
	for i := range e.rules {
		r := &e.rules[i]
		if fact.CPUUtil >= r.lowCPU && fact.CPUUtil < r.highCPU {
			action = r.action
		}
	}
	fact.Action = action
	return action
}

// ============================================================================
// REAL grule-rule-engine wrapper (github.com/hyperjumptech/grule-rule-engine)
// ============================================================================

const (
	gruleKBName = "m22-scalar-fair"
	gruleKBVer  = "1.0.0"
)

type gruleScalarEngine struct {
	engine *engine.GruleEngine
	lib    *ast.KnowledgeLibrary
}

// newGruleScalarEngine builds REAL grule rules from the shared specs. Returns
// an error if grule fails to parse — we NEVER silently fall back.
func newGruleScalarEngine(specs []scalarRuleSpec) (*gruleScalarEngine, error) {
	lib := ast.NewKnowledgeLibrary()
	rb := builder.NewRuleBuilder(lib)

	var sb strings.Builder
	for _, s := range specs {
		// Guard `Fact.Action == ""` makes each rule fire at most once so the
		// grule inference cycle terminates in one pass (disjoint bands ⇒ single
		// match anyway). Pure scalar comparisons — GRL parses this cleanly.
		sb.WriteString(fmt.Sprintf(
			"rule %s \"band %s\" salience %d { when Fact.CPUUtil >= %.6f && Fact.CPUUtil < %.6f && Fact.Action == \"\" then Fact.Action = \"%s\"; }\n\n",
			s.name, s.name, s.salience, s.lowCPU, s.highCPU, s.action,
		))
	}

	if err := rb.BuildRuleFromResource(gruleKBName, gruleKBVer, grulepkg.NewBytesResource([]byte(sb.String()))); err != nil {
		return nil, fmt.Errorf("grule failed to parse scalar rules: %w", err)
	}

	return &gruleScalarEngine{engine: engine.NewGruleEngine(), lib: lib}, nil
}

// Evaluate binds the fact into a fresh DataContext and runs the REAL grule
// inference cycle, returning the Action grule wrote back onto the fact.
func (g *gruleScalarEngine) Evaluate(ctx context.Context, fact *ScalarFact) (string, error) {
	fact.Action = "" // reset so rules re-evaluate on each call
	dctx := ast.NewDataContext()
	if err := dctx.Add("Fact", fact); err != nil {
		return "", err
	}
	kb := g.lib.GetKnowledgeBase(gruleKBName, gruleKBVer)
	if kb == nil {
		return "", fmt.Errorf("grule knowledge base not found")
	}
	if err := g.engine.ExecuteWithContext(ctx, dctx, kb); err != nil {
		return "", err
	}
	return fact.Action, nil
}

// ============================================================================
// CORRECTNESS PROOF — identical outputs on identical inputs
// ============================================================================

func TestM22_ScalarFairCorrectness(t *testing.T) {
	ctx := context.Background()

	for _, n := range []int{10, 30} {
		specs := makeScalarRuleSpecs(n)
		native := newNativeScalarEngine(specs)

		grule, err := newGruleScalarEngine(specs)
		if err != nil {
			t.Fatalf("N=%d: REAL grule MUST parse scalar rules but failed: %v", n, err)
		}

		// Probe a spread of CPUUtil inputs; verify identical Action every time.
		inputs := []float64{5.0, 12.7, 33.3, 50.0, 66.6, 75.5, 88.8, 99.9}
		mismatches := 0
		for _, cpu := range inputs {
			fn := NewScalarFact()
			fn.CPUUtil = cpu
			fg := NewScalarFact()
			fg.CPUUtil = cpu

			nativeAction := native.Evaluate(fn)
			gruleAction, err := grule.Evaluate(ctx, fg)
			if err != nil {
				t.Fatalf("N=%d cpu=%.1f: grule execution error: %v", n, cpu, err)
			}
			if nativeAction != gruleAction {
				mismatches++
				t.Errorf("N=%d cpu=%.1f: MISMATCH native=%q grule=%q", n, cpu, nativeAction, gruleAction)
			} else {
				t.Logf("N=%d cpu=%5.1f → native=%-10q grule=%-10q ✓ identical", n, cpu, nativeAction, gruleAction)
			}
		}
		if mismatches == 0 {
			t.Logf("✅ N=%d: CORRECTNESS PROVEN — REAL grule and EdgeAutonomy produce IDENTICAL actions on all %d inputs", n, len(inputs))
		}
	}
}

// ============================================================================
// HEAD-TO-HEAD BENCHMARKS — REAL grule vs EdgeAutonomy native
// ============================================================================

func benchNative(b *testing.B, n int) {
	specs := makeScalarRuleSpecs(n)
	e := newNativeScalarEngine(specs)
	fact := NewScalarFact()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.Evaluate(fact)
	}
}

func benchGrule(b *testing.B, n int) {
	ctx := context.Background()
	specs := makeScalarRuleSpecs(n)
	e, err := newGruleScalarEngine(specs)
	if err != nil {
		b.Fatalf("REAL grule MUST parse scalar rules but failed: %v", err)
	}
	fact := NewScalarFact()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := e.Evaluate(ctx, fact); err != nil {
			b.Fatalf("grule execution error: %v", err)
		}
	}
}

func BenchmarkM22_Native_Rule10(b *testing.B)  { benchNative(b, 10) }
func BenchmarkM22_Native_Rule30(b *testing.B)  { benchNative(b, 30) }
func BenchmarkM22_Grule_Rule10(b *testing.B)   { benchGrule(b, 10) }
func BenchmarkM22_Grule_Rule30(b *testing.B)   { benchGrule(b, 30) }

// ============================================================================
// In-process median verdict (count=6 style) for a self-contained summary
// ============================================================================

func TestM22_ScalarFairVerdict(t *testing.T) {
	ctx := context.Background()

	for _, n := range []int{10, 30} {
		specs := makeScalarRuleSpecs(n)
		native := newNativeScalarEngine(specs)
		grule, err := newGruleScalarEngine(specs)
		if err != nil {
			t.Fatalf("N=%d: REAL grule MUST parse but failed: %v", n, err)
		}
		fact := NewScalarFact()

		const iters = 20000
		const runs = 6
		nativeMed := make([]time.Duration, runs)
		gruleMed := make([]time.Duration, runs)

		for r := 0; r < runs; r++ {
			start := time.Now()
			for i := 0; i < iters; i++ {
				native.Evaluate(fact)
			}
			nativeMed[r] = time.Since(start) / iters

			start = time.Now()
			for i := 0; i < iters; i++ {
				if _, err := grule.Evaluate(ctx, fact); err != nil {
					t.Fatalf("grule execution error: %v", err)
				}
			}
			gruleMed[r] = time.Since(start) / iters
		}

		mn := medianDuration(nativeMed)
		mg := medianDuration(gruleMed)
		perRuleNative := float64(mn.Nanoseconds()) / float64(n)
		perRuleGrule := float64(mg.Nanoseconds()) / float64(n)

		t.Logf("\n=== M22 SCALAR FAIR VERDICT (N=%d rules, %d runs median) ===", n, runs)
		t.Logf("EdgeAutonomy native : %v/eval  (%.1f ns/rule)", mn, perRuleNative)
		t.Logf("REAL grule v1.20.4  : %v/eval  (%.1f ns/rule)", mg, perRuleGrule)
		if mn < mg {
			t.Logf("🏆 CLEAN WIN: EdgeAutonomy is %.1fx faster than REAL grule (identical outputs)", float64(mg)/float64(mn))
		} else {
			t.Logf("⚠️ LOSS: REAL grule is %.1fx faster", float64(mn)/float64(mg))
		}
	}
}
