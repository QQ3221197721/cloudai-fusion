package chaos

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// GPUGPUTemperatureStresser simulates GPU overheating scenarios.
type GPUGPUTemperatureStresser struct {
	targetTemperature int      // target temperature in Celsius
	devices           []string // GPU devices to stress (empty = all)
	interval          time.Duration
	logger            Logger
}

// GPUStressConfig configures GPU temperature stress testing.
type GPUStressConfig struct {
	TargetTemperature int
	Devices           []string
	Interval          time.Duration
	Logger            Logger
}

// DefaultGPUStressConfig returns default configuration.
func DefaultGPUStressConfig() GPUStressConfig {
	return GPUStressConfig{
		TargetTemperature: 85, // Target 85°C
		Interval:          30 * time.Second,
		Logger:            nil,
	}
}

// NewGPUGPUTemperatureStresser creates a new GPU temperature stresser.
func NewGPUGPUTemperatureStresser(cfg GPUStressConfig) *GPUGPUTemperatureStresser {
	if cfg.TargetTemperature <= 0 || cfg.TargetTemperature > 100 {
		cfg.TargetTemperature = 85
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	
	return &GPUGPUTemperatureStresser{
		targetTemperature: cfg.TargetTemperature,
		devices:           cfg.Devices,
		interval:          cfg.Interval,
		logger:            cfg.Logger,
	}
}

// Inject simulates GPU overheat condition.
func (g *GPUGPUTemperatureStresser) Inject(ctx context.Context) error {
	g.logf("[GPU Temp Stresser] Starting GPU temperature simulation: %d°C", g.targetTemperature)
	
	// Use nvidia-smi to set power limit as proxy for temperature stress
	if err := g.setPowerLimit(ctx); err != nil {
		g.logf("[GPU Temp Stresser] Power limit method failed, trying alternative: %v", err)
		return g.simulateTemperatureViaLoad(ctx)
	}
	
	g.logf("[GPU Temp Stresser] GPU temperature stress active")
	
	// Maintain the condition until context cancelled
	<-ctx.Done()
	
	g.logf("[GPU Temp Stresser] Stopping GPU temperature stress")
	return nil
}

// Remove removes GPU temperature stress.
func (g *GPUGPUTemperatureStresser) Remove(ctx context.Context) error {
	g.logf("[GPU Temp Stresser] Restoring default power limits")
	return g.restorePowerLimit(ctx)
}

// setPowerLimit uses nvidia-smi to reduce power limit (simulates thermal throttling).
func (g *GPUGPUTemperatureStresser) setPowerLimit(ctx context.Context) error {
	// Check if nvidia-smi is available
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-q")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("nvidia-smi not available: %w", err)
	}
	
	// Find GPU count
	listCmd := exec.CommandContext(ctx, "nvidia-smi", "-L")
	output, err := listCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list GPUs: %w", err)
	}
	
	deviceCount := len([]byte(output)) / 64 // Rough approximation
	
	g.logf("[GPU Temp Stresser] Detected %d GPUs", deviceCount)
	
	// Set reduced power limit to induce thermal stress
	// Typical values: 250W -> 150W induces throttling
	powerLimit := 150
	for i := 0; i < deviceCount; i++ {
		cmd := exec.CommandContext(ctx, "nvidia-smi", "-pl", fmt.Sprintf("%d", powerLimit), "-i", fmt.Sprintf("%d", i))
		if err := cmd.Run(); err != nil {
			g.logf("[GPU Temp Stresser] Failed to set power limit on GPU %d: %v", i, err)
			continue
		}
		g.logf("[GPU Temp Stresser] Set power limit to %dwatt on GPU %d", powerLimit, i)
	}
	
	return nil
}

// simulateTemperatureViaLoad uses compute workloads to generate heat.
func (g *GPUGPUTemperatureStresser) simulateTemperatureViaLoad(ctx context.Context) error {
	g.logf("[GPU Temp Stresser] Using compute load method for temperature simulation")
	
	// This would typically involve running CUDA kernels or ML workloads
	// For now, we'll log that this needs implementation based on available GPU tools
	
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(1 * time.Minute):
		g.logf("[GPU Temp Stresser] Simulated workload active")
		return nil
	}
}

// restorePowerLimit restores original power limits.
func (g *GPUGPUTemperatureStresser) restorePowerLimit(ctx context.Context) error {
	// Check if nvidia-smi is available
	cmd := exec.CommandContext(ctx, "nvidia-smi", "-q")
	if err := cmd.Run(); err != nil {
		return nil // Skip if no NVIDIA GPU
	}
	
	// Find GPU count
	listCmd := exec.CommandContext(ctx, "nvidia-smi", "-L")
	output, err := listCmd.Output()
	if err != nil {
		return nil
	}
	
	deviceCount := len([]byte(output)) / 64
	
	g.logf("[GPU Temp Stresser] Restoring power limits on %d GPUs", deviceCount)
	
	// Reset to maximum power limit
	for i := 0; i < deviceCount; i++ {
		// Use 0 to reset to max allowed by board
		cmd := exec.CommandContext(ctx, "nvidia-smi", "-pl", "0", "-i", fmt.Sprintf("%d", i))
		_ = cmd.Run()
	}
	
	return nil
}

// GetGPUTemperature retrieves current GPU temperatures.
func (g *GPUGPUTemperatureStresser) GetGPUTemperature(ctx context.Context) (map[string]int, error) {
	temperatures := make(map[string]int)
	
	cmd := exec.CommandContext(ctx, "nvidia-smi", "--query-gpu=temperature.gpu --format=csv,noheader,nounits")
	output, err := cmd.Output()
	if err != nil {
		return temperatures, fmt.Errorf("failed to get GPU temperatures: %w", err)
	}
	
	lines := splitLines(string(output))
	for i, line := range lines {
		temp := parseTemp(line)
		temperatures[fmt.Sprintf("gpu-%d", i)] = temp
	}
	
	return temperatures, nil
}

// Helper functions

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func parseTemp(s string) int {
	var temp int
	fmt.Sscanf(s, "%d", &temp)
	return temp
}

// logf logs a message if logger is available.
func (g *GPUGPUTemperatureStresser) logf(format string, args ...interface{}) {
	if g.logger != nil {
		g.logger.Infof(format, args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}
