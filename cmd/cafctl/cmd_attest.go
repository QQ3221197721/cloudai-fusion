// Package main - Attestation command for cafctl CLI
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"
)

var (
	attestStatement string
	attestKeyPath   string
	attestJSON      bool
)

// Global flags used by multiple commands
var (
	verbose          bool
	dryRun           bool
	exportFormats    []string
	exportOutputPath string
	exportListOnly   bool
)

// attestCmd represents the 'attest' command
var attestCmd = &cobra.Command{
	Use:   "attest [--statement TEXT]",
	Short: "Record a signed attestation in the evidence chain",
	Long: `Add a cryptographic attestation to your evidence chain. This creates a tamper-evident, 
verifiable record that can be independently validated offline using Ed25519 signatures.

Perfect for capturing important decisions, deployments, or any event that needs to be
proven unalterable in the future.`,
	Example: `  # Record a deployment attestation
  cafctl attest --statement "Deployed v2.3.1 to production cluster gpu-prod-01"
  
  # Record with specific signing key
  cafctl attest --statement "Signed release" --key ~/.caf/release-signing.pem
  
  # Generate new key pair and use it
  cafctl attest --statement "Key rotation" --generate-key
  
  # JSON output for automation
  cafctl attest --statement "CI/CD pipeline passed" --json`,
	RunE: runAttest,
}

func init() {
	rootCmd.AddCommand(attestCmd)
	
	attestCmd.Flags().StringVarP(&attestStatement, "statement", "s", "", 
		"Attestation statement to record")
	attestCmd.Flags().StringVarP(&attestKeyPath, "key", "k", "", 
		"Signing key path (default: use .caf/keys/private.pem from 'cafctl init')")
	attestCmd.Flags().BoolVarP(&attestJSON, "json", "j", false, 
		"Output in JSON format")
}

func runAttest(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	
	// Validate input
	if attestStatement == "" {
		return fmt.Errorf("attestation statement required; use --statement TEXT")
	}
	
	// Local-first: attestations are appended to the project's evidence chain.
	cafDir := ".caf"
	chainPath := filepath.Join(cafDir, "evidence.chain")
	projKeyPath := filepath.Join(cafDir, "keys", "private.pem")

	// Resolve the signing key. Priority: --key flag, then the project key created
	// by 'cafctl init', then an ephemeral fallback.
	var signer evidence.Signer
	if attestKeyPath != "" {
		keyPEM, err := os.ReadFile(filepath.Clean(attestKeyPath))
		if err != nil {
			PrintError("Failed to read key file: %v", err)
			return fmt.Errorf("read key: %w", err)
		}
		s, err := evidence.NewSignerFromPEM(keyPEM)
		if err != nil {
			PrintError("Invalid key file: %v", err)
			return fmt.Errorf("parse key: %w", err)
		}
		signer = s
	} else if keyPEM, err := os.ReadFile(filepath.Clean(projKeyPath)); err == nil {
		s, perr := evidence.NewSignerFromPEM(keyPEM)
		if perr != nil {
			PrintError("Invalid project key: %v", perr)
			return fmt.Errorf("parse key: %w", perr)
		}
		signer = s
	} else {
		s, gerr := evidence.GenerateEphemeralSigner()
		if gerr != nil {
			PrintError("Failed to generate key: %v", gerr)
			return fmt.Errorf("generate key: %w", gerr)
		}
		signer = s
		PrintWarning("No project key found; using an ephemeral key. Run 'cafctl init' for a persistent identity.")
	}

	// Load any existing chain so the new attestation continues the same hash chain.
	// We only append to (and overwrite) an existing chain when our signer matches
	// the chain's key, keeping it verifiable as a single-signer chain.
	store := evidence.NewMemoryStore()
	chainExisted := false
	keyMatches := true
	if bundle, rerr := evidence.ReadBundleFile(chainPath); rerr == nil {
		chainExisted = true
		for _, rec := range bundle.Records {
			if rec.KeyID != signer.KeyID() {
				keyMatches = false
				break
			}
		}
		if keyMatches {
			for _, rec := range bundle.Records {
				if aerr := store.Append(ctx, rec); aerr != nil {
					PrintError("Failed to load existing chain: %v", aerr)
					return aerr
				}
			}
		} else {
			PrintWarning("Signing key does not match the existing chain; recording a standalone (non-persisted) attestation.")
		}
	}

	// Create ledger
	lr, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		PrintError("Failed to initialize ledger: %v", err)
		return err
	}
	
	// Create attestation record
	evidenceID := "caf_att_" + common.NewUUID()[:8]
	recordInput := evidence.RecordInput{
		Actor:   "user",
		Action:  "attest.create",
		Subject: evidenceID,
		Input: map[string]string{
			"id":       evidenceID,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		},
		Output:  map[string]string{"status": "recorded"},
		Payload: map[string]string{"statement": attestStatement},
	}
	
	// Record the attestation
	record, err := lr.Record(ctx, recordInput)
	if err != nil {
		PrintError("Failed to record attestation: %v", err)
		return err
	}

	// Persist the extended chain unless we recorded a standalone attestation against
	// an existing chain signed by a different key.
	persisted := false
	if !chainExisted || keyMatches {
		if bundle, xerr := lr.Export(ctx); xerr == nil {
			if werr := os.MkdirAll(cafDir, 0o755); werr == nil {
				if werr := evidence.WriteBundleFile(chainPath, bundle); werr != nil {
					PrintWarning("Recorded but could not persist chain: %v", werr)
				} else {
					persisted = true
				}
			}
		}
	}

	if attestJSON {
		type AttestResponse struct {
			ID        string `json:"id"`
			Hash      string `json:"hash"`
			KeyID     string `json:"key_id"`
			Timestamp string `json:"timestamp"`
			Persisted bool   `json:"persisted"`
			ChainFile string `json:"chain_file"`
		}
		resp := AttestResponse{
			ID:        evidenceID,
			Hash:      record.Hash[:16] + "...",
			KeyID:     record.KeyID,
			Timestamp: record.Timestamp.Format(time.RFC3339Nano),
			Persisted: persisted,
			ChainFile: chainPath,
		}
		PrintInfo(ToJSON(resp))
	} else {
		fmt.Println("")
		greenBold.Println(OK() + " Attestation recorded")
		cyanBold.Printf("  ID:          %s\n", evidenceID)
		yellowBold.Printf("  Hash:        sha256:%s\n", record.Hash[:16]+"...")
		yellowBold.Printf("  Key ID:      %s\n", record.KeyID)
		yellowBold.Printf("  Statement:   \"%s\"\n", attestStatement)
		fmt.Printf("  Timestamp:   %s\n", record.Timestamp.Format(time.RFC3339Nano))
		if persisted {
			green.Printf("  Chain:       %s (seq #%d)\n", chainPath, record.Seq)
		} else {
			yellow.Printf("  Chain:       not persisted (standalone attestation)\n")
		}
		fmt.Println("")
		yellowBold.Print("  Tip: Verify with: cafctl verify\n")
	}

	return nil
}
