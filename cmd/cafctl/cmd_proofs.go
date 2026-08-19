// Package main - Real offline verifiers for the 16-well moat surface.
//
// Each `verify-*` command reads a signed proof/attestation and verifies it
// OFFLINE against a pinned public key via the pkg/evidence, pkg/provenance,
// pkg/redteam, pkg/fabric, pkg/delivery and pkg/scheduler verification cores.
// These commands have ZERO dependency on the running platform — an auditor
// recomputes everything from the public key and the proof bytes, which is the
// whole point of a verifiable control plane.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/delivery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/provenance"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/spf13/cobra"
)

// ============================================================================
// Shared helpers
// ============================================================================

// loadEd25519PubKeyTyped reads a PEM file and returns the parsed Ed25519 public key.
func loadEd25519PubKeyTyped(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pubkey: %w", err)
	}
	pub, err := evidence.ParsePublicKeyPEM(data)
	if err != nil {
		return nil, fmt.Errorf("parse pubkey: %w", err)
	}
	return pub, nil
}

// readJSONFile reads and unmarshals a JSON file into v.
func readJSONFile(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// ============================================================================
// verify-inclusion
// ============================================================================

// newVerifyInclusionCmd verifies a Merkle inclusion proof read from stdin
// against a pinned public key.
func newVerifyInclusionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-inclusion",
		Short: "Verify a Merkle inclusion proof from stdin",
		Long: `Verify that a specific evidence receipt is committed by a signed checkpoint.

Reads a JSON-serialized evidence.InclusionProofResponse from stdin and verifies:
  1. The checkpoint's Ed25519 signature against the pinned key
  2. That the audit path + leaf hash reconstruct the checkpoint's signed root

This gives cryptographic proof the receipt is committed by that checkpoint —
a third party can run this with zero access to the running platform.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}

			var resp evidence.InclusionProofResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("parse proof: %w", err)
			}

			if err := evidence.VerifyInclusionResponse(&resp, pub); err != nil {
				return fmt.Errorf("inclusion proof invalid: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sInclusion proof verified\n", OK())
			fmt.Fprintf(out, "  Record:    %s\n", resp.RecordID)
			fmt.Fprintf(out, "  Leaf:      index %d of %d\n", resp.LeafIndex, resp.TreeSize)
			return nil
		},
	}

	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-consistency
// ============================================================================

// newVerifyConsistencyCmd verifies an append-only consistency proof from stdin.
func newVerifyConsistencyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-consistency",
		Short: "Verify an append-only consistency proof from stdin",
		Long: `Verify that the size-First tree is an append-only prefix of the size-Second tree.

Reads a JSON-serialized evidence.ConsistencyProofResponse from stdin and verifies:
  1. The checkpoint's Ed25519 signature against the pinned key
  2. That no historical receipt was altered or removed between the two sizes

This proves the log is append-only — history cannot be rewritten.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			data, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}

			var resp evidence.ConsistencyProofResponse
			if err := json.Unmarshal(data, &resp); err != nil {
				return fmt.Errorf("parse proof: %w", err)
			}

			if err := evidence.VerifyConsistencyResponse(&resp, pub); err != nil {
				return fmt.Errorf("consistency proof invalid: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sConsistency proof verified\n", OK())
			fmt.Fprintf(out, "  Growth:    size %d -> %d (append-only)\n", resp.First, resp.Second)
			return nil
		},
	}

	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-completeness
// ============================================================================

// newVerifyCompletenessCmd verifies a namespace completeness proof.
func newVerifyCompletenessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-completeness --proof <file> --pubkey <file>",
		Short: "Verify a namespace completeness proof",
		Long: `Verify that a sealed namespace's completeness proof covers ALL its members.

A completeness proof cryptographically proves "every receipt in namespace X is
present, none hidden" — the auditor-facing surface of Moat A. Omitting even one
member fails verification.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			proofPath, _ := cmd.Flags().GetString("proof")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var proof evidence.CompletenessProof
			if err := readJSONFile(proofPath, &proof); err != nil {
				return err
			}

			if err := evidence.VerifyCompleteness(&proof, pub); err != nil {
				return fmt.Errorf("completeness proof invalid: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sCompleteness proof verified\n", OK())
			fmt.Fprintf(out, "  Namespace: %s\n", proof.Namespace)
			fmt.Fprintf(out, "  Members:   %d receipts covered, none hidden\n", len(proof.Members))
			return nil
		},
	}

	cmd.Flags().String("proof", "", "Path to completeness proof JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("proof")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-model-provenance
// ============================================================================

// newVerifyModelProvenanceCmd verifies a signed dataset manifest + model provenance.
func newVerifyModelProvenanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-model-provenance --manifest <m.json> --provenance <p.json> --pubkey <key.pem>",
		Short: "Verify SLSA-style model provenance",
		Long: `Verify a model's provenance chain: dataset manifest + model provenance.

Checks:
  1. The dataset manifest's Ed25519 signature against the pinned key
  2. The model provenance's signature against the same key
  3. That the provenance's dataset_manifest hash binds to the EXACT manifest

Tampering the weights or swapping the manifest fails verification — this is the
SLSA-for-models auditor surface of Moat B.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			manifestPath, _ := cmd.Flags().GetString("manifest")
			provPath, _ := cmd.Flags().GetString("provenance")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var manifest provenance.DatasetManifest
			if err := readJSONFile(manifestPath, &manifest); err != nil {
				return err
			}

			var prov provenance.ModelProvenance
			if err := readJSONFile(provPath, &prov); err != nil {
				return err
			}

			if err := provenance.VerifyManifest(&manifest, pub); err != nil {
				return fmt.Errorf("manifest invalid: %w", err)
			}
			if err := provenance.VerifyProvenance(&prov, pub); err != nil {
				return fmt.Errorf("provenance invalid: %w", err)
			}

			mh, err := provenance.ManifestHash(&manifest)
			if err != nil {
				return fmt.Errorf("hash manifest: %w", err)
			}
			if mh != prov.DatasetManifest {
				return fmt.Errorf("provenance binds a different manifest (%s != %s)", prov.DatasetManifest, mh)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sModel provenance verified\n", OK())
			fmt.Fprintf(out, "  Manifest:  %s\n", shortHex(mh))
			fmt.Fprintf(out, "  Weights:   %s\n", shortHex(prov.WeightsHash))
			fmt.Fprintf(out, "  Method:    %s\n", prov.Method)
			return nil
		},
	}

	cmd.Flags().String("manifest", "", "Path to dataset manifest JSON (required)")
	cmd.Flags().String("provenance", "", "Path to model provenance JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("provenance")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-deploy
// ============================================================================

// newVerifyDeployCmd verifies a deployment attestation's signature and integrity.
func newVerifyDeployCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-deploy --attestation <file> --pubkey <file>",
		Short: "Verify deployment attestation (DL-1: no drift)",
		Long: `Verify a deployment attestation against a pinned public key.

This verifier enforces DL-1 (last-mile provenance): proves the RUNNING deployment
equals the signed+approved artifact it should be. A drift attestation is validly
SIGNED (drift is recorded, not hidden), but it FAILS the default gate because
release promotion requires proven integrity, not just proven authenticity.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			attPath, _ := cmd.Flags().GetString("attestation")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var att delivery.DeployAttestation
			if err := readJSONFile(attPath, &att); err != nil {
				return err
			}

			if err := delivery.VerifyDeployAttestation(&att, pub); err != nil {
				return fmt.Errorf("attestation invalid: %w", err)
			}
			if err := att.ProvesIntegrity(); err != nil {
				return fmt.Errorf("gate failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sDeployment integrity verified\n", OK())
			fmt.Fprintf(out, "  Workload:  %s@%s\n", att.Workload, att.Cluster)
			fmt.Fprintf(out, "  Digest:    %s (no drift)\n", shortHex(att.ImageDigest))
			return nil
		},
	}

	cmd.Flags().String("attestation", "", "Path to deployment attestation JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("attestation")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-edge
// ============================================================================

// newVerifyEdgeCmd verifies an edge node's offline reconciliation.
func newVerifyEdgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-edge --attestation <file> --pubkey <file>",
		Short: "Verify edge offline autonomy compliance (DL-3)",
		Long: `Verify an edge node's signed reconciliation of its offline decisions.

Checks:
  1. The reconciliation's Ed25519 signature against the pinned key
  2. The embedded offline hash-chain (dense, unbroken, per-decision hashes)
  3. That the claimed chain head + compliance flag match the recomputed values
  4. (Default gate) That every offline decision stayed in policy

A node cannot sign a false "all in-policy" claim over a chain that contradicts it.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			attPath, _ := cmd.Flags().GetString("attestation")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var att delivery.EdgeReconciliation
			if err := readJSONFile(attPath, &att); err != nil {
				return err
			}

			if err := delivery.VerifyEdgeReconciliation(&att, pub); err != nil {
				return fmt.Errorf("reconciliation invalid: %w", err)
			}
			if err := att.ProvesCompliance(); err != nil {
				return fmt.Errorf("gate failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sEdge reconciliation verified\n", OK())
			fmt.Fprintf(out, "  Node:      %s\n", att.NodeID)
			fmt.Fprintf(out, "  Decisions: %d offline, all in-policy\n", att.DecisionCount)
			return nil
		},
	}

	cmd.Flags().String("attestation", "", "Path to edge reconciliation JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("attestation")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-failover
// ============================================================================

// newVerifyFailoverCmd verifies a failover attestation against SLO targets.
func newVerifyFailoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-failover --attestation <file> --pubkey <file>",
		Short: "Verify failover attestation meets SLO/BCP targets (DL-2)",
		Long: `Verify a failover attestation's signature and SLO compliance.

Checks:
  1. The attestation's Ed25519 signature against the pinned key
  2. (Default gate) MeetsSLO: exactly ONE promotion won (no split-brain),
     RTO/RPO within declared targets

A split-brain failover (2+ promotions) fails the gate even when validly signed.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			attPath, _ := cmd.Flags().GetString("attestation")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var att delivery.FailoverAttestation
			if err := readJSONFile(attPath, &att); err != nil {
				return err
			}

			if err := delivery.VerifyFailoverAttestation(&att, pub); err != nil {
				return fmt.Errorf("attestation invalid: %w", err)
			}
			if err := att.MeetsSLO(); err != nil {
				return fmt.Errorf("gate failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sFailover attestation verified\n", OK())
			fmt.Fprintf(out, "  Service:   %s\n", att.Service)
			fmt.Fprintf(out, "  RTO/RPO:   %.1fs / %.1fs (within targets)\n", att.RTOSeconds, att.RPOSeconds)
			return nil
		},
	}

	cmd.Flags().String("attestation", "", "Path to failover attestation JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("attestation")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-isolation
// ============================================================================

// newVerifyIsolationCmd verifies a GPU isolation attestation.
func newVerifyIsolationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-isolation --attestation <file> --pubkey <file>",
		Short: "Verify GPU workload isolation attestation (CN-3)",
		Long: `Verify a GPU isolation attestation's signature and isolation verdict.

Checks:
  1. The attestation's Ed25519 signature against the pinned key
  2. (Default gate) The probe verdict: the co-located tenants were isolated

A non-isolated co-placement fails the gate even when validly signed — the
auditor/SLA surface of CN-3.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			attPath, _ := cmd.Flags().GetString("attestation")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var att scheduler.IsolationAttestation
			if err := readJSONFile(attPath, &att); err != nil {
				return err
			}

			if err := scheduler.VerifyIsolationAttestation(&att, pub); err != nil {
				return fmt.Errorf("attestation invalid: %w", err)
			}
			if err := att.ProvesIsolation(); err != nil {
				return fmt.Errorf("gate failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sIsolation attestation verified\n", OK())
			fmt.Fprintf(out, "  Node:      %s\n", att.Node)
			fmt.Fprintf(out, "  Tenants:   isolated\n")
			return nil
		},
	}

	cmd.Flags().String("attestation", "", "Path to isolation attestation JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("attestation")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-remediation
// ============================================================================

// newVerifyRemediationCmd verifies an exploit->fix differential.
func newVerifyRemediationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-remediation --before <b.json> --after <a.json> --pubkey <key.pem>",
		Short: "Verify exploit->fix differential (RT-1)",
		Long: `Verify a remediation via a signed before/after exploit differential.

Checks:
  1. Both exploit proofs' Ed25519 signatures against the pinned key
  2. Both proofs bind the SAME witness (valid differential)
  3. Pre-fix: the exploit REPRODUCED; Post-fix: it did NOT

A "fix" whose post-fix proof still reproduces fails verification — the auditor
surface of RT-1.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			beforePath, _ := cmd.Flags().GetString("before")
			afterPath, _ := cmd.Flags().GetString("after")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var before, after redteam.ExploitProof
			if err := readJSONFile(beforePath, &before); err != nil {
				return err
			}
			if err := readJSONFile(afterPath, &after); err != nil {
				return err
			}

			if err := redteam.VerifyRemediation(&before, &after, pub); err != nil {
				return fmt.Errorf("remediation not proven: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sRemediation verified\n", OK())
			fmt.Fprintf(out, "  Finding:   %s (%s)\n", before.FindingID, before.Technique)
			fmt.Fprintf(out, "  Differential: reproduced pre-fix, NOT reproduced post-fix\n")
			return nil
		},
	}

	cmd.Flags().String("before", "", "Path to pre-fix exploit proof JSON (required)")
	cmd.Flags().String("after", "", "Path to post-fix exploit proof JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("before")
	_ = cmd.MarkFlagRequired("after")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}

// ============================================================================
// verify-saga
// ============================================================================

// newVerifySagaCmd verifies a cross-pillar saga's completeness proof.
func newVerifySagaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify-saga --proof <file> --pubkey <file>",
		Short: "Verify saga completeness and outcome",
		Long: `Verify a choreographed saga's completeness proof and report its outcome.

Checks:
  1. Every step receipt's signature against the pinned key
  2. The completeness proof: every saga step is present, none hidden

Then reports the saga outcome (steps completed vs aborted) derived ONLY from
the verified members — the MF Choreographer auditor surface.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			proofPath, _ := cmd.Flags().GetString("proof")
			pubkeyPath, _ := cmd.Flags().GetString("pubkey")

			pub, err := loadEd25519PubKeyTyped(pubkeyPath)
			if err != nil {
				return err
			}

			var proof evidence.CompletenessProof
			if err := readJSONFile(proofPath, &proof); err != nil {
				return err
			}

			if err := evidence.VerifyCompleteness(&proof, pub); err != nil {
				return fmt.Errorf("saga proof invalid: %w", err)
			}

			outcome := fabric.SagaOutcomeOf(&proof)

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%sSaga verified\n", OK())
			fmt.Fprintf(out, "  Saga:      %s\n", outcome.SagaID)
			fmt.Fprintf(out, "  Steps:     %d receipts, none hidden\n", outcome.Steps)
			if outcome.Aborted {
				fmt.Fprintf(out, "  Outcome:   ABORTED (compensations ran)\n")
			} else if outcome.Completed {
				fmt.Fprintf(out, "  Outcome:   COMPLETED\n")
			}
			return nil
		},
	}

	cmd.Flags().String("proof", "", "Path to saga completeness proof JSON (required)")
	cmd.Flags().String("pubkey", "", "Path to PEM public key (required)")
	_ = cmd.MarkFlagRequired("proof")
	_ = cmd.MarkFlagRequired("pubkey")

	return cmd
}
