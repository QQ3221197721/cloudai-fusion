// Package main - cafctl anomaly & disaster subcommands
package main

import (
	"fmt"
	"math/rand"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/anomaly"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/disaster"
	"github.com/spf13/cobra"
)

func newAnomalyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "anomaly",
		Short: "Anomaly — scan metrics for statistical outliers (offline)",
	}
	cmd.AddCommand(newAnomalyScanCmd())
	cmd.AddCommand(newAnomalyReportCmd())
	return cmd
}

// newAnomalyScanCmd scores a synthetic 8-d stream with streaming Mahalanobis.
func newAnomalyScanCmd() *cobra.Command {
	var samples int
	cmd := &cobra.Command{
		Use:           "scan [--samples <n>]",
		Short:         "Detect anomalies in a synthetic multivariate stream",
		Args:          cobra.NoArgs,
		Example:       "  cafctl anomaly scan\n  cafctl anomaly scan --samples 50",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			const d = 8 // dimensionality
			detector := anomaly.NewStreamingDetector(d, 0.975)

			rng := rand.New(rand.NewSource(42))
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl anomaly scan · statistical outlier detection")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Detector: %d-dimensional streaming Mahalanobis (p=%g)\n", d, detector.Threshold())
			fmt.Fprintln(out, "")

			norm := make([]float64, d)
			for i := 0; i < samples; i++ {
				for j := range norm {
					norm[j] = rng.NormFloat64()
				}
				score, anom := detector.Observe(norm)
				if anom {
					fmt.Fprintf(out, "[!] sample #%d  score=%.3f ⚠️ ANOMALY\n", i+1, score)
				} else if i%10 == 0 {
					fmt.Fprintf(out, "• sample #%d  score=%.3f ✓\n", i+1, score)
				}
			}
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Scan complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().IntVar(&samples, "samples", 30, "Number of samples to score")
	return cmd
}

func newDisasterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "disaster",
		Short: "Disaster — backup & failover operations manager (offline)",
	}
	cmd.AddCommand(newDisasterStatusCmd())
	return cmd
}

// newDisasterStatusCmd creates a demo backup via Manager + lists backups.
func newDisasterStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Show backup inventory & failover readiness",
		Args:          cobra.NoArgs,
		Example:       "  cafctl disaster status",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := disaster.ManagerConfig{}
			mgr := disaster.NewManager(cfg)

			// Create demo backup
			backup, err := mgr.CreateBackup("demo-cluster", disaster.BackupTypeFull, disaster.TargetAll)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return fmt.Errorf("create backup failed: %w", err)
			}
			if err := mgr.StartBackup(backup.ID); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return fmt.Errorf("start backup failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl disaster status · backup & failover")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Created demo backup: %s (type=%s)\n", OK(), backup.ID, backup.Type)
			fmt.Fprintf(out, "  Size:      %d bytes\n", backup.SizeBytes)
			fmt.Fprintf(out, "  Checksum:  %s\n", backup.Checksum)
			fmt.Fprintln(out, "")

			backups := mgr.ListBackups()
			fmt.Fprintln(out, "Registered backups:")
			for _, b := range backups {
				status := "\x1b[32m✓\x1b[m"
				if b.Status != disaster.BackupStatusCompleted {
					status = "\x1b[33m✗\x1b[m"
				}
				fmt.Fprintf(out, "  %s %-40s %s\n", status, b.ID, b.Status)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}
