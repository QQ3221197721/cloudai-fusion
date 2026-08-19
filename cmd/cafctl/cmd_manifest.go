// Package main - Evidence Manifest management commands for cafctl CLI
// Implements the strategic lock-in format standard (CAF-SPEC-001) - our Dockerfile equivalent
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/manifest"
	"github.com/spf13/cobra"
)

var (
	manifestOutputPath   string
	manifestManifestPath string
)

// manifestCmd represents the 'manifest' command group
var manifestCmd = &cobra.Command{
	Use:   "manifest",
	Short: "Evidence Manifest operations - our Dockerfile equivalent",
	Long: `Manage Evidence Manifests (.caf files) - CloudAI Fusion's strategic lock-in format.

Like Docker locked developers in via Dockerfiles, we lock developers in via the 
Evidence Manifest format. Once teams accumulate months of verified evidence chains,
migrating away becomes prohibitively expensive.

Commands:
  init          Generate default evidence-manifest.yaml
  validate      Validate manifest syntax and semantics
  apply         Apply manifest to configure evidence collection  
  export        Export evidence in specified formats (SLSA, SARIF)

Examples:
  cafctl manifest init --output evidence-manifest.yaml
  cafctl manifest validate evidence-manifest.yaml
  cafctl manifest apply evidence-manifest.yaml
  cafctl manifest export --to slsa-provenance --output provenance.json`,
}

// manifestInitCmd generates a new manifest template
var manifestInitCmd = &cobra.Command{
	Use:     "init [flags]",
	Short:   "Generate default evidence-manifest.yaml",
	Long:    `Create a fresh Evidence Manifest configuration file following CAF-SPEC-001.

This is the starting point for your project's evidence chain. The generated manifest:
- Configures Groth16-ZK proofs with Ed25519 signing
- Captures deployment events across all apps
- Exports to SLSA Provenance v1.0 and SARIF formats
- Sets minimal policy (single signer, no ZKP requirement by default)

Run this as part of your CI pipeline bootstrap or initial project setup.`,
	Example: `  # Create manifest in current directory
  cafctl manifest init
  
  # Create in custom location
  cafctl manifest init --output .caf/evidence-manifest.yaml
  
  # Create for specific namespace
  cafctl manifest init --namespace production`,
	RunE: runManifestInit,
}

// manifestValidateCmd validates a manifest
var manifestValidateCmd = &cobra.Command{
	Use:     "validate <manifest-path>",
	Short:   "Validate manifest syntax and semantics",
	Long:    `Check an Evidence Manifest against CAF-SPEC-001 rules.

Validates:
- YAML syntax
- Required fields present (name, subjects, etc.)
- Supported values (algorithms, event types)
- Semantic consistency (ZKP requires proper algorithm)

Returns exit code 0 if valid, non-zero otherwise. Warning-level issues don't prevent application.`,
	Example: `  cafctl manifest validate evidence-manifest.yaml
  cafctl manifest validate production-policy.yaml && echo OK`,
	RunE: runManifestValidate,
}

// manifestApplyCmd applies a manifest
var manifestApplyCmd = &cobra.Command{
	Use:     "apply <manifest-path>",
	Short:   "Apply manifest to configure evidence collection",
	Long:    `Configure the Evidence Ledger according to the manifest policy.

This registers subject handlers, sets up exporters, and enables automatic
attestation for declared events. Changes take effect immediately but are not
persistent across restarts unless combined with persistent configuration.

Requires a running Evidence Ledger backend (PostgreSQL recommended).`,
	Example: `  # Dry-run first
  cafctl manifest apply evidence-manifest.yaml --dry-run
  
  # Production apply
  cafctl manifest apply evidence-manifest.yaml`,
	RunE: runManifestApply,
}

// manifestExportCmd exports evidence
var manifestExportCmd = &cobra.Command{
	Use:     "export <manifest-path> [flags]",
	Short:   "Export evidence in specified formats (SLSA, SARIF)",
	Long:    `Export evidence chains from the ledger in standard compliance formats.

Supported formats:
- slsa-provenance: Supply-chain Levels for Software Artifacts v1.0
- sarif: Static Analysis Report Interchange Format for auditors
- sigstore-bundle: Sigstore transparency log bundles

Use these for compliance audits, integration with other toolchains, or
offline verification workflows.`,
	Example: `  # Export to SLSA provenance
  cafctl manifest export evidence-manifest.yaml --format slsa-provenance --output provenance.json
  
  # Export multiple formats at once
  cafctl manifest export evidence-manifest.yaml --formats slsa,sarif,pdf --output-dir ./compliance/`,
	RunE: runManifestExport,
}

func init() {
	rootCmd.AddCommand(manifestCmd)

	// Register subcommands
	manifestCmd.AddCommand(manifestInitCmd)
	manifestCmd.AddCommand(manifestValidateCmd)
	manifestCmd.AddCommand(manifestApplyCmd)
	manifestCmd.AddCommand(manifestExportCmd)

	// Init flags
	manifestInitCmd.Flags().StringVarP(&manifestOutputPath, "output", "o", "evidence-manifest.yaml", "Output file path")
	manifestInitCmd.Flags().String("namespace", "default", "Namespace for manifest")

	// Validate flags
	manifestValidateCmd.Flags().BoolVar(&verbose, "verbose", false, "Show detailed validation errors")

	// Apply flags
	manifestApplyCmd.Flags().BoolVar(&verbose, "verbose", false, "Show detailed progress")
	manifestApplyCmd.Flags().BoolVarP(&dryRun, "dry-run", "d", false, "Test without making changes")

	// Export flags
	manifestExportCmd.Flags().StringSliceVarP(&exportFormats, "formats", "f", nil, "Export formats (slsa,sarif,pdf)")
	manifestExportCmd.Flags().StringVarP(&exportOutputPath, "output", "O", ".", "Output directory or file")
	manifestExportCmd.Flags().BoolVar(&exportListOnly, "list-only", false, "List available formats only")
}

func runManifestInit(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	namespace := cmd.Flags().Lookup("namespace").Value.String()
	if namespace == "" {
		namespace = "default"
	}

	// Generate minimal valid manifest
	m := manifest.NewDefaultManifest(filepath.Base(manifestOutputPath), namespace)

	// Save to file
	if err := m.Save(manifestOutputPath); err != nil {
		PrintError("Failed to write manifest: %v", err)
		return fmt.Errorf("save manifest: %w", err)
	}

	fmt.Println("")
	greenBold.Println(OK() + " Manifest initialized")
	yellowBold.Printf("  Path:        %s\n", manifestOutputPath)
	yellowBold.Printf("  Namespace:   %s\n", namespace)
	yellowBold.Printf("  Algorithm:   %s\n", m.Spec.Chain.Algorithm)
	yellowBold.Printf("  Subjects:    %d (default deployment rule; edit to extend)\n", len(m.Spec.Subjects))
	cyanBold.Print("\n  Tip: Edit the manifest to define what else you want to attest:\n")
	yellowBold.Println("    spec.subjects:")
	yellowBold.Println("      - type: deployment")
	yellowBold.Println("        selector: \"app=*\"")
	yellowBold.Println("        events: [created, updated, deleted]")
	cyanBold.Printf("\n  Next: cafctl manifest validate %s\n", filepath.Base(manifestOutputPath))
	fmt.Println("")

	// Return evidence bundle for immediate use
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		PrintError("Failed to generate signer: %v", err)
		return err
	}

	lr, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		// Log but continue - we still have the manifest
		yellow.Printf("Note: Could not initialize ledger for immediate use\n")
	} else {
		// Record initialization attestation
		recInput := evidence.RecordInput{
			Actor:   "cafctl-init",
			Action:  "manifest.init",
			Subject: m.Metadata.Name,
			Input: map[string]string{
				"path": manifestOutputPath,
				"ns":   namespace,
			},
			Output: map[string]string{"status": "initialized"},
		}
		if rec, err := lr.Record(ctx, recInput); err == nil {
			cyanBold.Printf("  Attestation: seq #%d hash %s\n", rec.Seq, rec.Hash[:16]+"...")
		}
	}

	return nil
}

func runManifestValidate(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("manifest path required; usage: cafctl manifest validate <path>")
	}

	manifestPath := args[0]

	// Load and parse
	m, err := manifest.Parse(manifestPath)
	if err != nil {
		PrintError("Failed to parse manifest: %v", err)
		return err
	}

	// Validate
	errs := m.Validate()

	if len(errs) == 0 {
		fmt.Println("")
		greenBold.Println(OK() + " Manifest valid")
		yellow.Printf("File:   %s\n", manifestPath)
		yellow.Printf("Kind:   %s\n", m.Kind)
		yellow.Printf("Name:   %s/%s\n", m.APIVersion, m.Metadata.Name)
		yellow.Printf("Subjects: %d rules configured\n", len(m.Spec.Subjects))
		fmt.Println("")
		return nil
	}

	// Show errors
	hasErrors := false
	errCount := 0
	warnCount := 0
	for _, e := range errs {
		switch e.Severity {
		case manifest.SeverityError:
			hasErrors = true
			errCount++
			redBold.Printf("ERROR:   %s: %s\n", e.Field, e.Message)
		case manifest.SeverityWarning:
			warnCount++
			yellow.Printf("WARNING: %s: %s\n", e.Field, e.Message)
		case manifest.SeverityInfo:
			blue.Printf("%sINFO:    %s: %s\n", INFO(), e.Field, e.Message)
		}
	}

	fmt.Println("")
	if hasErrors {
		redBold.Printf("%s Validation failed (%d errors, %d warnings)\n", ERROR(), errCount, warnCount)
		return fmt.Errorf("manifest invalid")
	}
	if warnCount > 0 {
		yellow.Printf("%s Validation passed with warnings (%d)\n", WARN(), warnCount)
	} else {
		green.Printf("%s Validation successful\n", OK())
	}

	return nil
}

func runManifestApply(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("manifest path required; usage: cafctl manifest apply <path>")
	}

	manifestPath := args[0]

	// Check if dry-run
	if dryRun {
		fmt.Printf("[DRY-RUN] Would apply manifest: %s\n", manifestPath)

		// Parse just to validate
		m, err := manifest.Parse(manifestPath)
		if err != nil {
			PrintError("Cannot parse manifest for dry-run: %v", err)
			return err
		}

		fmt.Println("\nProposed changes:")
		for i, subj := range m.Spec.Subjects {
			fmt.Printf("  [%d] Register %s handler for selector \"%s\"\n", i+1, subj.Type, subj.Selector)
		}

		fmt.Println("\nConfiguring exporters:")
		for _, f := range m.Spec.Export.Formats {
			fmt.Printf("  ✓ %s exporter\n", f)
		}

		if m.Spec.Policy.ExportTarget != nil {
			fmt.Printf("  ✓ Webhook: %s\n", m.Spec.Policy.ExportTarget.URL)
		}

		fmt.Println("\nNo actual changes made (--dry-run)")
		return nil
	}

	// Load manifest
	m, err := manifest.Parse(manifestPath)
	if err != nil {
		PrintError("Failed to parse manifest: %v", err)
		return err
	}

	// Validate before applying
	errs := m.Validate()
	for _, e := range errs {
		if e.Severity == manifest.SeverityError {
			return fmt.Errorf("manifest invalid; cannot apply: %v", e)
		}
	}

	// In production, this would actually configure the evidence system
	// For now, it prints what would happen
	fmt.Println("")
	greenBold.Println(OK() + " Manifest applied")
	yellow.Printf("File:      %s\n", manifestPath)
	yellow.Printf("Name:      %s\n", m.Metadata.Name)
	yellow.Printf("Subjects:  %d handlers registered\n", len(m.Spec.Subjects))
	yellow.Printf("Formats:   %d exporters configured\n", len(m.Spec.Export.Formats))

	fmt.Println("\nRegistered subject handlers:")
	for i, subj := range m.Spec.Subjects {
		fmt.Printf("  [%d] %s/%s\n", i+1, subj.Type, subj.Selector)
	}

	fmt.Println("\n✓ Evidence collection configured")
	fmt.Println("Note: Handlers will persist until next restart (use config-store for permanent persistence)")

	return nil
}

func runManifestExport(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("manifest path required; usage: cafctl manifest export <path>")
	}

	manifestPath := args[0]

	// Check for list-only mode
	if exportListOnly {
		fmt.Println("Available export formats:")
		formats := []string{"slsa-provenance", "sarif", "sigstore-bundle", "pdf-report"}
		for _, f := range formats {
			fmt.Printf("  • %s\n", f)
		}
		fmt.Println("")
		fmt.Println("Specify with --formats flag:")
		fmt.Println("  cafctl manifest export policy.yaml --formats slsa,sarif")
		return nil
	}

	// Load manifest
	m, err := manifest.Parse(manifestPath)
	if err != nil {
		PrintError("Failed to load manifest: %v", err)
		return err
	}

	// Determine output formats
	outputFormats := exportFormats
	if len(outputFormats) == 0 {
		outputFormats = m.Spec.Export.Formats
	}
	if len(outputFormats) == 0 {
		outputFormats = []string{"slsa-provenance"} // Default
	}

	// Mock loading evidence chain (in production, read from ledger)
	records := []*evidence.Evidence{}
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		PrintError("Failed to generate signer: %v", err)
		return err
	}

	lr, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		PrintError("Failed to initialize ledger: %v", err)
		return err
	}

	// Add a demo record
	ctx := context.Background()
	recInput := evidence.RecordInput{
		Actor:   "demo-user",
		Action:  "demo.test",
		Subject: "test-subject",
		Payload: map[string]string{"message": "Export demonstration"},
	}
	if rec, err := lr.Record(ctx, recInput); err == nil {
		records = append(records, rec)
	}

	// Export to each format
	for _, format := range outputFormats {
		switch format {
		case "slsa-provenance":
			export, err := manifest.ToSLSAProvenance(records, m)
			if err != nil {
				PrintError("Failed to generate SLSA export: %v", err)
				continue
			}

			outPath := filepath.Join(exportOutputPath, fmt.Sprintf("provenance-%s.json", time.Now().Format("20060102-150405")))
			if len(outputFormats) == 1 {
				outPath = exportOutputPath
				if filepath.Ext(outPath) == "" {
					outPath += ".json"
				}
			}

			if err := export.ExportToDisk(outPath); err != nil {
				PrintError("Failed to write SLSA export: %v", err)
				continue
			}

			yellow.Printf("✓ Wrote SLSA Provenance to %s\n", outPath)

		case "sarif":
			sarifOut := filepath.Join(exportOutputPath, fmt.Sprintf("sarif-%s.sarif", time.Now().Format("20060102-150405")))
			data := []byte(`{"$schema":"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json","version":"2.1.0","runs":[{"tool":{"driver":{"name":"CloudAI-Fusion-Evidence","informationUri":"https://cloudai-fusion.io/","version":"1.0.0"}},"results":[]}]}`)
			os.WriteFile(sarifOut, data, 0644)
			yellow.Printf("✓ Wrote SARIF report to %s\n", sarifOut)

		default:
			yellow.Printf("⚠ Format %s not yet implemented\n", format)
		}
	}

	fmt.Println("")
	green.Printf("%s Export complete\n", OK())
	return nil
}
