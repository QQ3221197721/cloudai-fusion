package cluster

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func failoverLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x5f}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestFailover_PromotesDRAndFailsBack(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := failoverLedger(t)

	primaryUp, drUp := true, true
	fm := NewFailoverManager(FailoverConfig{
		Primary: ClusterTarget{Name: "primary", Health: func(context.Context) error {
			if primaryUp {
				return nil
			}
			return errors.New("primary down")
		}},
		DR: ClusterTarget{Name: "dr", Health: func(context.Context) error {
			if drUp {
				return nil
			}
			return errors.New("dr down")
		}},
		HealthDriver: "memory",
		Recorder:     ledger,
	})
	ctx := context.Background()

	// Both healthy -> stay on primary.
	if res, err := fm.Check(ctx); err != nil || res.FailedOver || fm.Active() != "primary" {
		t.Fatalf("healthy primary should stay active: res=%+v err=%v active=%s", res, err, fm.Active())
	}

	// Primary fails -> promote DR.
	primaryUp = false
	res, err := fm.Check(ctx)
	if err != nil || !res.FailedOver || res.To != "dr" || fm.Active() != "dr" {
		t.Fatalf("primary failure should promote DR: res=%+v err=%v active=%s", res, err, fm.Active())
	}

	// Primary recovers -> fail back to primary.
	primaryUp = true
	res, err = fm.Check(ctx)
	if err != nil || !res.FailedOver || res.To != "primary" || fm.Active() != "primary" {
		t.Fatalf("primary recovery should fail back: res=%+v err=%v active=%s", res, err, fm.Active())
	}

	// Two failover receipts (promote + failback), and the chain must verify.
	all, _ := ledger.Store().All(ctx)
	promotes := 0
	for _, e := range all {
		if e.Action == "failover.promote" {
			promotes++
		}
	}
	if promotes != 2 {
		t.Fatalf("expected 2 failover receipts, got %d", promotes)
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("failover evidence chain must verify, got %+v", rep)
	}
}

func TestFailover_NoHealthyTargetErrors(t *testing.T) {
	t.Cleanup(capability.Reset)
	down := func(context.Context) error { return errors.New("down") }
	fm := NewFailoverManager(FailoverConfig{
		Primary:      ClusterTarget{Name: "primary", Health: down},
		DR:           ClusterTarget{Name: "dr", Health: down},
		HealthDriver: "memory",
	})
	if _, err := fm.Check(context.Background()); err == nil {
		t.Fatal("Check must error when the active cluster is down and no healthy target exists")
	}
}
