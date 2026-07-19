// Command cafctl is the CloudAI Fusion control & verification CLI. Its flagship
// job is `cafctl verify`: independently checking an exported evidence chain
// OFFLINE, with no access to the running platform. This is what turns the
// platform's "prove it" promise into something an auditor can actually exercise.
//
// Typical use:
//
//	# export the ledger from the API, then verify it against a pinned key
//	curl -s $API/api/v1/evidence/export > chain.json
//	cafctl verify --bundle chain.json --pubkey trusted.pem
//
// Exit code is 0 only when every record's hash, chain link, and signature check
// out; any tamper or key mismatch yields a non-zero exit.
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/delivery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/provenance"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "cafctl",
		Short:         "CloudAI Fusion control & verification CLI",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newVerifyInclusionCmd())
	root.AddCommand(newVerifyConsistencyCmd())
	root.AddCommand(newVerifyCompletenessCmd())
	root.AddCommand(newVerifyZKCmd())
	root.AddCommand(newVerifyModelProvenanceCmd())
	root.AddCommand(newVerifyDeployCmd())
	root.AddCommand(newVerifyFailoverCmd())
	root.AddCommand(newVerifyEdgeCmd())
	root.AddCommand(newVerifyRemediationCmd())
	root.AddCommand(newVerifyIsolationCmd())
	root.AddCommand(newVerifySagaCmd())
	return root
}

func newVerifyCmd() *cobra.Command {
	var (
		bundlePath string
		pubkeyPath string
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Offline-verify an exported evidence chain",
		Long: "Verify a signed, hash-chained evidence bundle exported from " +
			"GET /api/v1/evidence/export. With --pubkey the chain is verified against a " +
			"PINNED public key (recommended); without it, the bundle's embedded key is " +
			"used, which only proves internal consistency.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			bundleBytes, err := readInput(bundlePath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var pubkeyBytes []byte
			if pubkeyPath != "" {
				pubkeyBytes, err = os.ReadFile(pubkeyPath)
				if err != nil {
					return fmt.Errorf("read pubkey: %w", err)
				}
			}
			ok, err := runVerify(bundleBytes, pubkeyBytes, jsonOut, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("evidence chain verification FAILED")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bundlePath, "bundle", "-", "path to exported evidence bundle JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to a pinned Ed25519 public key PEM (recommended)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the machine-readable verification report")
	return cmd
}

func readInput(path string, stdin io.Reader) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

// newVerifyInclusionCmd verifies an RFC 6962 inclusion proof (from
// GET /api/v1/evidence/records/:id/proof) against a pinned public key.
func newVerifyInclusionCmd() *cobra.Command {
	var proofPath, pubkeyPath string
	cmd := &cobra.Command{
		Use:   "verify-inclusion",
		Short: "Offline-verify an evidence inclusion proof against a pinned key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(proofPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var proof evidence.InclusionProofResponse
			if err := json.Unmarshal(data, &proof); err != nil {
				return fmt.Errorf("parse inclusion proof: %w", err)
			}
			if err := evidence.VerifyInclusionResponse(&proof, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Inclusion proof: INVALID (%v)\n", err)
				return fmt.Errorf("inclusion proof verification FAILED")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Inclusion proof: VALID (record %s at leaf %d of %d)\n", proof.RecordID, proof.LeafIndex, proof.TreeSize)
			return nil
		},
	}
	cmd.Flags().StringVar(&proofPath, "proof", "-", "path to inclusion proof JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyConsistencyCmd verifies an append-only consistency proof (from
// GET /api/v1/evidence/consistency) against a pinned public key.
func newVerifyConsistencyCmd() *cobra.Command {
	var proofPath, pubkeyPath string
	cmd := &cobra.Command{
		Use:   "verify-consistency",
		Short: "Offline-verify an append-only consistency proof against a pinned key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(proofPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var proof evidence.ConsistencyProofResponse
			if err := json.Unmarshal(data, &proof); err != nil {
				return fmt.Errorf("parse consistency proof: %w", err)
			}
			if err := evidence.VerifyConsistencyResponse(&proof, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Consistency proof: INVALID (%v)\n", err)
				return fmt.Errorf("consistency proof verification FAILED")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Consistency proof: VALID (append-only from size %d to %d)\n", proof.First, proof.Second)
			return nil
		},
	}
	cmd.Flags().StringVar(&proofPath, "proof", "-", "path to consistency proof JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyCompletenessCmd verifies a namespace completeness proof (from
// GET /api/v1/evidence/completeness) against a pinned public key. It proves the
// proof's members are EXACTLY the sealed set for the namespace - no omission and
// no cherry-picking - which is Moat A / M0 (docs/verifiable-moat-spec.md).
func newVerifyCompletenessCmd() *cobra.Command {
	var proofPath, pubkeyPath string
	cmd := &cobra.Command{
		Use:   "verify-completeness",
		Short: "Offline-verify a namespace completeness proof against a pinned key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(proofPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var proof evidence.CompletenessProof
			if err := json.Unmarshal(data, &proof); err != nil {
				return fmt.Errorf("parse completeness proof: %w", err)
			}
			if err := evidence.VerifyCompleteness(&proof, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Completeness proof: INVALID (%v)\n", err)
				return fmt.Errorf("completeness proof verification FAILED")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Completeness proof: VALID (namespace %q, %d members, no omission/cherry-picking)\n", proof.Namespace, len(proof.Members))
			return nil
		},
	}
	cmd.Flags().StringVar(&proofPath, "proof", "-", "path to completeness proof JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyModelProvenanceCmd verifies a model's training provenance OFFLINE
// against a pinned key: it checks the DatasetManifest and ModelProvenance
// signatures and that the provenance is bound to exactly that manifest, proving
// the weights were produced from that signed, in-scope corpus (Moat B).
func newVerifyModelProvenanceCmd() *cobra.Command {
	var manifestPath, provPath, pubkeyPath string
	cmd := &cobra.Command{
		Use:   "verify-model-provenance",
		Short: "Offline-verify a model's training provenance against a pinned key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			mBytes, err := os.ReadFile(manifestPath)
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			pBytes, err := os.ReadFile(provPath)
			if err != nil {
				return fmt.Errorf("read provenance: %w", err)
			}
			var m provenance.DatasetManifest
			if err := json.Unmarshal(mBytes, &m); err != nil {
				return fmt.Errorf("parse manifest: %w", err)
			}
			var p provenance.ModelProvenance
			if err := json.Unmarshal(pBytes, &p); err != nil {
				return fmt.Errorf("parse provenance: %w", err)
			}
			if err := provenance.VerifyModelProvenance(&m, &p, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Model provenance: INVALID (%v)\n", err)
				return fmt.Errorf("model provenance verification FAILED")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Model provenance: VALID (corpus %q, %d samples, method=%s)\n", m.Filter, m.SampleCount, p.Method)
			return nil
		},
	}
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "path to the signed DatasetManifest JSON (required)")
	cmd.Flags().StringVar(&provPath, "provenance", "", "path to the signed ModelProvenance JSON (required)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	_ = cmd.MarkFlagRequired("manifest")
	_ = cmd.MarkFlagRequired("provenance")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyZKCmd verifies a zkEvidence attestation (Moat A, Layer A1) OFFLINE
// against a pinned verifying key. It proves scope-compliance / completeness over a
// sealed set WITHOUT revealing any receipt — the confidential complement to
// verify-completeness. Exit is non-zero on any failure.
func newVerifyZKCmd() *cobra.Command {
	var attPath, vkPath string
	cmd := &cobra.Command{
		Use:   "verify-zk",
		Short: "Offline-verify a zkEvidence attestation against a pinned verifying key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			aBytes, err := os.ReadFile(attPath)
			if err != nil {
				return fmt.Errorf("read attestation: %w", err)
			}
			vkBytes, err := os.ReadFile(vkPath)
			if err != nil {
				return fmt.Errorf("read verifying key: %w", err)
			}
			var att zk.ZKAttestation
			if err := json.Unmarshal(aBytes, &att); err != nil {
				return fmt.Errorf("parse attestation: %w", err)
			}
			if err := zk.VerifyZK(&att, vkBytes); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "zkEvidence attestation: INVALID (%v)\n", err)
				return fmt.Errorf("zkEvidence verification FAILED")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "zkEvidence attestation: VALID (statement=%s, count=%d, no receipts revealed)\n", att.Statement, att.Count)
			return nil
		},
	}
	cmd.Flags().StringVar(&attPath, "attestation", "", "path to the ZKAttestation JSON (required)")
	cmd.Flags().StringVar(&vkPath, "vk", "", "path to the pinned verifying key bytes (required)")
	_ = cmd.MarkFlagRequired("attestation")
	_ = cmd.MarkFlagRequired("vk")
	return cmd
}

// newVerifyDeployCmd verifies a deploy attestation OFFLINE against a pinned key,
// and (by default) asserts the running deployment did not drift from the signed,
// approved artifact — the DL-1 release gate.
func newVerifyDeployCmd() *cobra.Command {
	var attPath, pubkeyPath string
	requireIntegrity := true
	cmd := &cobra.Command{
		Use:   "verify-deploy",
		Short: "Offline-verify a deploy attestation (asserts no drift by default)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(attPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var att delivery.DeployAttestation
			if err := json.Unmarshal(data, &att); err != nil {
				return fmt.Errorf("parse deploy attestation: %w", err)
			}
			if err := delivery.VerifyDeployAttestation(&att, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Deploy attestation: INVALID (%v)\n", err)
				return fmt.Errorf("deploy attestation verification FAILED")
			}
			if att.DriftDetected {
				fmt.Fprintf(cmd.OutOrStdout(), "Deploy attestation: SIGNED but DRIFT (%s: running %s != signed %s)\n", att.Workload, att.ImageDigest, att.SignedDigest)
				if requireIntegrity {
					return fmt.Errorf("deployment drift detected")
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deploy attestation: VALID (%s on %s matches the signed artifact)\n", att.Workload, att.Cluster)
			return nil
		},
	}
	cmd.Flags().StringVar(&attPath, "attestation", "-", "path to deploy attestation JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	cmd.Flags().BoolVar(&requireIntegrity, "require-integrity", true, "fail if the running deployment drifted from the signed artifact")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyFailoverCmd verifies a failover attestation OFFLINE against a pinned
// key, and (by default) asserts the SLO/BCP gate: exactly one promotion won (no
// split-brain) and measured RTO/RPO within their targets — the DL-2 audit gate.
func newVerifyFailoverCmd() *cobra.Command {
	var attPath, pubkeyPath string
	requireSLO := true
	cmd := &cobra.Command{
		Use:   "verify-failover",
		Short: "Offline-verify a failover attestation (asserts SLO + no split-brain by default)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(attPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var att delivery.FailoverAttestation
			if err := json.Unmarshal(data, &att); err != nil {
				return fmt.Errorf("parse failover attestation: %w", err)
			}
			if err := delivery.VerifyFailoverAttestation(&att, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Failover attestation: INVALID (%v)\n", err)
				return fmt.Errorf("failover attestation verification FAILED")
			}
			if requireSLO {
				if err := att.MeetsSLO(); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Failover attestation: SIGNED but SLO/BCP FAIL (%v)\n", err)
					return fmt.Errorf("failover SLO not met")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Failover attestation: VALID (%s %s->%s, RTO %.0fs / RPO %.0fs, 1 promotion)\n", att.Service, att.FromCluster, att.ToCluster, att.RTOSeconds, att.RPOSeconds)
			return nil
		},
	}
	cmd.Flags().StringVar(&attPath, "attestation", "-", "path to failover attestation JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	cmd.Flags().BoolVar(&requireSLO, "require-slo", true, "fail if RTO/RPO SLO is breached or split-brain is detected")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyEdgeCmd verifies an edge reconciliation OFFLINE against a pinned key.
// It recomputes the offline hash-chain and (by default) asserts every autonomous
// decision was in-policy — the DL-3 gate. A node cannot lie: a false compliance
// claim contradicting the embedded chain fails verification.
func newVerifyEdgeCmd() *cobra.Command {
	var attPath, pubkeyPath string
	requireCompliance := true
	cmd := &cobra.Command{
		Use:   "verify-edge",
		Short: "Offline-verify an edge reconciliation (asserts offline policy compliance by default)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(attPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var att delivery.EdgeReconciliation
			if err := json.Unmarshal(data, &att); err != nil {
				return fmt.Errorf("parse edge reconciliation: %w", err)
			}
			if err := delivery.VerifyEdgeReconciliation(&att, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Edge reconciliation: INVALID (%v)\n", err)
				return fmt.Errorf("edge reconciliation verification FAILED")
			}
			if requireCompliance {
				if err := att.ProvesCompliance(); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Edge reconciliation: SIGNED but NON-COMPLIANT (%v)\n", err)
					return fmt.Errorf("edge node made out-of-policy offline decisions")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Edge reconciliation: VALID (%s, %d offline decisions, all in-policy)\n", att.NodeID, att.DecisionCount)
			return nil
		},
	}
	cmd.Flags().StringVar(&attPath, "attestation", "-", "path to edge reconciliation JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	cmd.Flags().BoolVar(&requireCompliance, "require-compliance", true, "fail if any offline decision was out-of-policy")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyRemediationCmd verifies a differential exploit->fix proof OFFLINE: two
// signed ExploitProofs over the SAME witness where the exploit reproduced pre-fix
// and no longer reproduces post-fix — the RT-1 cryptographic before/after. With
// --require-real it additionally demands both replays were real hermetic runs.
func newVerifyRemediationCmd() *cobra.Command {
	var beforePath, afterPath, pubkeyPath string
	requireReal := false
	cmd := &cobra.Command{
		Use:   "verify-remediation",
		Short: "Offline-verify a differential exploit->fix proof against a pinned key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			bb, err := os.ReadFile(beforePath)
			if err != nil {
				return fmt.Errorf("read before proof: %w", err)
			}
			ab, err := os.ReadFile(afterPath)
			if err != nil {
				return fmt.Errorf("read after proof: %w", err)
			}
			var before, after redteam.ExploitProof
			if err := json.Unmarshal(bb, &before); err != nil {
				return fmt.Errorf("parse before proof: %w", err)
			}
			if err := json.Unmarshal(ab, &after); err != nil {
				return fmt.Errorf("parse after proof: %w", err)
			}
			if err := redteam.VerifyRemediation(&before, &after, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Remediation: NOT PROVEN (%v)\n", err)
				return fmt.Errorf("remediation verification FAILED")
			}
			if requireReal && (!before.ReplayReal || !after.ReplayReal) {
				fmt.Fprintf(cmd.OutOrStdout(), "Remediation: PROVEN but on a SIMULATED replay (before_real=%v after_real=%v)\n", before.ReplayReal, after.ReplayReal)
				return fmt.Errorf("a real hermetic replay was required")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Remediation: PROVEN (finding %s: reproduced pre-fix, no longer reproduces post-fix)\n", before.FindingID)
			return nil
		},
	}
	cmd.Flags().StringVar(&beforePath, "before", "", "path to the pre-fix exploit proof JSON (required)")
	cmd.Flags().StringVar(&afterPath, "after", "", "path to the post-fix exploit proof JSON (required)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	cmd.Flags().BoolVar(&requireReal, "require-real", false, "fail unless both replays were real hermetic runs")
	_ = cmd.MarkFlagRequired("before")
	_ = cmd.MarkFlagRequired("after")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifyIsolationCmd verifies a GPU isolation attestation OFFLINE against a
// pinned key, and (by default) asserts the probe confirmed no cross-tenant memory
// sharing — the CN-3 multi-tenant isolation gate.
func newVerifyIsolationCmd() *cobra.Command {
	var attPath, pubkeyPath string
	requireIsolation := true
	cmd := &cobra.Command{
		Use:   "verify-isolation",
		Short: "Offline-verify a GPU isolation attestation (asserts no cross-tenant sharing by default)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(attPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var att scheduler.IsolationAttestation
			if err := json.Unmarshal(data, &att); err != nil {
				return fmt.Errorf("parse isolation attestation: %w", err)
			}
			if err := scheduler.VerifyIsolationAttestation(&att, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Isolation attestation: INVALID (%v)\n", err)
				return fmt.Errorf("isolation attestation verification FAILED")
			}
			if requireIsolation {
				if err := att.ProvesIsolation(); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Isolation attestation: SIGNED but NOT ISOLATED (%v)\n", err)
					return fmt.Errorf("cross-tenant GPU isolation not proven")
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Isolation attestation: VALID (%s/%d %s, tenants %v isolated)\n", att.Node, att.GPUIndex, att.ShareMode, att.Tenants)
			return nil
		},
	}
	cmd.Flags().StringVar(&attPath, "attestation", "-", "path to isolation attestation JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	cmd.Flags().BoolVar(&requireIsolation, "require-isolation", true, "fail if the probe did not confirm cross-tenant isolation")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// newVerifySagaCmd verifies a cross-pillar saga's completeness proof OFFLINE: it
// proves every recorded step transition is present (no hidden step) and reports the
// terminal outcome (completed / aborted-compensated) — the MF Choreographer surface.
func newVerifySagaCmd() *cobra.Command {
	var proofPath, pubkeyPath string
	cmd := &cobra.Command{
		Use:   "verify-saga",
		Short: "Offline-verify a saga's completeness proof and report its outcome",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pub, err := loadPubkey(pubkeyPath)
			if err != nil {
				return err
			}
			data, err := readInput(proofPath, cmd.InOrStdin())
			if err != nil {
				return err
			}
			var proof evidence.CompletenessProof
			if err := json.Unmarshal(data, &proof); err != nil {
				return fmt.Errorf("parse saga proof: %w", err)
			}
			if err := evidence.VerifyCompleteness(&proof, pub); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Saga: INCOMPLETE/INVALID (%v)\n", err)
				return fmt.Errorf("saga completeness verification FAILED")
			}
			out := fabric.SagaOutcomeOf(&proof)
			status := "completed"
			if out.Aborted {
				status = "aborted (compensated)"
			} else if !out.Completed {
				status = "unterminated"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saga: COMPLETE & VERIFIED (%s, %d steps, %s)\n", out.SagaID, out.Steps, status)
			return nil
		},
	}
	cmd.Flags().StringVar(&proofPath, "proof", "-", "path to saga completeness proof JSON ('-' = stdin)")
	cmd.Flags().StringVar(&pubkeyPath, "pubkey", "", "path to the pinned Ed25519 public key PEM (required)")
	_ = cmd.MarkFlagRequired("pubkey")
	return cmd
}

// loadPubkey reads and parses an Ed25519 public key PEM file.
func loadPubkey(path string) (ed25519.PublicKey, error) {
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pubkey: %w", err)
	}
	return evidence.ParsePublicKeyPEM(pemBytes)
}

// runVerify is the testable core: it verifies bundleBytes (optionally against a
// pinned pubkey) and writes a report to out, returning whether the chain is valid.
func runVerify(bundleBytes, pubkeyBytes []byte, jsonOut bool, out io.Writer) (bool, error) {
	var bundle evidence.ExportBundle
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		return false, fmt.Errorf("parse bundle: %w", err)
	}

	var (
		report  *evidence.VerifyReport
		err     error
		pinned  bool
		keyNote string
	)
	if len(pubkeyBytes) > 0 {
		pub, perr := evidence.ParsePublicKeyPEM(pubkeyBytes)
		if perr != nil {
			return false, fmt.Errorf("parse pinned pubkey: %w", perr)
		}
		pinned = true
		if evidence.KeyIDFor(pub) != bundle.KeyID {
			keyNote = fmt.Sprintf("WARNING: pinned key id %s does not match bundle key id %s", evidence.KeyIDFor(pub), bundle.KeyID)
		}
		report, err = evidence.VerifyBundleWithKey(&bundle, pub)
	} else {
		keyNote = "NOTE: verified against the bundle's EMBEDDED key; pin --pubkey out-of-band to establish trust"
		report, err = evidence.VerifyBundle(&bundle)
	}
	if err != nil {
		return false, err
	}

	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return report.Valid, enc.Encode(report)
	}
	printReport(out, report, pinned, keyNote)
	return report.Valid, nil
}

func printReport(out io.Writer, r *evidence.VerifyReport, pinned bool, keyNote string) {
	verdict := "VALID"
	if !r.Valid {
		verdict = "INVALID"
	}
	fmt.Fprintf(out, "Evidence chain: %s\n", verdict)
	fmt.Fprintf(out, "  key id:        %s (%s)\n", r.KeyID, keyKind(pinned))
	fmt.Fprintf(out, "  records:       %d total, %d verified, %d failed\n", r.Total, r.Verified, r.Failed)
	fmt.Fprintf(out, "  anchored real: %d (Rekor)\n", r.AnchoredReal)
	if r.CheckpointPresent {
		fmt.Fprintf(out, "  checkpoint:    signature=%v root_match=%v (Merkle tree head)\n", r.CheckpointVerified, r.CheckpointRootMatch)
	}
	if r.MerkleRoot != "" {
		fmt.Fprintf(out, "  merkle root:   %s\n", r.MerkleRoot)
	}
	if keyNote != "" {
		fmt.Fprintf(out, "  %s\n", keyNote)
	}
	if !r.Valid {
		fmt.Fprintln(out, "  failing records:")
		for _, rec := range r.Records {
			if rec.HashOK && rec.ChainOK && rec.SignatureOK {
				continue
			}
			fmt.Fprintf(out, "    seq=%d id=%s action=%s hash=%v chain=%v sig=%v %s\n",
				rec.Seq, rec.ID, rec.Action, rec.HashOK, rec.ChainOK, rec.SignatureOK, rec.Error)
		}
	}
}

func keyKind(pinned bool) string {
	if pinned {
		return "pinned"
	}
	return "embedded"
}
