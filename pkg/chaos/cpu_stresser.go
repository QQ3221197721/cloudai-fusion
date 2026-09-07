// Package chaos provides fault injection tools for testing self-healing capabilities.
package chaos

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// CPUStresser injects CPU load to test system resilience and auto-scaling.
type CPUStresser struct {
	duration time.Duration
	workers  int // number of parallel CPU-intensive workers
	logger   Logger
}

// Logger is an interface for logging chaos experiments.
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// CPUStresserConfig configures CPU stress testing.
type CPUStresserConfig struct {
	Duration time.Duration
	Workers  int
	Logger   Logger
}

// DefaultCPUStresserConfig returns default configuration.
func DefaultCPUStresserConfig() CPUStresserConfig {
	return CPUStresserConfig{
		Duration: 5 * time.Minute,
		Workers:  runtime.NumCPU(),
		Logger:   nil, // will use standard logger
	}
}

// NewCPUStresser creates a new CPU stresser with the given configuration.
func NewCPUStresser(cfg CPUStresserConfig) *CPUStresser {
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.NumCPU()
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 5 * time.Minute
	}
	
	return &CPUStresser{
		duration: cfg.Duration,
		workers:  cfg.Workers,
		logger:   cfg.Logger,
	}
}

// Inject starts CPU stress testing.
func (c *CPUStresser) Inject(ctx context.Context) error {
	c.logf("[CPU Stresser] Starting CPU stress test: %d workers for %v", c.workers, c.duration)
	
	// Create worker goroutines that consume CPU
	done := make(chan bool)
	
	for i := 0; i < c.workers; i++ {
		go func(id int) {
			select {
			case <-ctx.Done():
				return
			default:
				cpuBurner(id, done)
			}
		}(i)
	}
	
	// Run for specified duration
	time.Sleep(c.duration)
	
	// Stop all workers
	close(done)
	
	c.logf("[CPU Stresser] Stopped CPU stress test")
	return nil
}

// Remove removes all CPU stress (should be called on cleanup).
func (c *CPUStresser) Remove(ctx context.Context) error {
	c.logf("[CPU Stresser] Cleanup complete")
	return nil
}

// cpuBurner performs CPU-intensive computation.
func cpuBurner(id int, done chan bool) {
	sum := 0.0
	
	for {
		select {
		case <-done:
			return
		default:
			// CPU-intensive operation: floating point calculations
			for j := 0.0; j < 1000000; j++ {
				sum += j * 0.0001
			}
		}
		
		_ = sum // Prevent optimization
		_ = id  // Prevent unused variable warning
	}
}

// logf logs a message if logger is available.
func (c *CPUStresser) logf(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger.Infof(format, args...)
	} else {
		fmt.Printf(format+"\n", args...)
	}
}

// Use stress-ng tool if available.
func stressWithStressNG(ctx context.Context, duration time.Duration, workers int) error {
	cmd := exec.CommandContext(ctx, "stress-ng", 
		"--cpu", fmt.Sprintf("%d", workers),
		"--timeout", duration.String())
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("stress-ng failed: %w, output: %s", err, string(output))
	}
	
	return nil
}
