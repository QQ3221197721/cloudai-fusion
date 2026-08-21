// Package main - cafctl scan sbom subcommand (M34 Supply Chain).
//
// This command surfaces real, offline SBOM generation capabilities:
//
//   - scan sbom (M34, pkg/security) — generates a CycloneDX-format SBOM
//     using the real pkg/security.SupplyChainManager with embedded components.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Supply chain security scanning (SBOM/SARIF)",
	}
	cmd.AddCommand(newScanSBOMCmd())
	return cmd
}

func newScanSBOMCmd() *cobra.Command {
	var image, digest string
	cmd := &cobra.Command{
		Use:           "sbom [--image <ref>] [--digest <sha256:...>]",
		Short:         "Generate CycloneDX SBOM from container image metadata",
		Args:          cobra.NoArgs,
		Example:       "  cafctl scan sbom\n  cafctl scan sbom --image ghcr.io/app:v1 --digest sha256:abcd...",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if image == "" {
				image = "ghcr.io/cloudai-fusion/app:dev"
			}
			if digest == "" {
				digest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			}

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)

			mgr := security.NewSupplyChainManager(security.SupplyChainConfig{Logger: logger})

			sbom := mgr.GenerateSBOM(image, digest)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl scan sbom · software bill of materials")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Generated SBOM:\n", OK())
			fmt.Fprintln(out, "  Image Reference:", sbom.ImageRef)
			fmt.Fprintln(out, "  Digest:", sbom.Digest)
			fmt.Fprintln(out, "  Format:", sbom.Format)
			fmt.Fprintln(out, "  Total Packages:", sbom.TotalPkgs)
			fmt.Fprintln(out, "  Generated At:", sbom.GeneratedAt.UTC().Format("2006-01-02 15:04:05 UTC"))
			fmt.Fprintln(out, "  Generator:", sbom.GeneratedBy)
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Components:")
			for i, comp := range sbom.Components {
				fmt.Fprintf(out, "  #%d %s@%s [%s/%s]\n", i+1, comp.Name, comp.Version, comp.Type, comp.Ecosystem)
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Licenses Detected:")
			for _, lic := range sbom.Licenses {
				fmt.Fprintf(out, "    • %s\n", lic)
			}
			fmt.Fprintln(out, "")

			if outputJSON, _ := cmd.Flags().GetBool("json"); outputJSON {
				jsonBytes, _ := json.MarshalIndent(sbom, "", "  ")
				fmt.Fprintf(out, "\n%s\n", string(jsonBytes))
			}

			fmt.Fprintf(out, "%s SBOM generated successfully.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVarP(&image, "image", "i", "", "Container image reference")
	cmd.Flags().StringVarP(&digest, "digest", "d", "", "Image digest (SHA256)")
	cmd.Flags().Bool("json", false, "Output as JSON")
	return cmd
}
