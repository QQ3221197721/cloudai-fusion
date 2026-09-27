package main

import (
    "fmt"
    "github.com/spf13/cobra"
    "github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

var cmdGpuTopology = &cobra.Command{
    Use:   "gpu",
    Short: "GPU topology discovery and management",
    Long: `GPU topology discovery using NVML library.
    
Displays physical GPU connections (PCIe/NVLink), enables topology-aware scheduling decisions.`,
    RunE: func(cmd *cobra.Command, args []string) error {
        discoverer, err := scheduler.NewNvmlTopologyDiscoverer()
        if err != nil {
            return fmt.Errorf("failed to initialize NVML: %w", err)
        }
        
        graph, err := discoverer.Discover()
        if err != nil {
            return fmt.Errorf("failed to discover topology: %w", err)
        }
        
        fmt.Println("GPU Topology Discovery Results:")
        fmt.Printf("Device Count: %d\n", graph.DeviceCount())
        
        for deviceID, connections := range graph.Connections() {
            fmt.Printf("\n%s:\n", deviceID)
            for _, conn := range connections {
                fmt.Printf("  → %s (%s, %d GB/s)\n", conn.Target, conn.Type, conn.Bandwidth)
            }
        }
        
        return nil
    },
}

func init() {
    rootCmd.AddCommand(cmdGpuTopology)
    cmdGpuTopology.Flags().BoolP("verbose", "v", false, "verbose output")
    cmdGpuTopology.Flags().StringP("format", "f", "text", "output format (text/json)")
}
