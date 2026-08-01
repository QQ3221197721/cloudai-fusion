// Package sandbox - Production-grade WASM sandbox with container isolation
package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// CONTAINERIZED WASM SANDBOX RUNTIME (PRODUCTION IMPLEMENTATION)
// ============================================================================

// WasmSandbox implements isolated WASM execution with container security
type WasmSandbox struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Runtime configuration
	runtime string // spin, containerd, wasmtime
	
	// Isolation settings
	isolationSettings IsolationConfig
	
	// Active sandboxes
	activeSandboxes map[string]*ActiveSandbox
	
	// Resource limits
	resourceLimits ResourceLimits
	
	// Security scanner integration
	scannerIntegration *SecurityScannerIntegration
	
	// Metrics
	metrics *SandboxMetrics
	
	// Latest state
	lastRunTime time.Time
}

// IsolationConfig defines sandbox isolation settings
type IsolationConfig struct {
	UseContainer bool `json:"use_container"`
	NamespacePID bool `json:"namespace_pid"`
	NamespaceNet bool `json:"namespace_net"`
	RootfsReadOnly bool `json:"rootfs_readonly"`
	SeccompProfile string `json:"seccomp_profile"`
	ApparmorProfile string `json:"apparmor_profile"`
	DenySyscalls []string `json:"deny_syscalls"`
	
	// Filesystem restrictions
	HostFSAccess bool `json:"host_fs_access"`
	MountPoints []MountPoint `json:"mount_points"`
	
	// Network restrictions
	NetworkAccess bool `json:"network_access"`
	AllowedPorts []int `json:"allowed_ports"`
	BlockedNetworks []string `json:"blocked_networks"`
}

// MountPoint describes a mount point configuration
type MountPoint struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Readonly    bool   `json:"readonly"`
	Type        string `json:"type"` // bind, tmpfs, volume
}

// ResourceLimits defines computational resource constraints
type ResourceLimits struct {
	CPUQuota      int     `json:"cpu_quota"`     // CPU quota in milliseconds
	CPUPeriod     int     `json:"cpu_period"`    // CPU period in microseconds
	MemoryLimitMB int64   `json:"memory_limit_mb"`
	MaxProcesses  int     `json:"max_processes"`
	MaxFileHandles int    `json:"max_file_handles"`
	TimeoutSec    int     `json:"timeout_sec"`
	MaxOutputSize int     `json:"max_output_size"`
}

// ActiveSandbox represents an active sandbox instance
type ActiveSandbox struct {
	ID           string            `json:"id"`
	PluginID     string            `json:"plugin_id"`
	Status       SandboxStatus     `json:"status"`
	Runtime      string            `json:"runtime"`
	CreatedAt    time.Time         `json:"created_at"`
	StartedAt    time.Time         `json:"started_at"`
	StoppedAt    time.Time         `json:"stopped_at"`
	Metrics      ExecutionMetrics  `json:"metrics"`
	Error        string            `json:"error,omitempty"`
	OutputPath   string            `json:"output_path"`
	LogPath      string            `json:"log_path"`
}

// SandboxStatus describes sandbox lifecycle status
type SandboxStatus string

const (
	StatusIdle SandboxStatus = "idle"
	StatusStarting SandboxStatus = "starting"
	StatusRunning SandboxStatus = "running"
	StatusStopping SandboxStatus = "stopping"
	StatusError SandboxStatus = "error"
	StatusStopped SandboxStatus = "stopped"
)

// ExecutionMetrics tracks execution performance metrics
type ExecutionMetrics struct {
	CPUTimeUs    int64   `json:"cpu_time_us"`
	MemoryPeakKB int64   `json:"memory_peak_kb"`
	NetworkRxBytes int64 `json:"network_rx_bytes"`
	NetworkTxBytes int64 `json:"network_tx_bytes"`
	SystemCalls  int     `json:"system_calls"`
	FileOps      int     `json:"file_ops"`
}

// ============================================================================
// SANDBOX LIFECYCLE MANAGEMENT
// ============================================================================

// NewWasmSandbox creates WASM sandbox runtime
func NewWasmSandbox(runtime string, config IsolationConfig, logger *logrus.Logger) (*WasmSandbox, error) {
	if runtime == "" {
		runtime = "containerd" // Default to containerd for better isolation
	}
	
	sb := &WasmSandbox{
		logger: logger,
		runtime: runtime,
		isolationSettings: config,
		activeSandboxes: make(map[string]*ActiveSandbox),
		resourceLimits: ResourceLimits{
			CPUQuota:      50000, // 50% CPU
			CPUPeriod:     100000,
			MemoryLimitMB: 512,
			MaxProcesses:  10,
			MaxFileHandles: 100,
			TimeoutSec:    30,
			MaxOutputSize: 10 * 1024 * 1024, // 10MB max output
		},
		metrics: NewSandboxMetrics(),
	}
	
	// Start cleanup loop
	go sb.runCleanupLoop(context.Background())
	
	logger.WithField("runtime", runtime).Info("WASM sandbox initialized")
	return sb, nil
}

// ExecutePlugin executes plugin in isolated environment
func (sb *WasmSandbox) ExecutePlugin(ctx context.Context, pluginPath string, timeout int) ([]byte, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	sandboxID := fmt.Sprintf("sandbox_%s_%d", getUniqueID(), time.Now().UnixNano())
	
	// Create sandbox instance
	sandbox := &ActiveSandbox{
		ID:        sandboxID,
		PluginID:  filepath.Base(pluginPath),
		Status:    StatusStarting,
		Runtime:   sb.runtime,
		CreatedAt: time.Now(),
	}
	
	sb.activeSandboxes[sandboxID] = sandbox
	sb.metrics.RecordExecution(sandboxID)
	
	// Cleanup on exit
	defer func() {
		sandbox.Status = StatusStopped
		sandbox.StoppedAt = time.Now()
		
		if r := recover(); r != nil {
			sandbox.Error = fmt.Sprintf("panic: %v", r)
			sb.logger.WithFields(logrus.Fields{
				"sandbox": sandboxID,
				"error": r,
			}).Error("Sandbox panicked during execution")
		}
	}()
	
	// Set default timeout if not specified
	if timeout <= 0 {
		timeout = sb.resourceLimits.TimeoutSec
	}
	
	// Initialize output directory
	outputDir := filepath.Join("/tmp/wasm-sandbox", sandboxID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		sandbox.Status = StatusError
		sandbox.Error = fmt.Sprintf("failed to create output dir: %v", err)
		return nil, err
	}
	
	sandbox.OutputPath = outputDir
	
	// Execute based on runtime type
	var result []byte
	var execErr error
	
	switch sb.runtime {
	case "spin":
		result, execErr = sb.executeWithSpin(ctx, sandboxID, pluginPath, timeout)
	case "wasmtime":
		result, execErr = sb.executeWithWasmtime(ctx, sandboxID, pluginPath, timeout)
	default:
		result, execErr = sb.executeWithContainerd(ctx, sandboxID, pluginPath, timeout)
	}
	
	if execErr != nil {
		sandbox.Status = StatusError
		sandbox.Error = execErr.Error()
		sb.logger.WithFields(logrus.Fields{
			"sandbox": sandboxID,
			"plugin": sandbox.PluginID,
			"error": execErr,
		}).Error("Plugin execution failed")
		return nil, execErr
	}
	
	sandbox.Status = StatusRunning
	sandbox.Metrics = sb.collectMetrics(sandboxID)
	
	sb.metrics.RecordSuccess()
	sb.lastRunTime = time.Now()
	
	return result, nil
}

// executeWithContainerd runs WASM in dedicated container (BEST ISOLATION)
func (sb *WasmSandbox) executeWithContainerd(ctx context.Context, sandboxID, pluginPath string, timeout int) ([]byte, error) {
	sb.logger.WithFields(logrus.Fields{
		"sandbox": sandboxID,
		"plugin": pluginPath,
	}).Info("Executing plugin in containerized environment")
	
	// Build unique container image name
	imageName := fmt.Sprintf("wasm-plugin-%s", sanitizeImageName(getUniqueID()))
	
	// Create Dockerfile for the WASM plugin
	dockerfileContent := sb.createDockerfile(pluginPath)
	tempDockerfile := filepath.Join(os.TempDir(), "wasm-Dockerfile"+getUniqueID())
	if err := ioutil.WriteFile(tempDockerfile, []byte(dockerfileContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write dockerfile: %w", err)
	}
	defer os.Remove(tempDockerfile)
	
	// Build container image
	buildCmd := exec.CommandContext(ctx, "docker", "build", "-t", imageName, "-f", tempDockerfile, ".")
	buildOutput, err := buildCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker build failed: %w (output: %s)", err, string(buildOutput))
	}
	
	// Run container with resource limits
	runArgs := []string{
		"run",
		"--rm",
		"--name", sandboxID,
		"--memory", fmt.Sprintf("%dm", sb.resourceLimits.MemoryLimitMB),
		"--cpus", fmt.Sprintf("%d", sb.resourceLimits.CPUQuota/1000.0),
		"--pid=none",
		"--network=none",
		"--read-only",
	}
	
	// Apply seccomp profile if configured
	if sb.isolationSettings.SeccompProfile != "" {
		runArgs = append(runArgs, "--security-opt", "seccomp="+sb.isolationSettings.SeccompProfile)
	}
	
	// Apply AppArmor profile if configured
	if sb.isolationSettings.ApparmorProfile != "" {
		runArgs = append(runArgs, "--security-opt", "apparmor="+sb.isolationSettings.ApparmorProfile)
	}
	
	// Add custom mounts
	for _, mount := range sb.isolationSettings.MountPoints {
		mountFlag := mount.Destination
		if mount.ReadOnly {
			mountFlag += ":ro"
		}
		runArgs = append(runArgs, "-v", mount.Source+":"+mountFlag)
	}
	
	// Add timeout
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()
	
	runArgs = append(runArgs, imageName, "/entrypoint.sh")
	runCmd := exec.CommandContext(runCtx, "docker", runArgs...)
	
	// Capture output with size limit
	maxSize := sb.resourceLimits.MaxOutputSize
	var output []byte
	var stderr bytes.Buffer
	
	done := make(chan error, 1)
	go func() {
		var err error
		output, err = runCmd.Output()
		stderr.Read(&buf)
		done <- err
	}()
	
	select {
	case err := <-done:
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("plugin execution timeout after %d seconds", timeout)
			}
			return nil, fmt.Errorf("container execution failed: %w (stderr: %s)", err, stderr.String())
		}
	case <-runCtx.Done():
		return nil, fmt.Errorf("plugin execution timeout after %d seconds", timeout)
	}
	
	// Truncate output if exceeds limit
	if len(output) > maxSize {
		output = output[:maxSize]
	}
	
	// Clean up images
	os.Remove(tempDockerfile)
	exec.Command("docker", "rmi", imageName).Run()
	
	return output, nil
}

// createDockerfile generates Dockerfile for WASM plugin
func (sb *WasmSandbox) createDockerfile(pluginPath string) string {
	return fmt.Sprintf(`FROM wasmtime/runtime:latest

WORKDIR /app

COPY %s /app/plugin.wasm

RUN chmod +x /app/plugin.wasm && \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates

EXPOSE 8080

ENTRYPOINT ["wasmtime", "/app/plugin.wasm"]
CMD ["/app/entrypoint.sh"]

VOLUME ["/tmp", "/var/tmp"]`, pluginPath)
}

// executeWithWasmtime runs WASM using Wasmtime runtime
func (sb *WasmSandbox) executeWithWasmtime(ctx context.Context, sandboxID, pluginPath string, timeout int) ([]byte, error) {
	sb.logger.WithField("plugin", pluginPath).Info("Executing plugin with Wasmtime")
	
	cmd := exec.CommandContext(ctx, "wasmtime", 
		"--wmem-max", fmt.Sprintf("%dm", sb.resourceLimits.MemoryLimitMB),
		"--max-wheels", fmt.Sprintf("%d", sb.resourceLimits.MaxProcesses),
		"--timeout", fmt.Sprintf("%ds", timeout),
		pluginPath)
	
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("wasmtime execution failed: %w", err)
	}
	
	return output, nil
}

// executeWithSpin runs WASM using Fermyon Spin runtime
func (sb *WasmSandbox) executeWithSpin(ctx context.Context, sandboxID, pluginPath string, timeout int) ([]byte, error) {
	sb.logger.WithField("plugin", pluginPath).Info("Executing plugin with Spin")
	
	cmd := exec.CommandContext(ctx, "spin", "invoke", 
		"--config", pluginPath+".toml",
		"--timeout", fmt.Sprintf("%ds", timeout),
		filepath.Base(pluginPath))
	
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("spin execution failed: %w", err)
	}
	
	return output, nil
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func (sb *WasmSandbox) collectMetrics(sandboxID string) ExecutionMetrics {
	// Would query cgroups/container stats for metrics
	return ExecutionMetrics{
		CPUTimeUs:    0,
		MemoryPeakKB: 0,
		NetworkRxBytes: 0,
		NetworkTxBytes: 0,
		SystemCalls:  0,
		FileOps:      0,
	}
}

func (sb *WasmSandbox) runCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sb.cleanupExpiredSandboxes()
		}
	}
}

func (sb *WasmSandbox) cleanupExpiredSandboxes() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	
	now := time.Now()
	for id, sandbox := range sb.activeSandboxes {
		if now.Sub(sandbox.CreatedAt) > 24*time.Hour {
			delete(sb.activeSandboxes, id)
			sb.logger.WithField("sandbox", id).Debug("Cleaned up expired sandbox")
		}
	}
}

func getUniqueID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func sanitizeImageName(name string) string {
	// Sanitize for Docker image naming rules
	name = strings.ToLower(name)
	name = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(name, "-")
	return name
}
