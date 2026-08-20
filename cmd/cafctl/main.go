// Package main - CAF CLI main entry point with all commands
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cafctl",
	Short: "CloudAI Fusion Control Tool - Real working CLI with evidence verification",
	Long: `CloudAI Fusion Control Tool provides a unified command-line interface with:

🔐 Evidence Verification: Verify integrity of tamper-evident chains offline
✍️  Attestation: Record signed attestation events 
💻 GPU Management: List, inspect topology, and allocate GPUs
📊 System Status: Monitor API server, evidence chain, and GPU status
🚀 Project Initialization: Initialize evidence chain infrastructure like 'git init'

These are REAL working commands that provide instant developer value.
Example: 'cafctl verify' gives you proof your control plane actions were real.`,
	Version: "1.0.0",
	Run: func(cmd *cobra.Command, args []string) {
		// If no command given, show help
		cmd.Help()
	},
}

func init() {
	// Add all subcommands
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(deployCmd)
	deployCmd.AddCommand(newDeployRunCmd(), newDeployRollbackCmd(), newDeployCheckCmd())
	rootCmd.AddCommand(edgeCmd)
	rootCmd.AddCommand(newMoatCmd())
	
	// Local control plane startup (Task 93: T1 developer experience)
	rootCmd.AddCommand(newUpCmd())
	
	// Red Team commands (NEWLY INTEGRATED!)
	rootCmd.AddCommand(redteamCmd)
	redteamCmd.AddCommand(campaignCmd)
	redteamCmd.AddCommand(visualizeCmd)
	redteamCmd.AddCommand(reportCmd)
	
	// Manifest commands (STRATEGIC LOCK-IN FORMAT) self-register via cmd_manifest.go init().
	
	// verify/attest/status/gpu/init self-register via their own init() functions.
	
	// Real offline verifiers for the 16-well moat surface (implemented in cmd_proofs.go).
	rootCmd.AddCommand(
		newVerifyInclusionCmd(),
		newVerifyConsistencyCmd(),
		newVerifyCompletenessCmd(),
		newVerifyModelProvenanceCmd(),
		newVerifyRemediationCmd(),
		newVerifySagaCmd(),
		newVerifyDeployCmd(),
		newVerifyEdgeCmd(),
		newVerifyFailoverCmd(),
		newVerifyIsolationCmd(),
	)
	
	// Model registry (Module 13): register/list/show/lineage/rollback.
	rootCmd.AddCommand(newModelCmd())
	
	// Training Job Orchestrator (Module 14): submit/run-once/status/list/cancel.
	rootCmd.AddCommand(newTrainCmd())
	
	// Inference Service Mesh (Module 15): deploy/list/show/route-set/record/stats/stop.
	rootCmd.AddCommand(newInferCmd())

	// Elastic Inference Pool (Module 12): create/node-add/list/show/acquire/release/leases/evaluate.
	rootCmd.AddCommand(newPoolCmd())
	
	// Model Performance Monitor (Module 20): record/baseline/report/alerts.
	rootCmd.AddCommand(newMonitorCmd())
	
	// Experiment Tracking (Module 19): start/metric/complete/fail/list/show/compare.
	rootCmd.AddCommand(newExperimentCmd())

	// ML Pipeline Designer (Module 18): create/publish/run/status/list/cancel.
	rootCmd.AddCommand(newPipelineCmd())

	// Cost-aware Scheduling (Module 17): estimate/config/report/optimize.
	rootCmd.AddCommand(newCostCmd())
	
	// Auto-scaling Engine (Module 16): policy/policy-list/evaluate-monitor/evaluate-experiment/apply/history
	rootCmd.AddCommand(newAutoscaleCmd())
	
	// Multi-tenant GPU Sharing (Module 11): create/list/allocate/delete
	rootCmd.AddCommand(newTenantCmd())
	
	// WellRouter (Module 6): rule-based routing engine with DLQ and attestation
	rootCmd.AddCommand(newWellRouterCmd())
	
	// Multi-Cloud Unified Interface (Module 2): provider-list/cluster-list/ping/plan/estimate-cost/operations
	rootCmd.AddCommand(newCloudCmd())
	
	// Environment self-check with actionable fixes (Task 93: T1 developer experience)
	rootCmd.AddCommand(newDoctorCmd())
	
	// Authentication inspection (T1 CLI — Module 7)
	rootCmd.AddCommand(newAuthCmd())
	
	// Audit, Billing, GitOps, MLops, SOC (batch 1)
	rootCmd.AddCommand(newAuditCmd())
	rootCmd.AddCommand(newBillingCmd())
	rootCmd.AddCommand(newGitopsCmd())
	rootCmd.AddCommand(newMlopsCmd())
	rootCmd.AddCommand(newSocCmd())
	
	// Security, Plugin, Anomaly, Disaster, Alerting, Tracing (batch 2)
	rootCmd.AddCommand(newSecurityCmd())
	rootCmd.AddCommand(newPluginCmd())
	rootCmd.AddCommand(newAnomalyCmd())
	rootCmd.AddCommand(newDisasterCmd())
	rootCmd.AddCommand(newAlertingCmd())
	rootCmd.AddCommand(newTracingCmd())
	
	// Bench performance testing (Task 144: T1 developer experience) - scheduler, reporting, messaging, runmode
	rootCmd.AddCommand(newBenchCmd())
	
	// Correlation, Cluster, Controller, Store, Mesh, Cache (batch 3)
	rootCmd.AddCommand(newCorrelationCmd())
	rootCmd.AddCommand(newClusterCmd())
	rootCmd.AddCommand(newControllerCmd())
	rootCmd.AddCommand(newStoreCmd())
	rootCmd.AddCommand(newMeshCmd())
	rootCmd.AddCommand(newCacheCmd())

	// AISecOps deep wells: Threat Hunting (M29), Sigma Detection (M30), SOAR (M32)
	rootCmd.AddCommand(newHuntCmd())
	rootCmd.AddCommand(newDetectCmd())
	rootCmd.AddCommand(newSoarCmd())

	// WASM Sandbox (M50 Engine + M51 Capability Security): validate/caps
	rootCmd.AddCommand(newWasmCmd())
	
	// API Client Generator + Documentation Generator (M40/M43): gen client/docs
	rootCmd.AddCommand(newGenCmd())
	
	// Edge Computing + CRDT Conflict Resolution (M24/M25/M26): edge resolve/discover/provision
	edgeCmd.AddCommand(newEdgeResolveCmd(), newEdgeDiscoverCmd(), newEdgeProvisionCmd())
	rootCmd.AddCommand(edgeCmd)
	
	// Sandbox Security Scanner (M42): sandbox run
	rootCmd.AddCommand(newSandboxCmd())
	
	// Hot-swap Component Versioning (M52): hotswap status
	rootCmd.AddCommand(newHotswapCmd())
	
	// RL Training Sidecar + Interactive Tutorial Engine (M10/M44)
	rootCmd.AddCommand(newRlCmd())
	rootCmd.AddCommand(newTutorialCmd())
	
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
