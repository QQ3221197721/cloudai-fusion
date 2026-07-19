package fabric_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// choreographer_test.go completes MF: a cross-pillar saga runs, records a signed
// receipt per transition, is completeness-provable (dropping a step fails), and on
// failure compensates in reverse - all offline-verifiable.

func TestChoreographer_CompletesAndProves(t *testing.T) {
	ctx := context.Background()
	ledger := newLedger(t) // helper from fabric_test.go (same test package)
	pub := ledger.Signer().PublicKey()
	ch := fabric.NewChoreographer(ledger, nil)

	step := func(name string) fabric.SagaStep {
		return fabric.SagaStep{Name: name, Do: func(context.Context) error { return nil }}
	}

	// §5.4c: finding -> quarantine -> redeploy -> re-prove.
	res, err := ch.Run(ctx, "saga-1", []fabric.SagaStep{step("quarantine"), step("redeploy"), step("reprove")})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !res.Completed || res.Aborted || res.Steps != 3 {
		t.Fatalf("result = %+v, want completed 3-step", res)
	}

	if _, err := ch.Seal(ctx, "saga-1"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	proof, err := ch.Proof(ctx, "saga-1")
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, pub); err != nil {
		t.Fatalf("saga completeness must verify: %v", err)
	}
	// 3 saga.step + 1 saga.completed.
	if len(proof.Members) != 4 {
		t.Fatalf("saga members = %d, want 4", len(proof.Members))
	}
	out := fabric.SagaOutcomeOf(proof)
	if !out.Completed || out.Aborted || out.Steps != 3 || out.SagaID != "saga-1" {
		t.Fatalf("outcome = %+v", out)
	}

	// Dropping a step from the report MUST fail completeness (no hidden steps).
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	if err := evidence.VerifyCompleteness(proof, pub); err == nil {
		t.Fatal("dropping a saga step MUST fail completeness")
	}
}

func TestChoreographer_AbortsAndCompensates(t *testing.T) {
	ctx := context.Background()
	ledger := newLedger(t)
	pub := ledger.Signer().PublicKey()
	ch := fabric.NewChoreographer(ledger, nil)

	var order []string
	step := func(name string, fail bool) fabric.SagaStep {
		return fabric.SagaStep{
			Name: name,
			Do: func(context.Context) error {
				order = append(order, "do:"+name)
				if fail {
					return fmt.Errorf("boom")
				}
				return nil
			},
			Compensate: func(context.Context) error { order = append(order, "undo:"+name); return nil },
		}
	}

	res, err := ch.Run(ctx, "saga-2", []fabric.SagaStep{step("a", false), step("b", true), step("c", false)})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Completed || !res.Aborted || res.FailedAt != "b" || res.Steps != 1 {
		t.Fatalf("result = %+v, want aborted at b", res)
	}
	// a ran, b failed, a compensated; c never ran.
	if want := []string{"do:a", "do:b", "undo:a"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}

	// The aborted saga is still completeness-provable, and its outcome is "aborted".
	if _, err := ch.Seal(ctx, "saga-2"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	proof, err := ch.Proof(ctx, "saga-2")
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, pub); err != nil {
		t.Fatalf("aborted saga completeness must verify: %v", err)
	}
	out := fabric.SagaOutcomeOf(proof)
	if !out.Aborted || out.Completed {
		t.Fatalf("outcome = %+v, want aborted", out)
	}
}
