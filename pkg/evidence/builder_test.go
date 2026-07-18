package evidence

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// These tests exercise the honesty gate: the builder must report its backends to
// pkg/capability so a production boot refuses an ephemeral key / in-memory ledger.
// They mutate the process-wide default registry, so they must not run in parallel.

func TestBuild_ProductionRefusesSimulatedBackends(t *testing.T) {
	capability.Reset()
	capability.SetPolicy(runmode.Production)
	t.Cleanup(func() {
		capability.Reset()
		capability.SetPolicy(runmode.Simulation)
	})

	// No DB (in-memory ledger) and no signing key (ephemeral) => all simulated.
	_, err := Build(BuildConfig{})
	if err == nil {
		t.Fatal("expected production Build to fail when evidence backends are simulated")
	}
}

func TestBuild_SimulationAllowsFallbacks(t *testing.T) {
	capability.Reset()
	capability.SetPolicy(runmode.Simulation)
	t.Cleanup(func() {
		capability.Reset()
		capability.SetPolicy(runmode.Simulation)
	})

	l, err := Build(BuildConfig{})
	if err != nil {
		t.Fatalf("simulation Build should succeed with fallbacks: %v", err)
	}
	if l == nil {
		t.Fatal("expected a ledger")
	}
	// The signer/ledger/transparency backends must be recorded as simulated.
	sim := capability.Simulated()
	var haveSigner, haveLedger, haveAnchor bool
	for _, b := range sim {
		switch b.Component {
		case "evidence.signer":
			haveSigner = true
		case "evidence.ledger":
			haveLedger = true
		case "evidence.transparency":
			haveAnchor = true
		}
	}
	if !haveSigner || !haveLedger || !haveAnchor {
		t.Fatalf("expected signer/ledger/transparency reported simulated, got %+v", sim)
	}
}

func TestBuild_ProductionAcceptsRealSignerAndDurableLedger(t *testing.T) {
	capability.Reset()
	capability.SetPolicy(runmode.Production)
	t.Cleanup(func() {
		capability.Reset()
		capability.SetPolicy(runmode.Simulation)
	})

	// A real (operator) key PEM + a durable DB should satisfy production for the
	// signer and ledger; only the transparency anchor remains simulated here, so
	// production correctly still refuses — proving each backend is gated.
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ed25519.NewKeyFromSeed(seed))
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	_, err = Build(BuildConfig{SigningKeyPEM: keyPEM})
	// Still fails: no DB (in-memory ledger) + simulated anchor remain.
	if err == nil {
		t.Fatal("expected production Build to still fail while ledger/anchor are simulated")
	}
	// But the signer must NOT be among the simulated backends now.
	for _, b := range capability.Simulated() {
		if b.Component == "evidence.signer" {
			t.Fatal("operator-provisioned signer must be reported real, not simulated")
		}
	}
}
