// Package main - CAF CLI main entry point with all commands
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cafctl",
	Short: "CloudAI Fusion Control Tool - Unified security and operations management",
	Long: `CloudAI Fusion Control Tool provides unified command-line interface for:
• Security operations (Red Team campaigns, vulnerability scanning)
• Operations management (deployment, edge computing, moat operations)
• Proof verification and evidence management`,
	Version: "1.0.0",
}

func init() {
	// Add all subcommands
	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(edgeCmd)
	rootCmd.AddCommand(moatCmd)
	rootCmd.AddCommand(proofsCmd)
	
	// Red Team commands (NEWLY INTEGRATED!)
	rootCmd.AddCommand(redteamCmd)
	redteamCmd.AddCommand(campaignCmd)
	redteamCmd.AddCommand(visualizeCmd)
	redteamCmd.AddCommand(reportCmd)
	
	// Global flags
	rootCmd.PersistentFlags().StringP("config", "c", "", "Config file path")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Verbose output")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// Example usage:
/*
# Deploy Kubernetes cluster
cafctl deploy create --name gpu-cluster --region us-east-1

# Manage edge computing nodes
cafctl edge register --node node1 --token abc123

# Moat operations
cafctl moat status

# Proof verification
cafctl proofs verify --file evidence.json

# NEW! Red Team campaign execution
cafctl redteam campaign start \
  --target my-gpu-system \
  --template "AD Compromise + Lateral Movement" \
  --realtime-visualization

# View live kill chain visualization
cafctl redteam visualize --campaign-id camp-123abc

# Generate campaign report
cafctl redteam report --campaign-id camp-123abc --format pdf
*/
