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

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
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
		report   *evidence.VerifyReport
		err      error
		pinned   bool
		keyNote  string
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
