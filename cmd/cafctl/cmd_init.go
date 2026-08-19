// Package main - Project initialization command for cafctl CLI
package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
	"github.com/spf13/cobra"
)

var (
	initDir       string
	initForce     bool
	initConfigDir string
	initMode      string // explicitly set run mode
	initYes       bool  // skip prompts, accept recommended values
)

// initCmd represents the 'init' command
var initCmd = &cobra.Command{
	Use:   "init [--dir PATH]",
	Short: "Initialize a CloudAI Fusion project",
	Long: `Initialize a new CloudAI Fusion project directory with all necessary configuration files:
• .caf/ evidence chain storage
• .caf/config.yaml for settings
• Initial signing key pair
• Evidence chain genesis record

This is your "git init" or "docker init" moment — sets up the foundational infrastructure
for tamper-evident, verifiable control plane operations.`,
	Example: `  # Initialize in current directory (interactive)
  cafctl init
  
  # Initialize in specific directory
  cafctl init --dir /path/to/myproject
  
  # Force overwrite if exists
  cafctl init --force
  
  # Auto-detect environment, accept recommendations (--yes)
  cafctl init --yes
  
  # Explicitly choose run mode (simulation / degraded / production)
  cafctl init --mode degraded`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	
	initCmd.Flags().StringVarP(&initDir, "dir", "d", ".", 
		"Target directory to initialize")
	initCmd.Flags().BoolVarP(&initForce, "force", "", false, 
		"Overwrite existing .caf directory if present")
	// New flags for interactive wizard
	initCmd.Flags().BoolVar(&initYes, "yes", false,
		"Auto-detect environment and accept recommended configuration")
	initCmd.Flags().StringVar(&initMode, "mode", "",
		"Run mode: simulation | degraded | production")
}

func runInit(cmd *cobra.Command, args []string) error {
	targetDir := filepath.Clean(initDir)
	
	// Scan environment first (pure function, testable)
	envReport := scanEnvironment()
	
	// Display initial capability panel
	fmt.Println("")
	cyanBold.Println("☕ CloudAI Fusion Initialization Wizard")
	fmt.Println(Separator('─', 64))
	fmt.Println("")
	
	if !initYes {
		PrintInfo("🔍 Detecting local capabilities...")
	}
	
	printCapabilityPanel(envReport)
	fmt.Println("")
	
	// Decide on run mode
	runMode := determineRunMode(envReport, initMode, initYes)
	
	fmt.Printf("\u2705 Selected run mode: %s\n", strings.ToUpper(runMode))
	fmt.Println("")
	CAFPath := filepath.Join(targetDir, ".caf")
	if _, err := os.Stat(CAFPath); err == nil {
		if !initForce {
			PrintError("Project already initialized at %s", CAFPath)
			PrintWarning("Use --force to overwrite existing configuration")
			return fmt.Errorf("already initialized")
		}
		PrintInfo("Removing existing .caf directory...")
		if err := os.RemoveAll(CAFPath); err != nil {
			PrintError("Failed to remove existing .caf: %v", err)
			return err
		}
	}
	
	// Create directory structure
	CAFDirs := []string{
		filepath.Join(CAFPath, "keys"),
		filepath.Join(CAFPath, "chains"),
		filepath.Join(CAFPath, "exports"),
	}
	
	for _, dir := range CAFDirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			PrintError("Failed to create directory %s: %v", dir, err)
			return err
		}
	}
	
	PrintSuccess(OK() + " Created .caf directory structure")
	
	// Generate signing key pair
	greenBold.Printf("Generating Ed25519 signing key pair...\n")
	keyPair, err := generateSigningKeyPair(CAFPath)
	if err != nil {
		PrintError("Failed to generate signing keys: %v", err)
		return err
	}
	
	yellowBold.Printf("  Public key saved: .caf/public.pem\n")
	greenBold.Printf("  Key ID:      %s\n", keyPair.KeyID)
	yellowBold.Printf("  Private key saved: .caf/keys/private.pem (used by 'cafctl attest')\n")
	
	// Initialize evidence ledger
	ctx := context.Background()
	store := evidence.NewMemoryStore()
	
	signer := keyPair.Signer
	
	lr, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		PrintError("Failed to initialize ledger: %v", err)
		return err
	}
	
	// Create genesis attestation
	genesisAttestation := fmt.Sprintf("CloudAI Fusion project initialized at %s", time.Now().UTC().Format(time.RFC3339))
	
	genesisInput := evidence.RecordInput{
		Actor:   "cafctl init",
		Action:  "system.init",
		Subject: targetDir,
		Input: map[string]string{
			"path":    targetDir,
			"version": rootCmd.Version,
		},
		Output:  map[string]string{"status": "initialized"},
		Payload: map[string]string{"statement": genesisAttestation},
	}
	
	genesisRecord, err := lr.Record(ctx, genesisInput)
	if err != nil {
		PrintError("Failed to record genesis evidence: %v", err)
		return err
	}
	
	// Export initial chain to the canonical location that 'cafctl verify' reads by default.
	chainPath := filepath.Join(CAFPath, "evidence.chain")
	bundle, err := exportBundle(lr, CAFPath+"/public.pem")
	if err != nil {
		PrintWarning("Could not export bundle: %v", err)
	} else {
		if err := evidence.WriteBundleFile(chainPath, bundle); err == nil {
			greenBold.Println(OK() + " Initialized evidence chain")
			yellowBold.Printf("  Genesis hash:  %s\n", genesisRecord.Hash[:16]+"...")
			yellowBold.Printf("  Chain file:    %s\n", chainPath)
			
			// Verify it worked
			readBundle, err := evidence.ReadBundleFile(chainPath)
			if err == nil && len(readBundle.Records) > 0 {
				verifiedReport, _ := evidence.VerifyBundle(readBundle)
				if verifiedReport.Valid {
					greenBold.Println(OK() + " Genesis chain verified successfully")
				} else {
					PrintWarning("Chain verification failed (but was written)")
				}
			}
		}
	}
	
	// Create default config
	configPath := filepath.Join(CAFPath, "config.yaml")
	configContent := getDefaultConfig(targetDir, keyPair.PublicKeyPEM, runMode)
	if err := os.WriteFile(configPath, configContent, 0o644); err != nil {
		PrintWarning("Could not write config file: %v", err)
	} else {
		yellowBold.Printf("Config file:   %s\n", configPath)
	}
	
	// Save public key for sharing
	pubKeyPath := filepath.Join(targetDir, ".caf", "public.pem")
	if err := os.WriteFile(pubKeyPath, keyPair.PublicKeyPEM, 0o644); err == nil {
		yellowBold.Printf("Public key:    %s (share this for verification)\n", pubKeyPath)
	}
	
	fmt.Println("")
	
	// Scaffold the zero-dependency local startup files: run manifest + quickstart.
	// These are what turn 'init' into a runnable project rather than just a key store.
	scaffoldResults, scaffoldErr := writeQuickstartScaffold(targetDir, quickstartScaffoldOptions{
		Port:    defaultLocalPort,
		RunMode: runMode,
		Force:   initForce,
	})
	if scaffoldErr != nil {
		PrintWarning("Could not write local startup scaffold: %v", scaffoldErr)
	} else {
		greenBold.Println(OK() + " Wrote local startup scaffold")
		for _, r := range scaffoldResults {
			yellowBold.Printf("  %-22s %s — %s\n", r.Path, r.Action, r.Desc)
		}
	}
	
	fmt.Println("")
	greenBold.Println(OK() + " CloudAI Fusion project initialized successfully!")
	fmt.Println("")
	
	// Run-mode-specific guidance so the user knows exactly what they are running.
	switch runMode {
	case "production":
		greenBold.Println("🟢 RUN MODE: PRODUCTION — simulated backends are FORBIDDEN.")
		yellowBold.Println("   The apiserver will refuse to boot if any subsystem is simulated.")
		yellowBold.Println("   Ensure real DB / messaging / cluster are configured before starting.")
	case "degraded":
		yellowBold.Println("🟡 RUN MODE: DEGRADED — real backends preferred, simulated ones surfaced loudly.")
	default:
		yellowBold.Println("⚠️  RUN MODE: SIMULATION — subsystems may run on in-memory fallbacks (dev only).")
		yellowBold.Println("   Do NOT use simulation mode for real workloads.")
	}
	fmt.Println("")
	yellowBold.Println("Next steps (no credentials, no Docker, no cluster required):")
	yellowBold.Println("  1. Start it:  cafctl up --local          (zero-dependency local plane)")
	yellowBold.Println("  2. Check it:  cafctl doctor              (environment self-check with fixes)")
	yellowBold.Println("  3. Inspect:   cafctl status              (real vs simulated subsystems)")
	yellowBold.Println("  4. Attest:    cafctl attest              (record events into the evidence chain)")
	yellowBold.Println("  5. Verify:    cafctl verify              (offline chain integrity check)")
	yellowBold.Printf("  Read %s for a copy-paste walkthrough.\n", filepath.Join(targetDir, "QUICKSTART.md"))
	fmt.Println("")
	
	return nil
}

type KeyPair struct {
	Signer         evidence.Signer
	KeyID          string
	PublicKeyPEM   []byte
}

func generateSigningKeyPair(cafPath string) (*KeyPair, error) {
	// Generate a random 32-byte seed and build a deterministic, stable signer.
	// Persisting the seed-derived private key lets subsequent 'cafctl attest'
	// commands sign with the SAME identity so the chain verifies as single-signer.
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate seed: %w", err)
	}
	signer, err := evidence.NewSignerFromSeed(seed)
	if err != nil {
		return nil, fmt.Errorf("create signer: %w", err)
	}

	// Marshal public key material.
	pubPEM, err := evidence.MarshalPublicKeyPEM(signer.PublicKey())
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}

	// Persist the private key (PKCS#8 PEM) to .caf/keys/private.pem (0600).
	priv := ed25519.NewKeyFromSeed(seed)
	privPEM, err := evidence.MarshalPrivateKeyPEM(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	privKeyPath := filepath.Join(cafPath, "keys", "private.pem")
	if err := os.WriteFile(filepath.Clean(privKeyPath), privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("write private key: %w", err)
	}

	// Save public key to disk for sharing with auditors.
	pubKeyPath := filepath.Join(cafPath, "public.pem")
	if err := os.WriteFile(filepath.Clean(pubKeyPath), pubPEM, 0o644); err != nil {
		return nil, fmt.Errorf("write public key: %w", err)
	}

	return &KeyPair{
		Signer:       signer,
		KeyID:        signer.KeyID(),
		PublicKeyPEM: pubPEM,
	}, nil
}

func exportBundle(lr *evidence.Ledger, pubKeyPath string) (*evidence.ExportBundle, error) {
	ctx := context.Background()
	bundle, err := lr.Export(ctx)
	if err != nil {
		return nil, err
	}
	// Add the public key path for reference
	bundle.PublicKeyPEM = "// Load with: cafctl verify --key-file <pubkey>"
	return bundle, nil
}

func getDefaultConfig(projectDir string, pubKey []byte, runMode string) []byte {
	return []byte(`# CloudAI Fusion Configuration
# Generated by cafctl init on ` + time.Now().UTC().Format(time.RFC3339) + `

project:
  name: CloudAI Fusion
  version: ` + rootCmd.Version + `
  initialized_at: ` + time.Now().UTC().Format(time.RFC3339Nano) + `
  target_dir: ` + projectDir + `

# run_mode governs whether simulated/in-memory backends are allowed:
#   simulation - all fallbacks allowed (dev/tests)
#   degraded   - real preferred, simulated surfaced loudly (staging)
#   production - simulated backends FORBIDDEN; apiserver fails fast at boot
run_mode: ` + runMode + `

evidence:
  enabled: true
  chain_directory: .caf/chains
  max_history_entries: 100000
  auto_verify: true

signing:
  algorithm: ed25519
  key_rotation_days: 90
  public_key_path: .caf/public.pem
  private_key_path: .caf/keys/private.pem

security:
  require_signature: true
  verify_before_deploy: true
  log_all_attestations: true
`)
}

// ============================================================================
// Init wizard helpers: environment panel + run-mode selection
// ============================================================================

// printCapabilityPanel renders the detected local environment. It stays readable
// on a no-color terminal by using textual [ OK ]/[SIM] markers, not color alone.
func printCapabilityPanel(report EnvReport) {
	fmt.Println("  Local environment scan:")
	for _, c := range report.Capabilities() {
		marker := "[SIM ]"
		label := "simulated"
		colorFn := yellow
		if c.Available {
			marker = "[REAL]"
			label = "real"
			colorFn = green
		}
		colorFn.Printf("    %s %-11s %s\n", marker, c.Name, label)
		if c.Available {
			if c.Detail != "" {
				defaultColor.Printf("           %s\n", c.Detail)
			}
		} else if c.Hint != "" {
			defaultColor.Printf("           → %s\n", c.Hint)
		}
	}
	fmt.Printf("  %d/%d real backends detected.\n", report.RealBackendCount(), len(report.Capabilities()))
}

// normalizeRunMode validates a user/flag-supplied run mode. It returns the
// canonical value and whether the input was a recognized mode.
func normalizeRunMode(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "simulation", "sim", "dev", "development":
		return "simulation", true
	case "degraded", "staging":
		return "degraded", true
	case "production", "prod":
		return "production", true
	default:
		return "", false
	}
}

// determineRunMode resolves the effective run mode from an explicit flag, the
// --yes shortcut, or an interactive prompt. When an explicit mode is invalid we
// warn and fall back to the recommendation rather than guessing silently.
func determineRunMode(report EnvReport, explicit string, yes bool) string {
	recommended := report.RecommendedRunMode()
	if explicit != "" {
		if mode, ok := normalizeRunMode(explicit); ok {
			return mode
		}
		PrintWarning("Unrecognized --mode %q; falling back to recommended %q", explicit, recommended)
		return recommended
	}
	if yes {
		return recommended
	}
	return promptRunMode(os.Stdin, recommended)
}

// promptRunMode asks the user to confirm/override the recommended run mode. It
// reads a single line; empty input (or a closed/non-interactive stdin) accepts
// the recommendation, so piping the command still works deterministically.
func promptRunMode(in *os.File, recommended string) string {
	yellow.Printf("Choose run mode [simulation/degraded/production] (default: %s): ", recommended)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		// EOF / non-interactive: accept the recommended default.
		fmt.Println(recommended)
		return recommended
	}
	choice := strings.TrimSpace(line)
	if choice == "" {
		return recommended
	}
	if mode, ok := normalizeRunMode(choice); ok {
		return mode
	}
	PrintWarning("Unrecognized mode %q; using recommended %q", choice, recommended)
	return recommended
}

// ensure the runmode package stays imported even if only used for docs/parity.
var _ = runmode.Simulation

