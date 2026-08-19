// Package main - Verify evidence chain command for cafctl CLI
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"
)

var (
	verifyChainFile string
	verifyNamespace string
	verifyJSON      bool
)

// verifyCmd represents the 'verify' command
var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify evidence chain integrity",
	Long: `Verify the integrity of a CloudAI Fusion evidence chain by checking:
• Ed25519 signatures on each record
• Merkle hash continuity between records
• Optional Rekor transparency log inclusion proofs

The command reads evidence from a local file or uses the default location
at .caf/evidence.chain. This is our "docker run nginx" moment — instant,
offline-verifiable proof that your control plane actions are real and untampered.`,
	Example: `  # Verify default chain file
  cafctl verify
  
  # Verify specific chain file  
  cafctl verify --chain-file /path/to/chain.json
  
  # Verify with public key pinning
  cafctl verify --chain-file chain.json --key ~/.caf/public.pem
  
  # Output as JSON for scripting
  cafctl verify --json`,
	RunE:          runVerify,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.AddCommand(verifyCmd)
	
	verifyCmd.Flags().StringVarP(&verifyChainFile, "chain-file", "f", "", 
		"Evidence chain file path (default: .caf/evidence.chain)")
	verifyCmd.Flags().StringVarP(&verifyNamespace, "namespace", "n", "", 
		"Filter records by namespace (optional)")
	verifyCmd.Flags().BoolVarP(&verifyJSON, "json", "j", false, 
		"Output in JSON format")
}

func runVerify(cmd *cobra.Command, args []string) error {
	// Load chain file
	chainPath := verifyChainFile
	if chainPath == "" {
		chainPath = filepath.Join(".caf", "evidence.chain")
	}
	
	bundle, err := evidence.ReadBundleFile(chainPath)
	if err != nil {
		if os.IsNotExist(err) {
			PrintError("Evidence chain file not found: %s", chainPath)
			PrintInfo("Run 'cafctl init' to initialize an evidence chain")
			return fmt.Errorf("chain file not found")
		}
		PrintError("Failed to read chain file: %v", err)
		return err
	}
	
	// Verify bundle. For large chains, use the parallel verifier: per-record
	// Ed25519 + SHA-256 verification is embarrassingly parallel and the parallel
	// path returns an identical report (see evidence.VerifyBundleParallel). Below
	// the threshold the sequential path avoids goroutine setup overhead.
	var report *evidence.VerifyReport
	if len(bundle.Records) >= evidence.ParallelVerifyThreshold {
		report, err = evidence.VerifyBundleParallel(bundle, 0) // 0 => auto-detect CPUs
	} else {
		report, err = evidence.VerifyBundle(bundle)
	}
	if err != nil {
		PrintError("Verification failed: %v", err)
		return err
	}
	
	if verifyJSON {
		printVerifyJSON(report)
		if !report.Valid {
			return fmt.Errorf("evidence chain verification failed")
		}
		return nil
	}
	
	if report.Valid {
		printVerifySuccess(bundle, report)
	} else {
		printVerifyFailure(bundle, report)
		return fmt.Errorf("evidence chain verification failed")
	}
	
	return nil
}

func printVerifySuccess(bundle *evidence.ExportBundle, report *evidence.VerifyReport) {
	totalEntries := len(bundle.Records)
	firstEntry := "<none>"
	lastEntry := "<none>"
	signers := make(map[string]bool)
	
	if totalEntries > 0 {
		first := bundle.Records[0]
		last := bundle.Records[totalEntries-1]
		
		firstEntry = first.Timestamp.Format("2006-01-02T15:04:05Z") + " (" + first.Action + ")"
		lastEntry = last.Timestamp.Format("2006-01-02T15:04:05Z") + " (" + last.Action + ")"
		
		signers[bundle.KeyID] = true
		for _, key := range bundle.Keys {
			signers[key.KeyID] = true
		}
	}
	
	numSigners := len(signers)
	
	fmt.Println("")
	greenBold.Println(OK() + " Evidence chain verified")
	cyanBold.Println("  Entries:   ", totalEntries)
	yellowBold.Println("  First:     ", firstEntry)
	yellowBold.Println("  Last:      ", lastEntry)
	yellowBold.Println("  Signers:   ", numSigners, "unique keys")
	greenBold.Println("  Status:    INTACT — no tampering detected")
	
	if report.AnchoredReal > 0 {
		greenBold.Printf("  Rekor:     %d entries externally anchored\n", report.AnchoredReal)
	}
	
	if report.RekorVerified > 0 {
		greenBold.Printf("  Proofs:    %d transparency proofs verified\n", report.RekorVerified)
	}
	
	if report.Valid && report.MerkleRoot != "" {
		fmt.Printf("  Root Hash: %s\n", report.MerkleRoot[:16]+"...")
	}
	
	fmt.Println("")
}

func printVerifyFailure(bundle *evidence.ExportBundle, report *evidence.VerifyReport) {
	failedRecords := make([]*evidence.RecordResult, 0)
	for i, r := range report.Records {
		if !r.OK() {
			failedRecords = append(failedRecords, &report.Records[i])
		}
	}
	
	fmt.Println("")
	redBold.Println(ERROR() + " Evidence chain BROKEN")
	
	if totalFailed := report.Failed; totalFailed > 0 {
		redBold.Printf("  Failed at %d record(s):\n", totalFailed)
	}
	
	for idx, result := range failedRecords {
		if idx >= 5 {
			fmt.Println("  ... and", len(failedRecords)-5, "more failures")
			break
		}
		
		entryNum := result.Seq
		timestamp := time.Time{}
		action := result.Action
		hashOk := result.HashOK
		signOk := result.SignatureOK
		chainOk := result.ChainOK
		
		if len(bundle.Records) > int(entryNum)-1 {
			timestamp = bundle.Records[entryNum-1].Timestamp
			action = bundle.Records[entryNum-1].Action
		}
		
		fmt.Printf("\n  Entry #%d (%s)\n", entryNum, timestamp.Format("2006-01-02T15:04:05Z"))
		fmt.Printf("    Action:    %s\n", action)
		
		if !hashOk {
			redBold.Print("    ✗         Hash verification FAILED\n")
		}
		if !signOk {
			redBold.Print("    ✗         Signature verification FAILED\n")
		}
		if !chainOk {
			redBold.Print("    ✗         Chain linkage FAILED\n")
		}
		
		if result.Error != "" {
			fmt.Printf("    Error:     %s\n", result.Error)
		}
		
		if idx < len(failedRecords)-1 {
			fmt.Println("  ---")
		}
	}
	
	redBold.Println("\n  Action: Run 'cafctl repair' to investigate or restore from backup")
	fmt.Println("")
}

// verifyBundleBytes is a testable helper that verifies a serialised bundle
// from raw bytes. It returns (valid, error) and writes human-readable output
// to the supplied writer. pinnedKeyPEM may be nil to skip key-pinning checks;
// when supplied, the chain is verified against exactly that key (rotation-free),
// so a bundle signed by a different key is reported INVALID with a WARNING.
//
// Malformed input (bundle bytes that do not parse) is a verification failure,
// not a caller error: it returns (false, nil) after printing INVALID, so callers
// (e.g. the moat-demo tamper check) can treat any corruption uniformly.
func verifyBundleBytes(bundleJSON []byte, pinnedKeyPEM []byte, verbose bool, out io.Writer) (bool, error) {
	var bundle evidence.ExportBundle
	if err := json.Unmarshal(bundleJSON, &bundle); err != nil {
		fmt.Fprintln(out, "INVALID")
		return false, nil
	}

	var report *evidence.VerifyReport
	var err error
	if len(pinnedKeyPEM) > 0 {
		pub, perr := evidence.ParsePublicKeyPEM(pinnedKeyPEM)
		if perr != nil {
			return false, fmt.Errorf("parse pinned key: %w", perr)
		}
		report, err = evidence.VerifyBundleWithKey(&bundle, pub)
	} else {
		report, err = evidence.VerifyBundle(&bundle)
	}
	if err != nil {
		return false, err
	}

	if report.Valid {
		fmt.Fprintln(out, "VALID")
		if len(pinnedKeyPEM) > 0 {
			fmt.Fprintln(out, "pinned key matched")
		}
	} else {
		fmt.Fprintln(out, "INVALID")
		if len(pinnedKeyPEM) > 0 {
			fmt.Fprintln(out, "WARNING: pinned key did not match bundle signer")
		}
	}
	return report.Valid, nil
}

func printVerifyJSON(report *evidence.VerifyReport) {
	type JSONReport struct {
		Total            int                    `json:"total"`
		Verified         int                    `json:"verified"`
		Failed           int                    `json:"failed"`
		AnchoredReal     int                    `json:"anchored_real"`
		RekorVerified    int                    `json:"rekor_verified"`
		KeyID            string                 `json:"key_id"`
		Valid            bool                   `json:"valid"`
		MerkleRoot       string                 `json:"merkle_root,omitempty"`
		Records          []interface{}          `json:"records"`
		CheckpointPresent bool                  `json:"checkpoint_present"`
		CheckpointVerified bool                `json:"checkpoint_verified"`
		CheckpointRootMatch bool               `json:"checkpoint_root_match"`
	}
	
	jsonReport := JSONReport{
		Total:             report.Total,
		Verified:          report.Verified,
		Failed:            report.Failed,
		AnchoredReal:      report.AnchoredReal,
		RekorVerified:     report.RekorVerified,
		KeyID:             report.KeyID,
		Valid:             report.Valid,
		MerkleRoot:        report.MerkleRoot,
		Records:           nil, // Omit full record details in JSON output
		CheckpointPresent: report.CheckpointPresent,
		CheckpointVerified: report.CheckpointVerified,
		CheckpointRootMatch: report.CheckpointRootMatch,
	}
	
	PrintInfo(ToJSON(jsonReport))
}
