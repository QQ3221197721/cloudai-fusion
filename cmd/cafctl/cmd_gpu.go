// Package main - GPU management commands for cafctl CLI
package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/spf13/cobra"
)

var (
	gpuListJSON bool
	gpuAllocCount int
	gpuAllocMIG string
)

// gpuCmd represents the 'gpu' command
var gpuCmd = &cobra.Command{
	Use:   "gpu [list|topology|allocate]",
	Short: "Manage NVIDIA GPU resources",
	Long: `GPU management commands for controlling NVIDIA accelerator resources:
• gpu list: Display all available GPUs with memory, utilization, and MIG status
• gpu topology: Show NVLink connectivity matrix between GPUs
• gpu allocate: Request GPU allocation with specific constraints

These commands work independently of any running scheduler for local-first operations.`,
}

func init() {
	rootCmd.AddCommand(gpuCmd)
	
	// List subcommand
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all GPUs",
		Long:  `Display detailed information about all available NVIDIA GPUs including memory usage, utilization, and driver version.`,
		RunE: runGPUList,
	}
	gpuCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&gpuListJSON, "json", "j", false, "Output in JSON format")
	
	// Topology subcommand
	topoCmd := &cobra.Command{
		Use:   "topology",
		Short: "Show GPU connectivity topology",
		Long: `Display NVLink connections between GPUs using nvidia-smi topology.
Shows which GPUs have direct PCIe/NVLink links and their bandwidth capabilities.`,
		RunE: runGPUTopology,
	}
	gpuCmd.AddCommand(topoCmd)
	
	// Allocate subcommand
	allocCmd := &cobra.Command{
		Use:   "allocate",
		Short: "Request GPU allocation",
		Long: `Request a GPU allocation based on specified criteria:
• --count: Number of GPU devices to allocate
• --mig: MIG slice profile (e.g., "3g.20gb", "7g.40gb")
• --exclusive: Request exclusive access to entire GPU

Note: This simulates allocation decisions; actual GPU isolation requires system-level permissions.`,
		Example: `  # Allocate 2 GPUs exclusively
  cafctl gpu allocate --count 2
  
  # Request MIG slice
  cafctl gpu allocate --mig "3g.20gb"
  
  # Multiple constraints
  cafctl gpu allocate --count 4 --mig "1g.5gb"`,
		RunE: runGPUAllocate,
	}
	gpuCmd.AddCommand(allocCmd)
	allocCmd.Flags().IntVarP(&gpuAllocCount, "count", "", 1, "Number of GPUs to allocate")
	allocCmd.Flags().StringVarP(&gpuAllocMIG, "mig", "", "", "MIG slice profile (e.g., 3g.20gb)")
}

func runGPUList(cmd *cobra.Command, args []string) error {
	if gpuListJSON {
		return runGPUListJSON(cmd, args)
	}
	
	cmd.Println("")
	cyanBold.Println("GPU Inventory")
	fmt.Println(Separator('━', 80))
	
	// Try to get nvidia-smi output
	output, err := exec.Command("nvidia-smi", "--query-gpu=index,name,memory.total,memory.used,memory.free,utilization.gpu,driver_version", "--format=csv,noheader,nounits").Output()
	if err != nil {
		yellow.Println(WARN() + " nvidia-smi not found or no NVIDIA GPUs detected")
		yellow.Println("  To install: sudo apt-get install nvidia-utils-xxx (or equivalent)")
		return nil
	}
	
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	
	headers := []string{"Index", "Name", "Memory Total", "Memory Used", "Memory Free", "Utility", "Driver"}
	rows := make([][]string, len(lines))
	
	for i, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) >= 7 {
			rows[i] = []string{
				strings.TrimSpace(parts[0]),
				strings.TrimSpace(parts[1]),
				strings.TrimSpace(parts[2]),
				strings.TrimSpace(parts[3]),
				strings.TrimSpace(parts[4]),
				strings.TrimSpace(parts[5]) + "%",
				strings.TrimSpace(parts[6]),
			}
		}
	}
	
	PrintTable(headers, rows)
	
	fmt.Println("")
	yellow.Printf("%d NVIDIA GPU(s) detected\n", len(lines))
	
	return nil
}

func runGPUListJSON(cmd *cobra.Command, args []string) error {
	type GPUInfo struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Memory    string `json:"memory_total"`
		Used      string `json:"memory_used"`
		Free      string `json:"memory_free"`
		Utility   string `json:"utility"`
		DriverVer string `json:"driver_version"`
	}
	
	var gpus []GPUInfo
	
	output, err := exec.Command("nvidia-smi", "--query-gpu=index,name,memory.total,memory.used,memory.free,utilization.gpu,driver_version", "--format=csv,noheader,nounits").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			parts := strings.Split(line, ",")
			if len(parts) >= 7 {
				gpus = append(gpus, GPUInfo{
					ID:        strings.TrimSpace(parts[0]),
					Name:      strings.TrimSpace(parts[1]),
					Memory:    strings.TrimSpace(parts[2]),
					Used:      strings.TrimSpace(parts[3]),
					Free:      strings.TrimSpace(parts[4]),
					Utility:   strings.TrimSpace(parts[5]),
					DriverVer: strings.TrimSpace(parts[6]),
				})
			}
		}
	}
	
	resp := map[string]interface{}{
		"gpus":        gpus,
		"count":       len(gpus),
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
	
	PrintInfo(ToJSONPretty(resp))
	return nil
}

func runGPUTopology(cmd *cobra.Command, args []string) error {
	cmd.Println("")
	cyanBold.Println("GPU Topology")
	fmt.Println(Separator('━', 80))
	
	// Use the canonical `nvidia-smi topo -m` matrix form: it prints the GPU-GPU
	// connection matrix plus CPU/NUMA affinity and works across driver versions
	// (the older `topology -g all -t GPU -m -T` invocation rejects -T with -m).
	output, err := exec.Command("nvidia-smi", "topo", "-m").Output()
	if err != nil {
		yellow.Println(WARN() + " Cannot display topology: nvidia-smi not available or unsupported")
		return nil
	}
	
	fmt.Print(string(output))
	
	// Count GPU rows in the matrix (lines beginning with the GPU<n> label).
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "GPU") {
			count++
		}
	}
	
	greenBold.Printf("Topology shows %d GPU(s) interconnected via NVLink/PCIe\n", count)
	
	return nil
}

func runGPUAllocate(cmd *cobra.Command, args []string) error {
	// Validate inputs
	if gpuAllocCount < 1 {
		return fmt.Errorf("count must be at least 1")
	}

	// Build the allocation response (simulated; real isolation needs the scheduler).
	type AllocationResponse struct {
		RequestID string   `json:"request_id"`
		Timestamp string   `json:"timestamp"`
		Requested int      `json:"requested_count"`
		Assigned  []string `json:"assigned_gpus"`
		MIG       string   `json:"mig_profile,omitempty"`
		Status    string   `json:"status"`
		Note      string   `json:"note"`
	}

	assigned := make([]string, 0, gpuAllocCount)
	for i := 0; i < gpuAllocCount && i < 8; i++ {
		assigned = append(assigned, fmt.Sprintf("GPU-%d", i))
	}

	response := AllocationResponse{
		RequestID: common.NewUUID()[:8] + "...",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Requested: gpuAllocCount,
		Assigned:  assigned,
		MIG:       gpuAllocMIG,
		Status:    "SIMULATED",
		Note:      "Real allocation requires scheduler service",
	}

	if statusJSON {
		PrintInfo(ToJSON(response))
	} else {
		fmt.Println("")
		greenBold.Println(OK() + " GPU allocation simulation")
		cyanBold.Printf("  Request ID:  %s\n", response.RequestID)
		yellowBold.Printf("  Timestamp:   %s\n", response.Timestamp)
		yellowBold.Printf("  Requested:   %d GPU(s)\n", response.Requested)
		yellowBold.Printf("  Assigned:    %d GPU(s): %v\n", len(response.Assigned), strings.Join(response.Assigned, ", "))

		if response.MIG != "" {
			green.Printf("  MIG Profile: %s\n", response.MIG)
		}

		fmt.Println("")
		yellowBold.Print("  Note: This is a simulated allocation. For real GPU scheduling,\n")
		yellowBold.Print("        start the full CloudAI Fusion stack and use the scheduler API.\n")
		fmt.Println("")
	}

	return nil
}
