// Package main - cafctl anomaly report subcommand (M31 Anomaly Detection).
//
// This command surfaces real, offline, in-memory anomaly detection capabilities:
//
//   - anomaly report (M31, pkg/anomaly) — generates an anomaly detection summary report
//     by running a batch scan through the streaming Mahalanobis detector. It demonstrates
//     multivariate statistical outlier detection using Welford's algorithm for incremental
//     mean/variance computation.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"fmt"
	"math/rand"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/anomaly"
	"github.com/spf13/cobra"
)

func newAnomalyReportCmd() *cobra.Command {
	var samples, dimension int
	var threshold float64
	cmd := &cobra.Command{
		Use:           "report [--samples <n>] [--dimension <d>] [--threshold <p>]",
		Short:         "Generate anomaly detection report with statistical summary",
		Args:          cobra.NoArgs,
		Example:       "  cafctl anomaly report\n  cafctl anomaly report --samples 100 --dimension 8 --threshold 0.975",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			detector := anomaly.NewStreamingDetector(dimension, threshold)

			rng := rand.New(rand.NewSource(12345)) // deterministic seed for reproducibility
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl anomaly report · multivariate anomaly detection report")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Report header
			fmt.Fprintln(out, "Detection Configuration:")
			fmt.Fprintf(out, "  Algorithm: Streaming Mahalanobis Distance (Welford's method)\n")
			fmt.Fprintf(out, "  Dimensionality: %d features\n", dimension)
			fmt.Fprintf(out, "  Threshold (p-value): %.3f (%.1f%% confidence)\n", threshold, threshold*100)
			fmt.Fprintln(out, "")

			// Generate samples and track results
			norm := make([]float64, dimension)
			totalCount := 0
			anomalyCount := 0
			maxScore := 0.0
			sumScore := 0.0
			detectedSamples := make([]struct {
				index int
				score float64
			}, 0)

			for i := 0; i < samples; i++ {
				for j := range norm {
					norm[j] = rng.NormFloat64()
				}
				score, anom := detector.Observe(norm)
				totalCount++
				sumScore += score
				if score > maxScore {
					maxScore = score
				}
				if anom {
					anomalyCount++
					detectedSamples = append(detectedSamples, struct {
						index int
						score float64
					}{index: i + 1, score: score})
				} else if i%20 == 0 {
					fmt.Fprintf(out, "• sample #%d  score=%.3f ✓ normal\n", i+1, score)
				}
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Detection Summary:")
			fmt.Fprintf(out, "  Total samples processed: %d\n", totalCount)
			fmt.Fprintf(out, "  Anomalies detected:      %d (%.1f%%)\n", anomalyCount, float64(anomalyCount)/float64(totalCount)*100)
			fmt.Fprintf(out, "  Average score:           %.3f\n", sumScore/float64(totalCount))
			fmt.Fprintf(out, "  Maximum score:           %.3f\n", maxScore)
			fmt.Fprintln(out, "")

			if len(detectedSamples) > 0 {
				fmt.Fprintln(out, "Detected Anomalies:")
				for _, d := range detectedSamples {
					symbol := WARN()
					if d.score >= 6.0 {
						symbol = redBold.Sprint("✗ CRITICAL")
					} else if d.score >= 4.5 {
						symbol = yellowBold.Sprint("⚠ HIGH")
					} else {
						symbol = cyan.Sprint("🔵 MEDIUM")
					}
					fmt.Fprintf(out, "  %-5s sample #%d  score=%.3f\n", symbol, d.index, d.score)
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintln(out, "Statistical Baseline (after training):")
			fmt.Fprintln(out, "  Mean estimation: Online incremental (O(n) single pass)")
			fmt.Fprintln(out, "  Variance estimation: Welford's numerically stable algorithm")
			fmt.Fprintln(out, "  Covariance assumption: Diagonal (independent dimensions)")
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Scan complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().IntVar(&samples, "samples", 50, "Number of samples to generate and score")
	cmd.Flags().IntVar(&dimension, "dimension", 8, "Number of feature dimensions")
	cmd.Flags().Float64Var(&threshold, "threshold", 0.975, "Confidence threshold for anomaly detection (0.0-1.0)")
	return cmd
}
