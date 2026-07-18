package evidence

import (
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// builder.go assembles a production-ready Ledger and — crucially — reports each
// of its backends (signer key, ledger store, transparency anchor) to
// pkg/capability. That is what makes the evidence plane obey the same honesty
// contract as the rest of the platform: an ephemeral in-process key or an
// in-memory (non-durable) ledger is allowed in dev, surfaced in staging, and
// makes a production boot refuse to start (capability.Enforce()).

// BuildConfig configures how the evidence Ledger resolves its backends. Any zero
// value degrades to a simulated fallback that is honestly reported.
type BuildConfig struct {
	DB            *gorm.DB       // non-nil -> durable GORM store; nil -> in-memory (simulated)
	SigningKeyPEM []byte         // non-empty -> operator key; empty -> ephemeral key (simulated)
	RekorURL      string         // non-empty -> real transparency anchor (Phase 5); empty -> simulated
	Logger        *logrus.Logger // optional; used to surface simulated backends loudly
}

// Build resolves the signer, store, and anchorer, reports each to the process
// capability registry, and returns a wired Ledger. In production it returns an
// error if any backend can only be simulated.
func Build(cfg BuildConfig) (*Ledger, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	var errs []error

	// --- Signer: real Ed25519 crypto always; the KEY provenance is what varies.
	var signer *Ed25519Signer
	if len(cfg.SigningKeyPEM) > 0 {
		s, err := NewSignerFromPEM(cfg.SigningKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("evidence: load signing key: %w", err)
		}
		signer = s
		if err := capability.MustReal("evidence.signer", "ed25519-pem", true,
			"operator-provisioned Ed25519 signing key"); err != nil {
			errs = append(errs, err)
		}
	} else {
		s, err := GenerateEphemeralSigner()
		if err != nil {
			return nil, err
		}
		signer = s
		logger.Warn("evidence: using EPHEMERAL signing key (dev/sim only); receipts cannot be verified across restarts")
		if err := capability.MustReal("evidence.signer", "ed25519-ephemeral", false,
			"in-process ephemeral key; set CLOUDAI_EVIDENCE_KEY for a stable, verifiable identity"); err != nil {
			errs = append(errs, err)
		}
	}

	// --- Store: durable Postgres/GORM vs non-durable in-memory.
	var store Store
	if cfg.DB != nil {
		gs, err := NewGORMStore(cfg.DB)
		if err != nil {
			return nil, err
		}
		store = gs
		if err := capability.MustReal("evidence.ledger", "gorm", true,
			"append-only evidence_records table"); err != nil {
			errs = append(errs, err)
		}
	} else {
		store = NewMemoryStore()
		logger.Warn("evidence: using IN-MEMORY ledger (non-durable); receipts are lost on restart")
		if err := capability.MustReal("evidence.ledger", "memory", false,
			"in-memory ledger; attach a database for a durable, auditable chain"); err != nil {
			errs = append(errs, err)
		}
	}

	// --- Anchorer: real Rekor when configured & reachable, else honest-simulated.
	var anchorer Anchorer = NewSimulatedAnchorer()
	if cfg.RekorURL != "" {
		rekor, rerr := NewRekorAnchorer(cfg.RekorURL, logger)
		if rerr != nil {
			// Configured but unreachable: fail fast in production (you asked for a
			// real transparency log); otherwise fall back to the honest simulated
			// anchor and surface why rather than pretending it worked.
			if capability.Policy().IsProduction() {
				return nil, fmt.Errorf("evidence: rekor configured but unreachable: %w", rerr)
			}
			logger.WithError(rerr).Warn("evidence: rekor unreachable; anchoring falls back to simulated")
		} else {
			anchorer = rekor
			logger.WithField("rekor_url", cfg.RekorURL).Info("evidence: real Rekor transparency anchoring enabled")
		}
	}
	if err := capability.MustReal("evidence.transparency", anchorer.Backend(), anchorer.Real(),
		"external transparency log (Rekor) for independent inclusion proofs"); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return NewLedger(LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: anchorer,
		Cap:      DefaultCapabilitySource(),
	})
}
