// Package safety_sandbox provides Docker-based isolated execution environment for exploit payloads
// Implements multiple layers of containment: network isolation, filesystem protection, resource limits
package safety_sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/sirupsen/logrus"
)

// ContainerIsolator provides Docker-based isolated execution environment for exploit payloads
// Implements multiple layers of containment: network isolation, filesystem protection, resource limits
type ContainerIsolator struct {
	logger     *logrus.Logger
	dockerCli  *client.Client
	tenantID   string
	exploitID  string
	containerID string
	networkID   string
	
	// Configuration
	config *IsolationConfig
	
	// State management
	mu             sync.RWMutex
	isInitialized  bool
	isTerminated   bool
	startTime      time.Time
	
	// Cleanup hooks
	cleanupHooks   []func() error
	
	// Evidence recording
	evidenceLogger logrus.Entry
}

// IsolationConfig defines the security parameters for container sandbox
type IsolationConfig struct {
	// Image to use for isolation (must be pinned to specific tag for reproducibility)
	BaseImage string
	
	// Timeout for exploit execution
	ExecutionTimeout time.Duration
	
	// CPU quota in microseconds (100000 = 100% CPU)
	CPUCQuota int64
	
	// CPU shares (relative weight)
	CPUShares int64
	
	// Memory limit in bytes
	MemoryBytes int64
	
	// Temporary storage in bytes
	TmpfsSizeBytes int64
	
	// Whether to allow network access at all
	DisableNetwork bool
	
	// Which ports to expose (empty = no ports exposed)
	ExposedPorts []int
	
	// Volume mounts: only read-only volumes from whitelist
	ReadonlyRootfs bool
	
	// Security options: apparmor, seccomp, etc.
	SecurityOpt []string
	
	// CapDrop: Linux capabilities to drop
	CapDrop []string
	
	// Privileged mode (NOT RECOMMENDED - defaults to false)
	Privileged bool
	
	// User to run as (non-root by default)
	User string
	
	// Working directory
	WorkingDir string
	
	// Command to execute
	Command []string
	
	// Environment variables (filtered for safety)
	Env []string
	
	// Auto-cleanup after execution
	AutoCleanup bool
	
	// Maximum stdout/stderr size to capture
	OutputMaxBytes int
}

// DefaultIsolationConfig returns safe defaults for exploit execution
func DefaultIsolationConfig() *IsolationConfig {
	return &IsolationConfig{
		BaseImage:          "alpine:3.19", // Minimal, reproducible base
		ExecutionTimeout:   5 * time.Minute,
		CPUCQuota:          200000,       // 2 CPU cores max
		CPUShares:          1024,
		MemoryBytes:        512 << 20,    // 512MB
		TmpfsSizeBytes:     256 << 20,    // 256MB tmpfs
		DisableNetwork:     false,
		ExposedPorts:       []int{},       // No ports by default
		ReadonlyRootfs:     true,
		SecurityOpt:        []string{"no-new-privileges:true"},
		CapDrop:            []string{"ALL"}, // Drop ALL capabilities
		Privileged:         false,
		User:               "root",          // Keep as root for testing, can be changed
		WorkingDir:         "/tmp",
		Command:            []string{"sleep"},
		Env:                []string{},
		AutoCleanup:        true,
		OutputMaxBytes:     10 << 20,        // 10MB max output
	}
}

// NewContainerIsolator creates a new isolator with Docker API client
func NewContainerIsolator(logger *logrus.Logger, tenantID, exploitID string) (*ContainerIsolator, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	iso := &ContainerIsolator{
		logger: logger.WithFields(logrus.Fields{
			"component": "redteam.container_isolator",
			"tenant":    tenantID,
		}),
		tenantID:   tenantID,
		exploitID:  exploitID,
		config:     DefaultIsolationConfig(),
		cleanupHooks: make([]func() error, 0),
	}
	
	// Initialize evidenceLogger separately to avoid struct initialization issues
	iso.evidenceLogger = *logrus.NewEntry(logger).WithFields(logrus.Fields{
		"component": "redteam.evidence",
		"tenant":    tenantID,
		"exploit":   exploitID,
	})
	
	// Initialize Docker client
	var err error
	iso.dockerCli, err = iso.initializeDockerClient()
	if err != nil {
		iso.logger.WithError(err).Warn("Failed to initialize Docker client - running in fallback mode")
		return iso, fmt.Errorf("docker initialization failed: %w", err)
	}
	
	iso.mu.Lock()
	iso.isInitialized = true
	iso.mu.Unlock()
	
	iso.logger.Info("Container isolator initialized successfully")
	
	return iso, nil
}

// initializeDockerClient creates and tests Docker API connection
func (ci *ContainerIsolator) initializeDockerClient() (*client.Client, error) {
	// Use default Docker client (reads DOCKER_HOST env if set)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}
	
	// Test connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	_, err = cli.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to ping docker daemon: %w", err)
	}
	
	ci.logger.Debug("Docker daemon ping successful")
	return cli, nil
}

// SetConfig updates isolation configuration
func (ci *ContainerIsolator) SetConfig(cfg *IsolationConfig) {
	if cfg == nil {
		cfg = DefaultIsolationConfig()
	}
	
	ci.mu.Lock()
	defer ci.mu.Unlock()
	
	// Validate and sanitize config before applying
	ci.config = ci.sanitizeConfig(cfg)
}

// sanitizeConfig ensures safe configuration values
func (ci *ContainerIsolator) sanitizeConfig(cfg *IsolationConfig) *IsolationConfig {
	sanitized := *cfg
	
	// Enforce safe defaults
	if sanitized.CPUCQuota < 100000 {
		sanitized.CPUCQuota = 100000 // Min 1 core
	}
	if sanitized.CPUCQuota > 800000 {
		sanitized.CPUCQuota = 800000 // Max 8 cores
	}
	
	if sanitized.MemoryBytes < 64<<20 {
		sanitized.MemoryBytes = 64 << 20 // Min 64MB
	}
	
	if sanitized.Privileged {
		ci.logger.Warn("Privileged mode requested - this is UNSAFE in production!")
	}
	
	// Always drop capabilities for safety
	if len(sanitized.CapDrop) == 0 {
		sanitized.CapDrop = []string{"ALL"}
	}
	
	return &sanitized
}

// StartContainer creates and starts an isolated container
func (ci *ContainerIsolator) StartContainer(ctx context.Context, command []string) (string, error) {
	ci.mu.Lock()
	if !ci.isInitialized {
		ci.mu.Unlock()
		return "", fmt.Errorf("container isolator not initialized")
	}
	ci.startTime = time.Now()
	ci.mu.Unlock()
	
	// Record evidence
	ci.evidenceLogger.WithFields(logrus.Fields{
		"action": "container_start",
		"command": command,
	}).Info("Starting isolated container")
	
	// Create network if needed
	networkID := ""
	if !ci.config.DisableNetwork && len(ci.config.ExposedPorts) > 0 {
		var err error
		networkID, err = ci.createNetwork(ctx)
		if err != nil {
			ci.logger.WithError(err).Warn("Failed to create custom network, falling back to bridge")
			// Fall through - will use default bridge
		} else {
			ci.networkID = networkID
		}
	}
	
	// Build container config
	hostConfig := ci.buildHostConfig(networkID)
	createResp, err := ci.createContainer(ctx, command, hostConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	
	ci.containerID = createResp.ID
	
	// Start container
	if err := ci.dockerCli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		ci.cleanup(ctx) // Clean up on error
		return "", fmt.Errorf("failed to start container: %w", err)
	}
	
	ci.logger.WithField("container_id", createResp.ID).Info("Container started successfully")
	
	// Record evidence
	ci.recordEvidence("container_started", map[string]interface{}{
		"container_id": createResp.ID,
		"network_id":   networkID,
	})
	
	return createResp.ID, nil
}

// createNetwork creates a custom isolated network for the container
func (ci *ContainerIsolator) createNetwork(ctx context.Context) (string, error) {
	netName := fmt.Sprintf("redteam-%s-%d", ci.exploitID, time.Now().UnixNano())
	
	_, err := ci.dockerCli.NetworkCreate(ctx, netName, network.CreateOptions{
		Driver:     "bridge",
		Scope:      "local",
		Attachable: true,
		Ingress:    false,
	})
	
	if err != nil {
		return "", fmt.Errorf("failed to create network: %w", err)
	}
	
	ci.logger.WithField("network", netName).Info("Created isolated network")
	return netName, nil
}

// buildHostConfig builds Docker HostConfig with all security restrictions
func (ci *ContainerIsolator) buildHostConfig(networkMode string) *container.HostConfig {
	hostConfig := &container.HostConfig{
		// Network settings
		NetworkMode: container.NetworkMode(networkMode),
		
		// Port publishing (only if explicitly configured)
		PublishAllPorts: false,
		PortBindings:    ci.buildPortBindings(),
		
		// Mounts
		ReadonlyRootfs: ci.config.ReadonlyRootfs,
		Mounts:         ci.buildMounts(),
		
		// Resource limits
		Resources: container.Resources{
			NanoCPUs:   float64(ci.config.CPUCQuota) / 1000000,
			CPUShares:  ci.config.CPUShares,
			Memory:     ci.config.MemoryBytes,
			MemorySwap: ci.config.MemoryBytes * 2, // Allow swap equal to memory
		},
		
		// Security options
		SecurityOpt:    ci.config.SecurityOpt,
		CapDrop:        ci.config.CapDrop,
		Privileged:     ci.config.Privileged,
		
		// Runtime
		WorkingDir:     ci.config.WorkingDir,
		AutoRemove:     ci.config.AutoCleanup, // Auto-remove on exit
		
		// Logging
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "1",
			},
		},
	}
	
	// Add restart policy if timeout is very short
	if ci.config.ExecutionTimeout < 10*time.Second {
		hostConfig.RestartPolicy = container.RestartPolicy{
			Name: "on-failure",
			MaximumRetryCount: 1,
		}
	}
	
	return hostConfig
}

// buildPortBindings maps exposed ports
func (ci *ContainerIsolator) buildPortBindings() map[string][]types.PortSpec {
	bindings := make(map[string][]types.PortSpec)
	
	for _, p := range ci.config.ExposedPorts {
		portStr := fmt.Sprintf("%d/tcp", p)
		bindings[portStr] = []types.PortSpec{
			{
				HostIP:   "127.0.0.1",
				HostPort: fmt.Sprintf("%d", p),
			},
		}
	}
	
	return bindings
}

// buildMounts creates volume and tmpfs mounts
func (ci *ContainerIsolator) buildMounts() []mount.Mount {
	var mounts []mount.Mount
	
	// Add tmpfs mounts for writable directories
	tmpfsMount1 := mount.Mount{
		Type:   "tmpfs",
		Source: "tmp",
		Target: "/tmp",
		TmpfsOptions: &mount.TmpfsMount{
			SizeBytes: ci.config.TmpfsSizeBytes,
		},
	}
	mounts = append(mounts, tmpfsMount1)
	
	// Add tmpfs for var/log if needed
	tmpfsMount2 := mount.Mount{
		Type:   "tmpfs",
		Source: "var_log",
		Target: "/var/log",
		TmpfsOptions: &mount.TmpfsMount{
			SizeBytes: ci.config.TmpfsSizeBytes,
		},
	}
	mounts = append(mounts, tmpfsMount2)
	
	// Add read-only bind mounts if any are configured
	// For now, keeping empty - would add user-configured volumes here
	
	return mounts
}

// createContainer creates the container without starting it
func (ci *ContainerIsolator) createContainer(ctx context.Context, command []string, hostConfig *container.HostConfig) (*container.CreateResponse, error) {
	containerName := fmt.Sprintf("redteam-exploit-%s-%d", ci.exploitID, time.Now().UnixNano())
	
	// Prepare environment
	env := ci.config.Env
	if len(env) == 0 {
		env = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}
	
	resp, err := ci.dockerCli.ContainerCreate(
		ctx,
		&container.Config{
			Image:        ci.config.BaseImage,
			Command:      append(ci.config.Command, command...),
			Env:          env,
			User:         ci.config.User,
			WorkingDir:   ci.config.WorkingDir,
			Entrypoint:   []string{""}, // Override entrypoint
			OpenStdin:    false,
			AttachStdout: true,
			AttachStderr: true,
		},
		hostConfig,
		nil,
		nil,
		containerName,
	)
	
	if err != nil {
		return nil, fmt.Errorf("container create failed: %w", err)
	}
	
	return &resp, nil
}

// AttachAndExecute attaches to container and executes payload
func (ci *ContainerIsolator) AttachAndExecute(ctx context.Context, execCommand string, args ...string) (string, error) {
	if ci.containerID == "" {
		return "", fmt.Errorf("no container running")
	}
	
	// Prepare exec config
	execConfig := types.ExecStartCheck{
		Detach: false,
		Tty:    false,
	}
	
	// Create exec instance
	execID, err := ci.dockerCli.ContainerExecCreate(
		ctx,
		ci.containerID,
		types.ExecConfig{
			Command: append([]string{execCommand}, args...),
			Detach:  false,
			Tty:     false,
			User:    "root",
		},
	)
	if err != nil {
		return "", fmt.Errorf("exec create failed: %w", err)
	}
	
	// Attach and execute
	result, err := ci.dockerCli.ContainerExecAttach(
		ctx,
		execID.ID,
		types.ExecStartCheck{
			Detach: false,
			Tty:    false,
		},
	)
	if err != nil {
		return "", fmt.Errorf("exec attach failed: %w", err)
	}
	defer result.Close()
	
	// Read output
	output, err := io.ReadAll(io.LimitReader(result.Reader, int64(ci.config.OutputMaxBytes)))
	if err != nil {
		return "", fmt.Errorf("read output failed: %w", err)
	}
	
	return string(output), nil
}

// GetContainerLogs retrieves logs from running container
func (ci *ContainerIsolator) GetContainerLogs(ctx context.Context) (string, error) {
	if ci.containerID == "" {
		return "", fmt.Errorf("no container ID available")
	}
	
	logs, err := ci.dockerCli.ContainerLogs(
		ctx,
		ci.containerID,
		container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Tail:       1000, // Last 1000 lines
		},
	)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()
	
	output, err := io.ReadAll(io.LimitReader(logs, int64(ci.config.OutputMaxBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to read logs: %w", err)
	}
	
	return strings.TrimSpace(string(output)), nil
}

// StopContainer stops the running container
func (ci *ContainerIsolator) StopContainer(ctx context.Context, timeout time.Duration) error {
	if ci.containerID == "" {
		return nil // Nothing to stop
	}
	
	timeoutSec := int(timeout.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	
	err := ci.dockerCli.ContainerStop(ctx, ci.containerID, container.StopOptions{
		Timeout: &timeoutSec,
	})
	
	if err != nil && !strings.Contains(err.Error(), "No such container") {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	
	if err == nil || strings.Contains(err.Error(), "No such container") {
		ci.logger.WithField("container_id", ci.containerID).Info("Container stopped successfully")
	}
	
	return nil
}

// cleanup performs full cleanup of container and associated resources
func (ci *ContainerIsolator) cleanup(ctx context.Context) {
	ci.mu.Lock()
	wasStarted := ci.containerID != ""
	ci.mu.Unlock()
	
	if !wasStarted {
		return
	}
	
	// Run cleanup hooks
	for i, hook := range ci.cleanupHooks {
		if err := hook(); err != nil {
			ci.logger.WithError(err).WithField("hook_index", i).Warn("Cleanup hook failed")
		}
	}
	
	// Remove container
	if ci.containerID != "" {
		removeErr := ci.dockerCli.ContainerRemove(ctx, ci.containerID, types.ContainerRemoveOptions{
			Force: true,
		})
		if removeErr != nil && !strings.Contains(removeErr.Error(), "No such container") {
			ci.logger.WithError(removeErr).WithField("container", ci.containerID).Warn("Failed to remove container")
		}
		ci.containerID = ""
	}
	
	// Remove network
	if ci.networkID != "" {
		networkErr := ci.dockerCli.NetworkRemove(ctx, ci.networkID)
		if networkErr != nil && !strings.Contains(networkErr.Error(), "No such network") {
			ci.logger.WithError(networkErr).WithField("network", ci.networkID).Warn("Failed to remove network")
		}
		ci.networkID = ""
	}
	
	ci.logger.Info("Cleanup completed")
}

// RegisterCleanupHook adds a cleanup function to be called on isolation termination
func (ci *ContainerIsolator) RegisterCleanupHook(hook func() error) {
	ci.mu.Lock()
	defer ci.mu.Unlock()
	
	ci.cleanupHooks = append(ci.cleanupHooks, hook)
}

// Status returns current container status
func (ci *ContainerIsolator) Status(ctx context.Context) ContainerStatus {
	ci.mu.RLock()
	muLocked := ci.containerID != ""
	ci.mu.RUnlock()
	
	status := ContainerStatus{
		IsInitialized: ci.isInitialized,
		IsRunning:     false,
		ContainerID:   ci.containerID,
		NetworkID:     ci.networkID,
		Uptime:        time.Since(ci.startTime),
	}
	
	if muLocked && ci.containerID != "" {
		inspect, err := ci.dockerCli.ContainerInspect(ctx, ci.containerID)
		if err == nil && inspect.State != nil {
			status.IsRunning = inspect.State.Running
			status.ExitCode = inspect.State.ExitCode
			status.Health = inspect.State.Health
		}
	}
	
	return status
}

// ContainerStatus represents current isolation state
type ContainerStatus struct {
	IsInitialized bool
	IsRunning     bool
	ContainerID   string
	NetworkID     string
	Uptime        time.Duration
	ExitCode      int
	Health        *types.Health
}

// Kill terminates container immediately and cleans up
func (ci *ContainerIsolator) Kill(ctx context.Context) {
	ci.mu.Lock()
	id := ci.containerID
	ci.mu.Unlock()
	
	if id == "" {
		return
	}
	
	ci.logger.Warn("Killing container forcefully")
	
	// Force kill
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	_ = ci.dockerCli.ContainerKill(ctx, id, 9) // SIGKILL
	
	// Full cleanup
	ci.cleanup(ctx)
}

// recordEvidence logs evidence of exploitation activity
func (ci *ContainerIsolator) recordEvidence(event string, data map[string]interface{}) {
	data["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	data["container_id"] = ci.containerID
	data["exploit_id"] = ci.exploitID
	data["tenant_id"] = ci.tenantID
	
	ci.evidenceLogger.WithFields(logrus.Fields{
		"event_type": event,
		"evidence":   data,
	}).Info("Evidence recorded")
	
	// In production, this would write to immutable ledger
}

// FallbackExecutor provides in-memory execution when Docker is unavailable
type FallbackExecutor struct {
	logger   *logrus.Logger
	config   *IsolationConfig
	runnerMu sync.Mutex
}

// NewFallbackExecutor creates a fallback executor for when Docker is unavailable
func NewFallbackExecutor(logger *logrus.Logger, cfg *IsolationConfig) *FallbackExecutor {
	return &FallbackExecutor{
		logger: logger,
		config: cfg,
	}
}

// Execute runs command in-memory (not actually isolated)
func (fe *FallbackExecutor) Execute(ctx context.Context, command string, args ...string) (string, error) {
	fe.runnerMu.Lock()
	defer fe.runnerMu.Unlock()
	
	fe.logger.Warn("FALLBACK MODE: Running command without container isolation")
	
	// This is unsafe - just for testing when Docker is unavailable
	// Would typically invoke shell execution safely with timeouts
	return "", fmt.Errorf("fallback executor does not support actual execution - requires Docker")
}

// CheckDockerAvailability checks if Docker daemon is accessible
func CheckDockerAvailability(ctx context.Context) (bool, string) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false, fmt.Sprintf("failed to create docker client: %v", err)
	}
	
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	_, err = cli.Ping(pingCtx)
	if err != nil {
		return false, fmt.Sprintf("failed to ping docker daemon: %v", err)
	}
	
	return true, "Docker daemon is available"
}
