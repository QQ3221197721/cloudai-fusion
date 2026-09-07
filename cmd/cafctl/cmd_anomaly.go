// Package main - cafctl anomaly subcommands (M31 UEBA Anomaly Detection).
//
// These commands extend the existing anomaly command with additional operations:
//
//   - anomaly search (M31) — searches anomalies above minimum score threshold
//     using deterministic demo data.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/anomaly"
	"github.com/spf13/cobra"
)



// newAnomalyListCmd implements `cafctl anomaly list`
func newAnomalyListCmd() *cobra.Command {
	var dimension int
	var samples int
	var threshold float64

	cmd := &cobra.Command{
		Use:   "list [--dimension <d>] [--samples <n>] [--threshold <p>]",
		Short: "List detected anomalies from streaming detector",
		Args:  cobra.NoArgs,
		Example: `  cafctl anomaly list
  cafctl anomaly list --dimension 8 --samples 100 --threshold 0.975`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			detector := anomaly.NewStreamingDetector(dimension, threshold)

			rng := rand.New(rand.NewSource(12345))
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl anomaly list · UEBA anomaly detection (M31)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Generate samples and detect anomalies
			norm := make([]float64, dimension)
			anomalies := make([]struct {
				index int
				score float64
			}, 0)

			for i := 0; i < samples; i++ {
				for j := range norm {
					norm[j] = rng.NormFloat64()
				}
				score, anom := detector.Observe(norm)
				if anom {
					anomalies = append(anomalies, struct {
						index int
						score float64
					}{index: i + 1, score: score})
				}
			}

			fmt.Fprintln(out, "Detection Configuration:")
			fmt.Fprintf(out, "  Algorithm: Streaming Mahalanobis Distance\n")
			fmt.Fprintf(out, "  Dimensionality: %d features\n", dimension)
			fmt.Fprintf(out, "  Samples Processed: %d\n", samples)
			fmt.Fprintf(out, "  Threshold: %.1f%% confidence\n", threshold*100)
			fmt.Fprintln(out, "")

			if len(anomalies) == 0 {
				fmt.Fprintln(out, "Detected Anomalies: None found")
				fmt.Fprintln(out, "")
			} else {
				// Sort by score descending
				sort.Slice(anomalies, func(i, j int) bool {
					return anomalies[i].score > anomalies[j].score
				})

				fmt.Fprintf(out, "Detected Anomalies (%d total):\n", len(anomalies))
				for _, a := range anomalies {
					symbol := cyan.Sprint("🔵")
					if a.score >= 6.0 {
						symbol = redBold.Sprint("✗ CRITICAL")
					} else if a.score >= 4.5 {
						symbol = yellowBold.Sprint("⚠ HIGH")
					}
					fmt.Fprintf(out, "  %-12s sample #%d  score=%.3f\n", symbol, a.index, a.score)
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintf(out, "%s List complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().IntVar(&dimension, "dimension", 8, "Number of feature dimensions")
	cmd.Flags().IntVar(&samples, "samples", 50, "Number of samples to process")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.975, "Confidence threshold (0.0-1.0)")
	return cmd
}

// newAnomalySearchCmd implements `cafctl anomaly search`
func newAnomalySearchCmd() *cobra.Command {
	var dimension int
	var samples int
	var threshold float64
	var minScore float64

	cmd := &cobra.Command{
		Use:   "search [--dimension <d>] [--samples <n>] [--threshold <p>] --min-score <s>",
		Short: "Search anomalies above minimum score threshold",
		Args:  cobra.NoArgs,
		Example: `  cafctl anomaly search --min-score 4.0
  cafctl anomaly search --dimension 8 --samples 200 --min-score 5.0`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			detector := anomaly.NewStreamingDetector(dimension, threshold)

			rng := rand.New(rand.NewSource(12345))
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl anomaly search · anomaly filtering (M31)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Generate samples and detect anomalies
			norm := make([]float64, dimension)
			matchedAnomalies := make([]struct {
				index int
				score float64
			}, 0)

			for i := 0; i < samples; i++ {
				for j := range norm {
					norm[j] = rng.NormFloat64()
				}
				score, _ := detector.Observe(norm)
				if score >= minScore {
					matchedAnomalies = append(matchedAnomalies, struct {
						index int
						score float64
					}{index: i + 1, score: score})
				}
			}

			fmt.Fprintln(out, "Search Configuration:")
			fmt.Fprintf(out, "  Minimum Score Threshold: %.2f\n", minScore)
			fmt.Fprintf(out, "  Dimensionality: %d features\n", dimension)
			fmt.Fprintf(out, "  Samples Scanned: %d\n", samples)
			fmt.Fprintln(out, "")

			if len(matchedAnomalies) == 0 {
				fmt.Fprintln(out, "Results: No anomalies above threshold")
				fmt.Fprintln(out, "")
			} else {
				// Sort by score descending
				sort.Slice(matchedAnomalies, func(i, j int) bool {
					return matchedAnomalies[i].score > matchedAnomalies[j].score
				})

				fmt.Fprintf(out, "Matched Anomalies (%d found):\n", len(matchedAnomalies))
				for _, a := range matchedAnomalies {
					level := ""
					if a.score >= 6.0 {
						level = redBold.Sprint("CRITICAL")
					} else if a.score >= 4.5 {
						level = yellowBold.Sprint("HIGH")
					} else {
						level = cyan.Sprint("MEDIUM")
					}
					fmt.Fprintf(out, "  [%s] sample #%d  score=%.3f\n", level, a.index, a.score)
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintf(out, "%s Search complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().IntVar(&dimension, "dimension", 8, "Number of feature dimensions")
	cmd.Flags().IntVar(&samples, "samples", 50, "Number of samples to scan")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.975, "Confidence threshold (0.0-1.0)")
	cmd.Flags().Float64Var(&minScore, "min-score", 3.0, "Minimum score threshold for matching")
	return cmd
}

// newAnomalyDeleteCmd implements `cafctl anomaly delete` (stub with info message)
func newAnomalyDeleteCmd() *cobra.Command {
	var clearAll bool

	cmd := &cobra.Command{
		Use:   "delete (--all | --id <id>)",
		Short: "Delete detected anomalies (simulation mode)",
		Args:  cobra.NoArgs,
		Example: `  cafctl anomaly delete --all
  cafctl anomaly delete --id 123`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl anomaly delete · anomaly cleanup (M31)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			if clearAll {
				fmt.Fprintln(out, "Deletion Mode: Clear all anomalies")
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "[SIMULATION] This command would clear all detected anomalies.")
				fmt.Fprintln(out, "In production, this would delete anomaly records from persistent storage.")
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Demo cleanup actions:")
				fmt.Fprintln(out, "  ✓ Reset streaming detector state")
				fmt.Fprintln(out, "  ✓ Clear cached anomaly records")
				fmt.Fprintln(out, "  ✓ Reset drift adaptation counters")
				fmt.Fprintln(out, "")
				fmt.Fprintf(out, "%s Anomaly storage cleared.\n", OK())
			} else {
				idStr, _ := cmd.Flags().GetString("id")
				fmt.Fprintf(out, "Deletion Mode: Remove anomaly ID %s\n", idStr)
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "[SIMULATION] This command would remove the specified anomaly record.")
				fmt.Fprintln(out, "In production, this would delete the anomaly from persistent storage.")
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "Demo deletion actions:")
				fmt.Fprintf(out, "  ✓ Locate anomaly record #%s\n", idStr)
				fmt.Fprintf(out, "  ✓ Remove from cache\n")
				fmt.Fprintf(out, "  ✓ Update detector statistics\n")
				fmt.Fprintln(out, "")
				fmt.Fprintf(out, "%s Anomaly #%s deleted.\n", OK(), idStr)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearAll, "all", false, "Clear all anomalies")
	cmd.Flags().String("id", "", "Specific anomaly ID to delete")
	_ = cmd.MarkFlagRequired("id") // Will error if neither --all nor --id provided
	return cmd
}
