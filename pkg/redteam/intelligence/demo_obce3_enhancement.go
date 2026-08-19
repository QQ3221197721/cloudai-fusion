package redteam

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// RunOBCE3Demo demonstrates the OBCE3 offensive capability pipeline
func RunOBCE3Demo() {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetReportCaller(true)

	fmt.Println("===========================================")
	fmt.Println("CloudAI Fusion - OBCE3 Offensive Capability")
	fmt.Println("Multi-Source CVE Intelligence Pipeline Demo")
	fmt.Println("===========================================")
	fmt.Println()

	// Create logger
	log := logger.WithField("component", "demo")
	log.Info("Starting OBCE3 demo pipeline...")

	// Initialize multi-source feed manager
	feedManager := NewMultiSourceFeedManager(logger, "/tmp/cve_cache")
	log.Info("MultiSourceFeedManager initialized")

	// Initialize enrichment pipeline
	enrichmentPipeline := NewCVEEnrichmentPipeline(feedManager, logger, 5, 100)
	log.Info("CVEEnrichmentPipeline initialized")

	// Kill Chain Chainer
	chainer := NewKillChainChainer(logger)
	log.Info("KillChainChainer initialized")

	fmt.Println()
	fmt.Println("Testing Multi-Source Data Aggregation...")
	fmt.Println("-----------------------------------------")

	// Simulate fetching data from multiple sources (without actual API calls)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Run enrichment pipeline with limited test data
	go func() {
		err := enrichmentPipeline.Run(ctx, 5)
		if err != nil {
			log.Warnf("Pipeline completed with warnings: %v", err)
		} else {
			log.Info("Pipeline completed successfully")
		}
	}()

	// Wait for processing
	time.Sleep(2 * time.Second)

	metrics := enrichmentPipeline.GetMetrics()
	fmt.Printf("\nProcessing Metrics:\n")
	fmt.Printf("  Total Processed: %d\n", metrics.TotalProcessed)
	fmt.Printf("  Processing Time: %.2fs\n", metrics.Duration.Seconds())
	fmt.Printf("  Throughput: %.2f CVEs/sec\n", metrics.Throughput)

	fmt.Println()
	fmt.Println("Testing Kill Chain Construction...")
	fmt.Println("-----------------------------------")

	// Simulate attack chain building
	simulatedCVEs := []string{
		"CVE-2024-38694", // PrintSpooler RCE
		"CVE-2024-21410", // Windows Kernel
		"CVE-2024-30076", // Window Code Execution
	}

	constraints := AttackConstraints{
		MaxSteps:      5,
		MinSuccessRate: 0.6,
		AllowedTactics: []string{"Initial Access", "Execution", "Persistence", "Privilege Escalation"},
	}

	result, err := chainer.FindOptimalAttackPath(ctx, simulatedCVEs, []string{"Actions on Objectives"}, constraints)
	if err != nil {
		log.Errorf("Failed to build attack path: %v", err)
	} else {
		fmt.Printf("\nGenerated Attack Path:\n")
		fmt.Printf("  Path ID: %s\n", result.Path.ID)
		fmt.Printf("  Name: %s\n", result.Path.Name)
		fmt.Printf("  Description: %s\n", result.Path.Description)
		fmt.Printf("  Steps: %d\n", len(result.Path.Steps))
		fmt.Printf("  Score: %.2f\n", result.Score)
		fmt.Printf("  Detection Risk: %.1f%%\n", result.DetectionRisk*100)
		fmt.Printf("  Estimated Time: %v\n", result.EstimatedTime.Round(time.Minute))

		fmt.Println("\nStep Details:")
		for i, step := range result.Path.Steps {
			fmt.Printf("  Step %d: %s\n", i+1, step.Phase)
			fmt.Printf("    Type: %s\n", step.Type)
			fmt.Printf("    Risk Score: %.1f\n", step.RiskScore)
			fmt.Printf("    Privileges: %v\n\n", step.RequiredPrivileges)
		}

		fmt.Printf("Scoring Rationale:\n%s\n", result.Rationale)
	}

	fmt.Println()
	fmt.Println("===========================================")
	fmt.Println("Demo Completed Successfully!")
	fmt.Println("OBCE3 Offense Capability Enhancement Ready")
	fmt.Println("===========================================")
}
