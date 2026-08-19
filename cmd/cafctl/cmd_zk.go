// Package main - Zero-Knowledge proof demonstration commands for cafctl CLI.
//
// The `zk-demo` command group is a one-command, end-to-end tour of the platform's
// Groth16 + Poseidon2 attestation pipeline (pkg/evidence/zk). It lets a developer
// FEEL the moat: generate a real succinct proof over a set of confidential evidence
// witnesses, then verify it fully offline against a pinned verifying key. Nothing
// here fakes cryptography — it drives the exact same Groth16Prover/VerifyZK used in
// production, just with a small, self-contained set of demo witnesses.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
	"github.com/spf13/cobra"
)

// zk-demo flag state. Prefixed to avoid clashing with other command globals.
var (
	zkDemoOutputPath   string // where the attestation JSON is written by `generate`
	zkDemoVKOutputPath string // where the verifying-key bytes are written by `generate`
	zkDemoCount        int    // number of demo witnesses to prove over
	zkDemoNamespace    string // demo scope/namespace label
	zkDemoJSON         bool   // machine-readable JSON output instead of the pretty tour
)

// zkDemoCmd is the `zk-demo` command group.
var zkDemoCmd = &cobra.Command{
	Use:   "zk-demo",
	Short: "End-to-end Groth16 + Poseidon2 zero-knowledge proof demonstration",
	Long: `Experience the CloudAI Fusion zero-knowledge attestation pipeline in one command.

zk-demo drives the REAL prover and verifier from pkg/evidence/zk:
  - generate: compile a Groth16 circuit, run a trusted setup, and produce a
    succinct proof over a set of confidential evidence witnesses. The proof reveals
    NOTHING about the individual records — only that they exist, are all in scope,
    and hash to a public commitment.
  - verify:   check a generated proof fully offline against its pinned verifying
    key. No prover, no secrets, no network — exactly what a third-party auditor does.

This is our Dockerfile-equivalent moat: once a team accumulates months of
offline-verifiable proof chains, migrating away means walking away from every
attestation an auditor already trusts.

Examples:
  # Generate a demo attestation (proof.json) + its verifying key (vk.bin)
  cafctl zk-demo generate --output _tmp/zkp/proof.json --vk-output _tmp/zkp/vk.bin

  # Verify the generated attestation offline
  cafctl zk-demo verify _tmp/zkp/proof.json _tmp/zkp/vk.bin`,
}

// zkDemoGenerateCmd implements `zk-demo generate`.
var zkDemoGenerateCmd = &cobra.Command{
	Use:   "generate [flags]",
	Short: "Generate a real Groth16 attestation over demo evidence witnesses",
	Long: `Generate a real zero-knowledge attestation and its verifying key.

The command builds a set of confidential "leaf witnesses" (each a semantic
projection of a sealed evidence receipt), then invokes the Groth16 prover:

  1. Compile the completeness circuit for exactly N members.
  2. Run a Groth16 trusted setup (per-circuit; the VKID pins the resulting key).
  3. Compute the public Poseidon2 commitments off-circuit.
  4. Produce a succinct proof that all N members are in scope and hash to the
     committed root — WITHOUT revealing any witness.

Two artifacts are written:
  - the attestation JSON (public inputs + proof + VKID), safe to publish.
  - the verifying key bytes, published out-of-band and pinned by VKID.`,
	Example: `  cafctl zk-demo generate
  cafctl zk-demo generate --output proof.json --vk-output vk.bin --count 10
  cafctl zk-demo generate --namespace redteam/engagement/demo --json`,
	RunE: runZKDemoGenerate,
}

// zkDemoVerifyCmd implements `zk-demo verify`.
var zkDemoVerifyCmd = &cobra.Command{
	Use:   "verify <proof.json> <vk.bin>",
	Short: "Verify a generated attestation fully offline",
	Long: `Verify a generated attestation against its pinned verifying key.

Verification is pure and offline: it needs only the attestation JSON and the
verifying-key bytes. It confirms that:
  - the verifying key's SHA-256 matches the attestation's VKID (no key swap), and
  - the Groth16 proof is valid for the attestation's public inputs.

A simulated / proof-less attestation is rejected. This is exactly what a
third-party auditor runs — no trust in the prover required.`,
	Example: `  cafctl zk-demo verify proof.json vk.bin
  cafctl zk-demo verify _tmp/zkp/proof.json _tmp/zkp/vk.bin --json`,
	Args: cobra.ExactArgs(2),
	RunE: runZKDemoVerify,
}

func init() {
	rootCmd.AddCommand(zkDemoCmd)
	zkDemoCmd.AddCommand(zkDemoGenerateCmd)
	zkDemoCmd.AddCommand(zkDemoVerifyCmd)

	// generate flags
	zkDemoGenerateCmd.Flags().StringVarP(&zkDemoOutputPath, "output", "o", "proof.json", "Path to write the attestation JSON")
	zkDemoGenerateCmd.Flags().StringVar(&zkDemoVKOutputPath, "vk-output", "vk.bin", "Path to write the verifying key bytes")
	zkDemoGenerateCmd.Flags().IntVar(&zkDemoCount, "count", 10, "Number of demo evidence witnesses to prove over")
	zkDemoGenerateCmd.Flags().StringVar(&zkDemoNamespace, "namespace", "demo", "Demo scope/namespace label")
	zkDemoGenerateCmd.Flags().BoolVar(&zkDemoJSON, "json", false, "Emit machine-readable JSON instead of the pretty tour")

	// verify flags
	zkDemoVerifyCmd.Flags().BoolVar(&zkDemoJSON, "json", false, "Emit machine-readable JSON instead of the pretty tour")
}

// buildDemoWitnesses constructs n in-scope confidential witnesses for the demo
// namespace. Each witness mirrors a sealed evidence receipt: same scope, a
// monotonic index, in-scope=true, and a SHA-256 content hash reduced into the
// BN254 scalar field. These are the private inputs the circuit reasons over.
func buildDemoWitnesses(n int, namespace string) []zk.LeafWitness {
	nsFE := zk.FieldFromBytes([]byte(namespace))
	ws := make([]zk.LeafWitness, n)
	for i := range ws {
		h := sha256.Sum256([]byte(fmt.Sprintf("witness-%d", i)))
		ws[i] = zk.LeafWitness{
			Namespace:   nsFE,
			Eidx:        uint64(i),
			InScope:     true,
			PayloadHash: zk.FieldFromBytes(h[:]),
		}
	}
	return ws
}

// zkGenerateResult is the machine-readable summary emitted with --json.
type zkGenerateResult struct {
	Statement   string  `json:"statement"`
	Namespace   string  `json:"namespace"`
	Count       int     `json:"count"`
	PublicRoot  string  `json:"public_root"`
	ScopeCommit string  `json:"scope_commit"`
	VKID        string  `json:"vk_id"`
	Mode        string  `json:"mode"`
	ProofBytes  int     `json:"proof_bytes"`
	VKBytes     int     `json:"vk_bytes"`
	ElapsedMS   float64 `json:"elapsed_ms"`
	ProofPath   string  `json:"proof_path"`
	VKPath      string  `json:"vk_path"`
}

func runZKDemoGenerate(cmd *cobra.Command, args []string) error {
	if zkDemoCount <= 0 {
		PrintError("--count must be a positive integer (got %d)", zkDemoCount)
		return fmt.Errorf("invalid --count: %d", zkDemoCount)
	}

	if !zkDemoJSON {
		printZKBanner("GENERATE", "Groth16 + Poseidon2 zero-knowledge attestation")
		cyanBold.Printf("  Statement:   %s\n", zk.StmtCompletePredicate)
		cyanBold.Printf("  Namespace:   %s\n", zkDemoNamespace)
		cyanBold.Printf("  Witnesses:   %d confidential evidence records\n\n", zkDemoCount)
	}

	// Step 1: build the confidential witness set.
	zkStep(1, 4, "Building confidential witness set")
	witnesses := buildDemoWitnesses(zkDemoCount, zkDemoNamespace)
	zkStepDone(1, 4, fmt.Sprintf("%d witnesses ready", len(witnesses)))

	// Steps 2-3: compile circuit + trusted setup + prove all happen inside Prove.
	// We frame it as a single measured cryptographic operation and time it honestly.
	zkStep(2, 4, "Compiling circuit, running trusted setup, and proving")
	start := time.Now()
	prover := zk.Groth16Prover{}
	attestation, vkBytes, err := prover.Prove(cmd.Context(), zk.StmtCompletePredicate, "all-in-scope", witnesses)
	elapsed := time.Since(start)
	if err != nil {
		PrintError("Proof generation failed: %v", err)
		return fmt.Errorf("generate proof: %w", err)
	}
	zkStepDone(2, 4, fmt.Sprintf("proof produced in %s", elapsed.Round(time.Millisecond)))

	// Step 3: persist the attestation JSON.
	zkStep(3, 4, "Writing attestation JSON")
	if err := writeAttestationJSON(zkDemoOutputPath, attestation); err != nil {
		PrintError("Failed to write attestation: %v", err)
		return err
	}
	zkStepDone(3, 4, zkDemoOutputPath)

	// Step 4: persist the verifying key bytes.
	zkStep(4, 4, "Writing verifying key")
	if err := writeBytesFile(zkDemoVKOutputPath, vkBytes); err != nil {
		PrintError("Failed to write verifying key: %v", err)
		return err
	}
	zkStepDone(4, 4, zkDemoVKOutputPath)

	if zkDemoJSON {
		res := zkGenerateResult{
			Statement:   string(attestation.Statement),
			Namespace:   zkDemoNamespace,
			Count:       attestation.Count,
			PublicRoot:  attestation.PublicRoot,
			ScopeCommit: attestation.ScopeCommit,
			VKID:        attestation.VKID,
			Mode:        attestation.Mode,
			ProofBytes:  len(attestation.Proof),
			VKBytes:     len(vkBytes),
			ElapsedMS:   float64(elapsed.Microseconds()) / 1000.0,
			ProofPath:   zkDemoOutputPath,
			VKPath:      zkDemoVKOutputPath,
		}
		fmt.Println(ToJSON(res))
		return nil
	}

	// Pretty summary.
	fmt.Println("")
	printZKProgressBar(1.0)
	greenBold.Println("\n" + OK() + "Attestation generated")
	yellowBold.Printf("  Statement:    %s\n", attestation.Statement)
	yellowBold.Printf("  Mode:         %s\n", attestation.Mode)
	yellowBold.Printf("  Members:      %d\n", attestation.Count)
	yellowBold.Printf("  Public root:  %s\n", shortHex(attestation.PublicRoot))
	yellowBold.Printf("  Scope commit: %s\n", shortHex(attestation.ScopeCommit))
	yellowBold.Printf("  VKID:         %s\n", shortHex(attestation.VKID))
	yellowBold.Printf("  Proof size:   %d bytes\n", len(attestation.Proof))
	yellowBold.Printf("  VK size:      %d bytes\n", len(vkBytes))
	yellowBold.Printf("  Prove time:   %s\n", elapsed.Round(time.Millisecond))
	fmt.Println("")
	cyanBold.Printf("  Next: cafctl zk-demo verify %s %s\n", zkDemoOutputPath, zkDemoVKOutputPath)
	fmt.Println("")
	return nil
}

// zkVerifyResult is the machine-readable summary emitted with --json.
type zkVerifyResult struct {
	Valid       bool    `json:"valid"`
	Statement   string  `json:"statement"`
	Count       int     `json:"count"`
	PublicRoot  string  `json:"public_root"`
	ScopeCommit string  `json:"scope_commit"`
	VKID        string  `json:"vk_id"`
	Mode        string  `json:"mode"`
	ElapsedMS   float64 `json:"elapsed_ms"`
	Error       string  `json:"error,omitempty"`
}

func runZKDemoVerify(cmd *cobra.Command, args []string) error {
	proofPath := args[0]
	vkPath := args[1]

	if !zkDemoJSON {
		printZKBanner("VERIFY", "offline Groth16 verification (no prover, no secrets)")
		cyanBold.Printf("  Proof:  %s\n", proofPath)
		cyanBold.Printf("  VK:     %s\n\n", vkPath)
	}

	// Step 1: load the attestation JSON.
	zkStep(1, 3, "Reading attestation JSON")
	att, err := readAttestationJSON(proofPath)
	if err != nil {
		PrintError("Failed to read attestation: %v", err)
		return err
	}
	zkStepDone(1, 3, fmt.Sprintf("%d members, statement=%s", att.Count, att.Statement))

	// Step 2: load the verifying key bytes.
	zkStep(2, 3, "Reading verifying key")
	vkBytes, err := os.ReadFile(vkPath)
	if err != nil {
		PrintError("Failed to read verifying key: %v", err)
		return fmt.Errorf("read vk %q: %w", vkPath, err)
	}
	zkStepDone(2, 3, fmt.Sprintf("%d bytes", len(vkBytes)))

	// Step 3: verify offline and time it.
	zkStep(3, 3, "Verifying proof offline")
	start := time.Now()
	verr := zk.VerifyZK(att, vkBytes)
	elapsed := time.Since(start)

	if zkDemoJSON {
		res := zkVerifyResult{
			Valid:       verr == nil,
			Statement:   string(att.Statement),
			Count:       att.Count,
			PublicRoot:  att.PublicRoot,
			ScopeCommit: att.ScopeCommit,
			VKID:        att.VKID,
			Mode:        att.Mode,
			ElapsedMS:   float64(elapsed.Microseconds()) / 1000.0,
		}
		if verr != nil {
			res.Error = verr.Error()
		}
		fmt.Println(ToJSON(res))
		if verr != nil {
			return fmt.Errorf("verification failed: %w", verr)
		}
		return nil
	}

	if verr != nil {
		zkStepFail(3, 3, "invalid")
		fmt.Println("")
		redBold.Println(ERROR() + "Attestation INVALID")
		redBold.Printf("  Reason: %v\n", verr)
		fmt.Println("")
		return fmt.Errorf("verification failed: %w", verr)
	}
	zkStepDone(3, 3, fmt.Sprintf("valid in %s", elapsed.Round(time.Microsecond)))

	fmt.Println("")
	printZKProgressBar(1.0)
	greenBold.Println("\n" + OK() + "Attestation VERIFIED")
	yellowBold.Printf("  Statement:    %s\n", att.Statement)
	yellowBold.Printf("  Members:      %d\n", att.Count)
	yellowBold.Printf("  Public root:  %s\n", shortHex(att.PublicRoot))
	yellowBold.Printf("  Scope commit: %s\n", shortHex(att.ScopeCommit))
	yellowBold.Printf("  VKID:         %s\n", shortHex(att.VKID))
	yellowBold.Printf("  Verify time:  %s\n", elapsed.Round(time.Microsecond))
	greenBold.Println("  Status:       PROOF SOUND — verified without any secret input")
	fmt.Println("")
	return nil
}

// --- persistence helpers -------------------------------------------------

// writeAttestationJSON marshals the attestation to indented JSON and writes it,
// creating parent directories as needed.
func writeAttestationJSON(path string, att *zk.ZKAttestation) error {
	data, err := json.MarshalIndent(att, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal attestation: %w", err)
	}
	return writeBytesFile(path, data)
}

// readAttestationJSON reads and unmarshals an attestation JSON file.
func readAttestationJSON(path string) (*zk.ZKAttestation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read attestation %q: %w", path, err)
	}
	var att zk.ZKAttestation
	if err := json.Unmarshal(data, &att); err != nil {
		return nil, fmt.Errorf("parse attestation %q: %w", path, err)
	}
	return &att, nil
}

// writeBytesFile writes raw bytes to path, creating parent directories as needed.
func writeBytesFile(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %q: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// --- presentation helpers ------------------------------------------------

// printZKBanner prints a titled banner for a zk-demo phase.
func printZKBanner(phase, subtitle string) {
	fmt.Println("")
	blueBold.Println(Separator('═', 64))
	blueBold.Printf("  cafctl zk-demo · %s\n", phase)
	cyan.Printf("  %s\n", subtitle)
	blueBold.Println(Separator('═', 64))
	fmt.Println("")
}

// zkStep prints an in-progress step line.
func zkStep(i, total int, msg string) {
	cyan.Printf("  [%d/%d] %s...\n", i, total, msg)
}

// zkStepDone prints a completed step line.
func zkStepDone(i, total int, detail string) {
	green.Printf("  [%d/%d] %s %s\n", i, total, OK(), detail)
}

// zkStepFail prints a failed step line.
func zkStepFail(i, total int, detail string) {
	redBold.Printf("  [%d/%d] %s %s\n", i, total, ERROR(), detail)
}

// printZKProgressBar renders a filled progress bar for the given fraction [0,1].
func printZKProgressBar(fraction float64) {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	const width = 40
	filled := int(fraction * float64(width))
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	greenBold.Printf("  [%s] %3.0f%%\n", bar, fraction*100)
}

// shortHex abbreviates a long hex string for compact display.
func shortHex(s string) string {
	// Validate it is hex; if not, return as-is (defensive, never panics).
	if _, err := hex.DecodeString(s); err != nil {
		return s
	}
	if len(s) <= 20 {
		return s
	}
	return s[:12] + "…" + s[len(s)-8:]
}
