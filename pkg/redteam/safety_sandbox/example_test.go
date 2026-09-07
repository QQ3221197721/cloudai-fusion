package safety_sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// ExampleContainerIsolation demonstrates how to use ContainerIsolator for safe exploit execution
func ExampleContainerIsolation() {
	// Initialize logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	
	// Create a new container isolator for tenant and exploit
	iso, err := NewContainerIsolator(
		logger,
		"tenant-production",
		"exploit-cve-2024-1234",
	)
	if err != nil {
		logrus.Fatalf("Failed to create isolator: %v", err)
	}
	
	// Configure isolation parameters
	cfg := DefaultIsolationConfig()
	cfg.BaseImage = "alpine:3.19"
	cfg.ExecutionTimeout = 5 * time.Minute
	cfg.MemoryBytes = 512 << 20 // 512MB
	cfg.CPUCQuota = 200000      // 2 CPU cores
	cfg.DisableNetwork = true   // No network access
	
	iso.SetConfig(cfg)
	
	ctx := context.Background()
	
	// Start isolated container with command
	containerID, err := iso.StartContainer(ctx, []string{"/bin/sh", "-c", "echo 'Hello from isolated container'"})
	if err != nil {
		logrus.Fatalf("Failed to start container: %v", err)
	}
	defer func() {
		_ = iso.StopContainer(ctx, 5*time.Second)
		iso.cleanup(ctx)
	}()
	
	fmt.Printf("Container started with ID: %s\n", containerID)
	
	// Get logs
	output, err := iso.GetContainerLogs(ctx)
	if err != nil {
		logrus.Warnf("Failed to get logs: %v", err)
	} else {
		fmt.Printf("Container output: %s\n", output)
	}
	
	// Check status
	status := iso.Status(ctx)
	fmt.Printf("Container running: %v, Exit code: %d\n", status.IsRunning, status.ExitCode)
}

// ExampleNetworkIsolation demonstrates network isolation capabilities
func ExampleNetworkIsolation() {
	logger := logrus.StandardLogger()
	
	iso, err := NewContainerIsolator(logger, "tenant-test", "exploit-network")
	if err != nil {
		logrus.Fatalf("Failed to create isolator: %v", err)
	}
	
	// Configure with restricted network access
	cfg := DefaultIsolationConfig()
	cfg.DisableNetwork = false
	cfg.ExposedPorts = []int{8080} // Only expose port 8080 on localhost
	cfg.BaseImage = "nginx:alpine"
	
	iso.SetConfig(cfg)
	
	ctx := context.Background()
	containerID, err := iso.StartContainer(ctx, []string{})
	if err != nil {
		logrus.Fatalf("Failed to start nginx container: %v", err)
	}
	
	defer func() {
		_ = iso.StopContainer(ctx, 5*time.Second)
		iso.cleanup(ctx)
	}()
	
	fmt.Printf("Nginx running at http://127.0.0.1:8080 in ID: %s\n", containerID)
}

// ExampleResourceLimits demonstrates resource limiting
func ExampleResourceLimits() {
	logger := logrus.StandardLogger()
	
	iso, err := safety_sandbox.NewContainerIsolator(logger, "tenant-research", "exploit-resource-limit")
	if err != nil {
		logrus.Fatalf("Failed to create isolator: %v", err)
	}
	
	// Set strict resource limits
	cfg := safety_sandbox.DefaultIsolationConfig()
	cfg.BaseImage = "alpine:3.19"
	cfg.CPUCQuota = 100000          // 1 CPU core max
	cfg.MemoryBytes = 256 << 20     // 256MB RAM
	cfg.TmpfsSizeBytes = 128 << 20  // 128MB tmpfs
	
	iso.SetConfig(cfg)
	
	ctx := context.Background()
	containerID, err := iso.StartContainer(ctx, []string{"sh", "-c", "cat /proc/meminfo | head -5"})
	if err != nil {
		logrus.Fatalf("Failed to start memory test: %v", err)
	}
	
	defer func() {
		_ = iso.StopContainer(ctx, 5*time.Second)
		iso.cleanup(ctx)
	}()
	
	output, _ := iso.GetContainerLogs(ctx)
	fmt.Printf("Memory info inside container:\n%s\n", output)
	fmt.Printf("Container ID: %s\n", containerID)
}

// ExamplePayloadExecution demonstrates executing actual exploits safely
func ExamplePayloadExecution() {
	logger := logrus.StandardLogger()
	
	// Check Docker availability
	available, msg := CheckDockerAvailability(context.Background())
	if !available {
		logrus.Warn(msg)
		
		// Use fallback executor if available
		fallback := NewFallbackExecutor(logger, DefaultIsolationConfig())
		_, err := fallback.Execute(context.Background(), "ls", "-la")
		if err != nil {
			logrus.Info("Fallback mode active - no real execution without Docker")
		}
		return
	}
	
	iso, err := NewContainerIsolator(logger, "tenant-attack-sim", "exploit-lab")
	if err != nil {
		logrus.Fatalf("Failed to create isolator: %v", err)
	}
	
	// Configure for payload testing
	cfg := &IsolationConfig{
		BaseImage:       "alpine:3.19",
		ExecutionTimeout: 2 * time.Minute,
		ReadonlyRootfs:  true,
		Command:         []string{""}, // Override entrypoint
		CapDrop:         []string{"ALL"},
		SecurityOpt:     []string{"no-new-privileges:true"},
		User:            "root",
	}
	
	iso.SetConfig(cfg)
	
	ctx := context.Background()
	
	// Execute a test payload (e.g., vulnerability scanner binary)
	_, err = iso.StartContainer(ctx, []string{"wget", "--spider", "http://example.com"})
	if err != nil {
		logrus.Infof("Wget test failed (may be expected): %v", err)
	}
	
	defer func() {
		_ = iso.StopContainer(ctx, 5*time.Second)
		iso.cleanup(ctx)
	}()
	
	output, _ := iso.GetContainerLogs(ctx)
	fmt.Printf("Payload output:\n%s\n", output)
}

// ExampleIntegrationWithExploitEngine shows integration with exploit engine
type ExploitEngineExample struct {
	iso        *safety_sandbox.ContainerIsolator
	logger     *logrus.Logger
	tenantID   string
	exploitID  string
}

func NewExploitEngineExample(tenantID, exploitID string) (*ExploitEngineExample, error) {
	logger := logrus.StandardLogger().WithFields(logrus.Fields{
		"tenant": tenantID,
		"exploit": exploitID,
	})
	
	iso, err := safety_sandbox.NewContainerIsolator(logger, tenantID, exploitID)
	if err != nil {
		return nil, fmt.Errorf("failed to create isolator: %w", err)
	}
	
	return &ExploitEngineExample{
		iso:       iso,
		logger:    logger,
		tenantID:  tenantID,
		exploitID: exploitID,
	}, nil
}

// ExecutePayload runs exploit payload in isolated container
func (ee *ExploitEngineExample) ExecutePayload(ctx context.Context, payload []byte, target string) (string, error) {
	ee.logger.WithFields(logrus.Fields{
		"target": target,
		"size": len(payload),
	}).Info("Executing payload in isolated container")
	
	// Configure container for this specific payload
	cfg := safety_sandbox.DefaultIsolationConfig()
	cfg.BaseImage = "alpine:3.19"
	cfg.ExecutionTimeout = 3 * time.Minute
	
	ee.iso.SetConfig(cfg)
	
	// Execute the payload (would typically write to temp file and run)
	containerID, err := ee.iso.StartContainer(ctx, []string{"/bin/sh", "-c", "echo 'Test payload execution'"})
	if err != nil {
		return "", fmt.Errorf("payload execution failed: %w", err)
	}
	
	// Get result
	output, err := ee.iso.GetContainerLogs(ctx)
	if err != nil {
		return containerID, fmt.Errorf("failed to get results: %w", err)
	}
	
	// Cleanup
	_ = ee.iso.StopContainer(ctx, 5*time.Second)
	ee.iso.cleanup(ctx)
	
	return output, nil
}

// Cleanup terminates any remaining containers
func (ee *ExploitEngineExample) Cleanup(ctx context.Context) {
	if ee.iso != nil && ee.iso.Status(ctx).ContainerID != "" {
		ee.iso.Kill(ctx)
	}
}
