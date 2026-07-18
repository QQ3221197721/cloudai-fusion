//go:build integration

package cluster

import (
	"bytes"
	"context"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func restConfigForContext(name string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: name}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

// TestFailover_Integration_RealClusters runs ONLY with -tags integration against
// TWO real kind clusters (kind-cloudai + kind-cloudai-dr). It proves failover uses
// REAL client-go API-server health probes and emits a verifiable receipt when a
// dead primary is replaced by the healthy DR. Run:
//
//	go test -tags integration ./pkg/cluster/ -run Integration -v
func TestFailover_Integration_RealClusters(t *testing.T) {
	ctx := context.Background()

	primaryCfg, err := restConfigForContext("kind-cloudai")
	if err != nil {
		t.Skipf("no context kind-cloudai: %v", err)
	}
	drCfg, err := restConfigForContext("kind-cloudai-dr")
	if err != nil {
		t.Skipf("no context kind-cloudai-dr: %v", err)
	}
	primaryHealth, err := ClusterHealthChecker(primaryCfg)
	if err != nil {
		t.Fatalf("primary health checker: %v", err)
	}
	drHealth, err := ClusterHealthChecker(drCfg)
	if err != nil {
		t.Fatalf("dr health checker: %v", err)
	}

	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x60}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})

	// Both real clusters healthy -> stays on primary (real probes, no failover).
	fm := NewFailoverManager(FailoverConfig{
		Primary:      ClusterTarget{Name: "kind-cloudai", Health: primaryHealth},
		DR:           ClusterTarget{Name: "kind-cloudai-dr", Health: drHealth},
		HealthDriver: "client-go",
		Recorder:     ledger,
	})
	res, err := fm.Check(ctx)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !res.PrimaryHealthy || !res.DRHealthy || res.FailedOver {
		t.Fatalf("both real clusters healthy -> no failover expected: %+v", res)
	}
	t.Logf("both clusters healthy via REAL probes; active=%s", fm.Active())

	// Now make the primary unreachable (bogus endpoint) while DR stays REAL+healthy.
	badCfg := &rest.Config{Host: "https://127.0.0.1:1", Timeout: 2 * time.Second}
	badHealth, err := ClusterHealthChecker(badCfg)
	if err != nil {
		t.Fatalf("bad health checker: %v", err)
	}
	fm2 := NewFailoverManager(FailoverConfig{
		Primary:      ClusterTarget{Name: "kind-cloudai", Health: badHealth},
		DR:           ClusterTarget{Name: "kind-cloudai-dr", Health: drHealth},
		HealthDriver: "client-go",
		Recorder:     ledger,
	})
	res2, err := fm2.Check(ctx)
	if err != nil {
		t.Fatalf("failover check: %v", err)
	}
	if !res2.FailedOver || res2.To != "kind-cloudai-dr" || !res2.DRHealthy {
		t.Fatalf("dead primary should fail over to the REAL healthy DR: %+v", res2)
	}
	t.Logf("REAL failover: primary unreachable -> promoted DR (dr_healthy=%v via real API probe)", res2.DRHealthy)

	// The failover receipt must be real and the chain must verify offline.
	all, _ := ledger.Store().All(ctx)
	var realFailover bool
	for _, e := range all {
		if e.Action != "failover.promote" {
			continue
		}
		for _, b := range e.Backends {
			if b.Component == "cluster.failover" && b.Mode == "real" && b.Driver == "client-go" {
				realFailover = true
			}
		}
	}
	if !realFailover {
		t.Fatal("expected a real (client-go) failover.promote receipt")
	}
	if rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("failover evidence must verify, got %+v", rep)
	}
}
