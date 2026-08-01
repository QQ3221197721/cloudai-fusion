// Package resources implements real GPU resource management with NVIDIA toolkit integration
package resources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	// NVIDIA Container Toolkit commands
	nvidiaToolkitPath = "/usr/bin/nvidia-container-toolkit"
	migPartitionCmd   = "nvidia-smi mig create-gpu-instance"
	migQueryCmd       = "nvidia-smi mig query"
	
	// Default MIG shapes for A100/H100
	defaultMIGShape   = "1g.5gb" // Single slice, 5GB memory
	defaultGPUMode    = "MIG"    // Compute mode
	
	// Timeout settings
	execTimeout = time.Minute * 5
)

// RealResourceManager implements true GPU partitioning with NVIDIA toolkit
type RealResourceManager struct {
	logger         *logrus.Logger
	nvidiaToolkit  string
	systemInfo     SystemInfo
}

type MIGConfig struct {
	GPUInstanceID  int
	CPUCount       int
	MemoryMB       int
	CreateCount    int
	TileProfile    string
}

func NewRealResourceManager(ctx context.Context, logger *logrus.Logger) (*RealResourceManager, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	// Check if nvidia-container-toolkit is available
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil, fmt.Errorf("NVIDIA tools not installed: %w", err)
	}
	
	rm := &RealResourceManager{
		logger:      logger.WithFields(logrus.Fields{"component": "gpu_manager"}),
		nvidiaToolkit: nvidiaToolkitPath,
	}
	
	// Collect system info
	if err := rm.collectSystemInfo(ctx); err != nil {
		return nil, fmt.Errorf("failed to collect system info: %w", err)
	}
	
	return rm, nil
}

// PartitionGPU performs actual MIG partitioning on physical GPU
func (rm *RealResourceManager) PartitionGPU(ctx context.Context, spec GPUSpec) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	
	rm.logger.WithFields(logrus.Fields{
		"gpu_id": spec.GPUID,
		"mig_shape": spec.MIGShape,
	}).Info("Partitioning GPU with MIG")
	
	// Step 1: Query current GPU topology
	currentTopology, err := rm.queryMIGTopology(ctx)
	if err != nil {
		return fmt.Errorf("failed to query MIG topology: %w", err)
	}
	
	// Step 2: Check if MIG can be configured
	if !rm.isMIGAvailable(currentTopology) {
		return fmt.Errorf("MIG not supported or already configured")
	}
	
	// Step 3: Execute nvidia-smi command for MIG partitioning
	cmd := exec.CommandContext(ctx, "nvidia-smi", 
		"mig", "-i", spec.GPUID,
		"create-gpu-instance",
		"-n", spec.MIGConfigs.CPUCount,
		"-m", spec.MIGConfigs.MemoryMB,
		"-p", spec.MIGConfigs.TileProfile,
	)
	
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("MIG partition failed: %w\nOutput: %s", err, output)
	}
	
	rm.logger.WithField("output", string(output)).Info("GPU partitioned successfully")
	
	return nil
}

// queryMIGTopology queries current MIG configuration
func (rm *RealResourceManager) queryMIGTopology(ctx context.Context) (*MIGTopology, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "nvidia-smi", "mig", "query")
	output, err := cmd.Output()
	if err != nil {
		// MIG might not be enabled yet
		return &MIGTopology{}, nil
	}
	
	var topology MIGTopology
	if err := json.Unmarshal(output, &topology); err != nil {
		return nil, fmt.Errorf("failed to parse MIG query: %w", err)
	}
	
	return &topology, nil
}

// isMIGAvailable checks if MIG can be configured
func (rm *RealResourceManager) isMIGAvailable(topology *MIGTopology) bool {
	// Check if GPUs support MIG
	for _, gpu := range topology.GPUs {
		if gpu.SupportsMIG {
			return true
		}
	}
	return false
}

// CheckMIGStatus verifies MIG partitioning was successful
func (rm *RealResourceManager) CheckMIGStatus(ctx context.Context) (bool, error) {
	status, err := rm.queryMIGTopology(ctx)
	if err != nil {
		return false, err
	}
	
	// Verify all GPUs are properly partitioned
	for _, gpu := range status.GPUs {
		if !gpu.IsActive || gpu.PartitionStatus != "ACTIVE" {
			return false, nil
		}
	}
	
	return true, nil
}
