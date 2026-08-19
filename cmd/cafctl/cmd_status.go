// Package main - Status command for cafctl CLI
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"
)

var (
	statusJSON              bool
	statusInterval          int
	statusOfflineReadConfig bool // read local config for run mode when API is down
)

// statusCmd represents the 'status' command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show CloudAI Fusion system health overview",
	Long: `Display real-time status of your CloudAI Fusion deployment including:
• API Server connectivity and health
• Run mode (simulation / degraded / production) with a prominent badge
• Per-subsystem capability panel: real vs simulated (from /api/v1/capabilities)
• Evidence chain integrity and entry count
• GPU availability and MIG configuration

Output uses textual [REAL]/[SIM] markers so it is readable on a no-color terminal.
Use --json for machine-readable output suitable for dashboards and monitoring scripts.`,
	Example: `  # Quick status check
  cafctl status
  
  # Continuous monitoring with updates every 5 seconds
  cafctl status --interval 5
  
  # JSON output for CI/CD pipelines
  cafctl status --json`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().BoolVarP(&statusJSON, "json", "j", false,
		"Output in JSON format")
	statusCmd.Flags().IntVarP(&statusInterval, "interval", "", 0,
		"Update interval in seconds (default: disable continuous mode)")
	statusCmd.Flags().BoolVar(&statusOfflineReadConfig, "offline-read-config", true,
		"Read run mode from local .caf/config.yaml when the API server is offline")
}

type StatusResponse struct {
	Timestamp    string              `json:"timestamp"`
	APIServer    ServerStatus        `json:"api_server"`
	Evidence     EvidenceStatus      `json:"evidence"`
	GPU          []GPUInfo           `json:"gpu,omitempty"`
	Scheduler    SchedulerStatus     `json:"scheduler,omitempty"`
	RedTeam      RedTeamStatus       `json:"redteam,omitempty"`
	Uptime       string              `json:"uptime,omitempty"`
	Capabilities CapabilitiesSummary `json:"capabilities,omitempty"`
}

type ServerStatus struct {
	Status    string `json:"status"`
	URL       string `json:"url"`
	LatencyMS int    `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type EvidenceStatus struct {
	Count      int64  `json:"count"`
	Intact     bool   `json:"intact"`
	KeyID      string `json:"key_id"`
	LatestHash string `json:"latest_hash,omitempty"`
}

type GPUInfo struct {
	Name        string `json:"name"`
	MemoryTotal string `json:"memory_total"`
	MemoryUsed  string `json:"memory_used"`
	MemoryFree  string `json:"memory_free"`
	Utility     string `json:"utility_percent"`
	DriverVer   string `json:"driver_version"`
	MIGMode     string `json:"mig_mode,omitempty"`
	Devices     []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		PersistenceMode string `json:"persistence_mode"`
	} `json:"devices,omitempty"`
}

type SchedulerStatus struct {
	Running bool   `json:"running"`
	Epoch   int    `json:"epoch"`
	State   string `json:"state"`
	LastRun string `json:"last_run,omitempty"`
}

type RedTeamStatus struct {
	Running      bool   `json:"running"`
	Status       string `json:"status"`
	LastCampaign string `json:"last_campaign,omitempty"`
	LastRun      string `json:"last_run,omitempty"`
}

// CapabilitiesResponse matches the GET /api/v1/capabilities contract exposed by
// pkg/api (handleCapabilities): run_mode + per-subsystem real/simulated status.
type CapabilitiesResponse struct {
	RunMode        string    `json:"run_mode"`
	AllReal        bool      `json:"all_real"`
	SimulatedCount int       `json:"simulated_count"`
	Backends       []Backend `json:"backends"`
	Simulated      []Backend `json:"simulated"`
}

// Backend mirrors one entry from /api/v1/capabilities (pkg/capability.Backend).
type Backend struct {
	Component string `json:"component"`
	Mode      string `json:"mode"`
	Driver    string `json:"driver"`
	Detail    string `json:"detail,omitempty"`
}

// CapabilitiesSummary is the rendered view held on StatusResponse. Source records
// whether the data came from the live API or the local config fallback.
type CapabilitiesSummary struct {
	RunMode        string    `json:"run_mode,omitempty"`
	AllReal        bool      `json:"all_real"`
	SimulatedCount int       `json:"simulated_count"`
	Backends       []Backend `json:"backends,omitempty"`
	Source         string    `json:"source,omitempty"` // "api" | "local-config"
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	if statusInterval > 0 {
		return runContinuousStatus(ctx)
	}

	// Single status check
	resp := getStatus(ctx)

	if statusJSON {
		PrintInfo(ToJSON(resp))
	} else {
		printStatusTable(resp)
	}

	return nil
}

func runContinuousStatus(ctx context.Context) error {
	fmt.Println("🔍 CloudAI Fusion Status Monitor (Ctrl+C to stop)")
	fmt.Println(strings.Repeat("=", 80))

	ticker := time.NewTicker(time.Duration(statusInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nShutting down...")
			return ctx.Err()
		case <-ticker.C:
			fmt.Print("\033[H\033[J") // Clear screen
			resp := getStatus(ctx)
			printStatusTable(resp)
		}
	}
}

func getStatus(ctx context.Context) StatusResponse {
	resp := StatusResponse{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Check API server
	apiURL := "http://localhost:8080/health"
	resp.APIServer.URL = apiURL

	start := time.Now()
	client := &http.Client{Timeout: 2 * time.Second}
	httpResp, err := client.Get(apiURL)
	if err == nil {
		resp.APIServer.Status = "Online"
		resp.APIServer.LatencyMS = int(time.Since(start).Milliseconds())
		_ = httpResp.Body.Close()

		// Query /api/v1/capabilities for the honest real-vs-simulated panel.
		capURL := "http://localhost:8080/api/v1/capabilities"
		if capResp, capErr := client.Get(capURL); capErr == nil {
			if capResp.StatusCode == http.StatusOK {
				var caps CapabilitiesResponse
				if parseJSON(capResp.Body, &caps) == nil {
					resp.Capabilities = CapabilitiesSummary{
						RunMode:        caps.RunMode,
						AllReal:        caps.AllReal,
						SimulatedCount: caps.SimulatedCount,
						Backends:       caps.Backends,
						Source:         "api",
					}
				}
			}
			_ = capResp.Body.Close()
		}
	} else {
		resp.APIServer.Status = "Offline"
		resp.APIServer.Error = FormatError(err)

		// Fallback: read run_mode from the local .caf/config.yaml so the user
		// still sees their configured mode even when the server is down.
		if statusOfflineReadConfig {
			readRunModeFromLocalConfig(&resp.Capabilities)
		}
	}

	// Check evidence chain (local-first): read the on-disk chain if present.
	chainPath := filepath.Join(".caf", "evidence.chain")
	if bundle, berr := evidence.ReadBundleFile(chainPath); berr == nil {
		resp.Evidence.Count = int64(len(bundle.Records))
		resp.Evidence.KeyID = bundle.KeyID
		if report, verr := evidence.VerifyBundle(bundle); verr == nil {
			resp.Evidence.Intact = report.Valid
		}
		if len(bundle.Records) > 0 {
			last := bundle.Records[len(bundle.Records)-1]
			resp.Evidence.LatestHash = last.Hash[:16] + "..."
		}
	}

	// Check GPU status (if available)
	gpuInfo := checkGPUs()
	if len(gpuInfo) > 0 {
		resp.GPU = gpuInfo
	}

	return resp
}

// parseJSON decodes r into v.
func parseJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

// readRunModeFromLocalConfig parses the local .caf/config.yaml for a run_mode line.
// It is a deliberately tiny line scanner (no YAML dependency) that only extracts
// the single field we need for the offline status fallback.
func readRunModeFromLocalConfig(summary *CapabilitiesSummary) {
	path := filepath.Clean(filepath.Join(".caf", "config.yaml"))
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "run_mode:") {
			mode := strings.TrimSpace(strings.TrimPrefix(line, "run_mode:"))
			if mode != "" {
				summary.RunMode = mode
				summary.Source = "local-config"
			}
			return
		}
	}
}

func checkGPUs() []GPUInfo {
	var gpus []GPUInfo

	// Try nvidia-smi
	cmd := exec.Command("nvidia-smi", "--query-gpu=name,memory.total,memory.used,memory.free,utilization.gpu,driver_version,-o", "csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return gpus
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) >= 4 {
			gpu := GPUInfo{
				Name:        strings.TrimSpace(parts[0]),
				MemoryTotal: parts[1],
				MemoryUsed:  parts[2],
				MemoryFree:  parts[3],
				Utility:     parts[4],
				DriverVer:   parts[5],
			}
			gpus = append(gpus, gpu)
		}
	}

	return gpus
}

// runModeBadge renders a prominent, no-color-safe run-mode banner to w. Simulation
// gets a loud warning; production gets an explicit confirmation. It is written
// against an io.Writer so tests can capture and assert the exact banner text.
func runModeBadge(w io.Writer, mode, source string) {
	src := ""
	if source == "local-config" {
		src = " (from local .caf/config.yaml — API server offline)"
	}
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "production":
		greenBold.Fprintf(w, "  [PROD] RUN MODE: PRODUCTION  [PRODUCTION READY]%s\n", src)
		green.Fprintf(w, "  ✓ Simulated backends are forbidden; every subsystem must be real.\n")
	case "degraded":
		yellowBold.Fprintf(w, "  [DEG] 🟡 RUN MODE: DEGRADED  [!! REAL PREFERRED, SIM SURFACED !!]%s\n", src)
	case "simulation":
		yellowBold.Fprintf(w, "  [SIM ] ⚠️  RUN MODE: SIMULATION  [!!! WARNING: SIMULATED BACKENDS ALLOWED !!!]%s\n", src)
		yellow.Fprintf(w, "  ⚠ Do NOT use simulation mode for real workloads — data is not persisted to real infra.\n")
	default:
		defaultColor.Fprintf(w, "  RUN MODE: unknown%s\n", src)
	}
}

// printRunModeBadge is the test-facing entry point that writes the badge to w.
func printRunModeBadge(w io.Writer, mode, source string) {
	runModeBadge(w, mode, source)
}

// printCapabilityBackends renders the per-subsystem real/simulated table to w
// with textual markers so it reads correctly on a no-color terminal.
func printCapabilityBackends(w io.Writer, caps CapabilitiesSummary) {
	if len(caps.Backends) == 0 {
		return
	}
	fmt.Fprintln(w, "")
	cyanBold.Fprintf(w, "  Subsystem capabilities (real vs simulated):\n")
	for _, b := range caps.Backends {
		marker := "[SIM ]"
		colorFn := yellow
		switch strings.ToLower(b.Mode) {
		case "real":
			marker = "[REAL]"
			colorFn = green
		case "disabled", "offlined", "offline":
			marker = "[OFF ]"
			colorFn = defaultColor
		}
		detail := b.Driver
		if b.Detail != "" {
			detail = fmt.Sprintf("%s — %s", b.Driver, b.Detail)
		}
		colorFn.Fprintf(w, "    %s %-24s %s\n", marker, b.Component, detail)
	}
	if caps.SimulatedCount == 0 {
		green.Fprintf(w, "  ✓ All %d subsystems real.\n", len(caps.Backends))
	} else {
		yellow.Fprintf(w, "  ⚠ %d/%d subsystem(s) simulated.\n", caps.SimulatedCount, len(caps.Backends))
	}
}

func printStatusTable(resp StatusResponse) {
	fmt.Println("")
	cyanBold.Println("CloudAI Fusion Status")
	fmt.Println(Separator('━', 80))

	// Run-mode badge (most important line — always shown when known).
	if resp.Capabilities.RunMode != "" {
		runModeBadge(os.Stdout, resp.Capabilities.RunMode, resp.Capabilities.Source)
		fmt.Println(Separator('─', 80))
	}

	// API Server
	apiSymbol := "●"
	apiColor := yellow
	if resp.APIServer.Status == "Online" {
		apiColor = green
	} else if resp.APIServer.Status == "Offline" {
		apiColor = red
	}

	fmt.Printf("%s API Server:    ", apiSymbol)
	apiColor.Println(resp.APIServer.Status)
	if resp.APIServer.LatencyMS > 0 {
		yellow.Printf("  Latency:     %dms\n", resp.APIServer.LatencyMS)
	}
	if resp.APIServer.Error != "" {
		red.Printf("  Error:       %s\n", resp.APIServer.Error)
		// Actionable next steps — never leave the user with a raw dial error.
		yellow.Println("  Next steps:")
		yellow.Println("    • Start the server:  go run ./cmd/apiserver --config cloudai-fusion.yaml")
		yellow.Println("    • Or check a custom port/host if you changed the default :8080")
		yellow.Println("    • Local evidence chain still works offline (see below).")
	}

	// Capability panel from /api/v1/capabilities (when server was reachable).
	printCapabilityBackends(os.Stdout, resp.Capabilities)

	// Evidence Chain
	fmt.Println("")
	evidenceSymbol := "●"
	evidenceColor := cyan
	if resp.Evidence.Count > 0 && resp.Evidence.Intact {
		evidenceColor = green
	}

	fmt.Printf("%s Evidence:      ", evidenceSymbol)
	if resp.Evidence.Count > 0 {
		evidenceColor.Printf("%d entries, chain intact\n", resp.Evidence.Count)
	} else {
		evidenceColor.Printf("%d entries (empty)\n", resp.Evidence.Count)
		yellow.Println("  Next step: run 'cafctl init' to create the evidence chain, then 'cafctl attest'.")
	}

	if resp.Evidence.Count > 0 && resp.Evidence.LatestHash != "" {
		yellow.Printf("  Latest hash: %s\n", resp.Evidence.LatestHash)
	}

	// GPUs
	if len(resp.GPU) > 0 {
		fmt.Printf("%s GPUs:          ", infoSymbol)
		cyanBold.Printf("%d× NVIDIA GPUs detected\n", len(resp.GPU))

		for _, gpu := range resp.GPU {
			yellow.Printf("  • %s (%s free / %s total)\n", gpu.Name, gpu.MemoryFree, gpu.MemoryTotal)
			yellow.Printf("    Utility:   %s%% | Driver: %s\n", gpu.Utility, gpu.DriverVer)
		}
	}

	fmt.Println("")

	// Footer with timestamp
	defaultColor.Printf("Generated at %s\n", resp.Timestamp)
	fmt.Println("")
}
