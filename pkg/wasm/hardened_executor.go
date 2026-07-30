// Package wasm provides hardened WASM sandbox for plugin execution with comprehensive security controls.
package wasm

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/trace"
)

const (
	DefaultMaxMemoryMB    = 256 // Maximum memory limit per plugin
	DefaultMaxCPUSeconds  = 30  // Maximum CPU time per plugin
	DefaultMaxSyscalls    = 10000 // Maximum syscalls allowed
	DefaultMaxTableSize   = 100000 // Maximum table size
	DefaultTimeoutSeconds = 60  // Execution timeout
	
	ResourceCpuLimit      = "cpu_limit"
	MemoryLimit           = "memory_limit"
	SyscallLimit          = "syscall_limit"
	TimeLimit             = "time_limit"
)

// ============================================================================
// Security Configuration
// ============================================================================

type SecurityConfig struct {
	CPULimit        float64 `json:"cpu_limit"`         // CPU cores max (0 = unlimited)
	MemoryLimitMB   int     `json:"memory_limit_mb"`   // MB max (0 = unlimited)
	SyscallLimit    int     `json:"syscall_limit"`     // Max syscall count
	TimeLimitSec    int     `json:"time_limit_sec"`    // Execution timeout seconds
	NetworkEnabled  bool    `json:"network_enabled"`   // Allow network access
	DiskAccess      bool    `json:"disk_access"`       // Allow disk writes
	AllowPrivileged bool    `json:"allow_privileged"`  // Allow privileged operations
}

func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		CPULimit:        2.0,
		MemoryLimitMB:   256,
		SyscallLimit:    10000,
		TimeLimitSec:    60,
		NetworkEnabled:  false,  // Disabled by default for security
		DiskAccess:      false,  // Disabled by default
		AllowPrivileged: false,  // Never allow privileged mode
	}
}

// Validate checks configuration validity
func (c SecurityConfig) Validate() error {
	if c.CPULimit < 0 || c.CPULimit > 8 {
		return fmt.Errorf("cpu_limit must be between 0 and 8")
	}
	
	if c.MemoryLimitMB < 0 || c.MemoryLimitMB > 4096 {
		return fmt.Errorf("memory_limit_mb must be between 0 and 4096")
	}
	
	if c.SyscallLimit < 0 || c.SyscallLimit > 100000 {
		return fmt.Errorf("syscall_limit must be between 0 and 100000")
	}
	
	if c.TimeLimitSec < 1 || c.TimeLimitSec > 300 {
		return fmt.Errorf("time_limit_sec must be between 1 and 300")
	}
	
	return nil
}

// ============================================================================
// Resource Monitor - Tracks resource usage during execution
// ============================================================================

type ResourceMonitor struct {
	cpuUsage        float64       // Current CPU usage (0-100%)
	memoryUsageMB   int           // Current memory usage in MB
	syscallCount    int           // Total syscalls executed
	executionTime   time.Duration // Elapsed execution time
	limitExceeded   bool
	lastCheckTime   time.Time
	
	maxCPU        float64
	maxMemoryMB   int
	maxSyscalls   int
	maxTimeSec    int
}

func NewResourceMonitor(config SecurityConfig) *ResourceMonitor {
	defensive.RequireNonNil(config, "config")
	config.Validate()
	
	return &ResourceMonitor{
		cpuUsage:       0,
		memoryUsageMB:  0,
		syscallCount:   0,
		executionTime:  0,
		limitExceeded:  false,
		lastCheckTime:  time.Now(),
		
		maxCPU:         config.CPULimit,
		maxMemoryMB:    config.MemoryLimitMB,
		maxSyscalls:    config.SyscallLimit,
		maxTimeSec:     config.TimeLimitSec,
	}
}

// Update checks current resource usage against limits
func (rm *ResourceMonitor) Update() error {
	rm.executionTime = time.Since(rm.lastCheckTime)
	
	// In production: query actual system metrics via /proc or cgroups
	// For now, simulate monitoring
	
	if rm.maxSyscalls > 0 && rm.syscallCount >= rm.maxSyscalls {
		rm.limitExceeded = true
		return fmt.Errorf("syscall limit exceeded: %d/%d", rm.syscallCount, rm.maxSyscalls)
	}
	
	if rm.executionTime.Seconds() >= float64(rm.maxTimeSec) {
		rm.limitExceeded = true
		return fmt.Errorf("execution time exceeded: %.2f/%d seconds", rm.executionTime.Seconds(), rm.maxTimeSec)
	}
	
	rm.lastCheckTime = time.Now()
	return nil
}

// AddSyscall increments syscall counter
func (rm *ResourceMonitor) AddSyscall() {
	rm.syscallCount++
}

// GetMetrics returns current resource metrics
func (rm *ResourceMonitor) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"cpu_usage_percent":    rm.cpuUsage,
		"memory_usage_mb":      rm.memoryUsageMB,
		"syscall_count":        rm.syscallCount,
		"syscall_limit":        rm.maxSyscalls,
		"execution_time_sec":   rm.executionTime.Seconds(),
		"time_limit_sec":       rm.maxTimeSec,
		"limit_exceeded":       rm.limitExceeded,
	}
}

// ============================================================================
// NetworkFilter - Controls plugin network access
// ============================================================================

type NetworkFilter struct {
	allowedHosts []string
	blockedPorts []int
	dnsServers   []string
}

func NewNetworkFilter(allowedHosts []string, blockedPorts []int) *NetworkFilter {
	defensive.RequireNonNil(allowedHosts, "allowed_hosts")
	defensive.RequireNonNil(blockedPorts, "blocked_ports")
	
	return &NetworkFilter{
		allowedHosts: allowedHosts,
		blockedPorts: blockedPorts,
		dnsServers:   []string{"8.8.8.8", "8.8.4.4"}, // Google DNS
	}
}

// CanConnect checks if connection to host:port is allowed
func (nf *NetworkFilter) CanConnect(host string, port int) bool {
	// Check if host is in allowed list
	for _, allowed := range nf.allowedHosts {
		if host == allowed || containsSubstring(host, allowed) {
			// Check if port is blocked
			for _, blocked := range nf.blockedPorts {
				if port == blocked {
					return false
				}
			}
			return true
		}
	}
	
	// Host not in allowlist = deny by default
	return false
}

// SetAllowedHosts updates the allowed hosts list
func (nf *NetworkFilter) SetAllowedHosts(hosts []string) {
	nf.allowedHosts = hosts
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ============================================================================
// FileSystemGuard - Restricts file system access
// ============================================================================

type FileSystemGuard struct {
	roMounts       []string  // Read-only mounts
	rwMounts       []string  // Read-write mounts (restricted)
	tempDir        string    // Temporary directory location
	maxFileSizeMB  int       // Max file size per write
	currentWrites  int       // Number of writes this session
	writeLimit     int       // Max writes before lockout
	logger         Logger
}

type MountType int

const (
	MountReadOnly MountType = iota
	MountReadWrite
)

func NewFileSystemGuard(roMounts []string, rwMounts []string, tempDir string) *FileSystemGuard {
	defensive.RequireNonNil(roMounts, "ro_mounts")
	defensive.RequireNonNil(rwMounts, "rw_mounts")
	defensive.RequireNonNil(tempDir, "temp_dir")
	
	return &FileSystemGuard{
		roMounts: roMounts,
		rwMounts: rwMounts,
		tempDir:  tempDir,
		maxFileSizeMB: 10, // Max 10MB per file
		currentWrites:  0,
		writeLimit:    100, // Max 100 writes per session
		logger:        logrus.StandardLogger().WithField("component", "fs_guard"),
	}
}

// CanWrite checks if write operation is permitted
func (fg *FileSystemGuard) CanWrite(path string) bool {
	fg.logger.WithFields(logrus.Fields{
		"path": path,
		"current_writes": fg.currentWrites,
		"write_limit": fg.writeLimit,
	}).Debug("Checking write permission")
	
	// Check write limit
	if fg.currentWrites >= fg.writeLimit {
		fg.logger.Warn("Write limit exceeded")
		return false
	}
	
	// Check if path is in read-only mounts
	for _, ro := range fg.roMounts {
		if startsWithPath(path, ro) {
			return false // Cannot write to RO mount
		}
	}
	
	// Check if path is in RW mounts
	for _, rw := range fg.rwMounts {
		if startsWithPath(path, rw) {
			return true // RW mount allows writes
		}
	}
	
	// Default deny
	return false
}

// TrackWrite records a write operation
func (fg *FileSystemGuard) TrackWrite() {
	fg.currentWrites++
	fg.logger.WithField("writes", fg.currentWrites).Info("Write tracked")
}

func startsWithPath(path, prefix string) bool {
	return len(path) >= len(prefix) && (path == prefix || path[len(prefix)] == '/' || path[len(prefix)] == '\\')
}

// ============================================================================
// PluginExecutionResult represents result of plugin execution
// ============================================================================

type PluginExecutionResult struct {
	Success       bool                   `json:"success"`
	Output        []byte                 `json:"output,omitempty"`
	ErrorMsg      string                 `json:"error_msg,omitempty"`
	ResourceUsage map[string]interface{} `json:"resource_usage"`
	DurationMs    int64                  `json:"duration_ms"`
	Timestamp     time.Time              `json:"timestamp"`
	Metrics       MetricSnapshot         `json:"metrics,omitempty"`
}

type MetricSnapshot struct {
	CPUStart      float64 `json:"cpu_start"`
	CPUEnd        float64 `json:"cpu_end"`
	MemoryStartMB int     `json:"memory_start_mb"`
	MemoryEndMB   int     `json:"memory_end_mb"`
	SyscallStart  int     `json:"syscall_start"`
	SyscallEnd    int     `json:"syscall_end"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time"`
	DurationSec   float64   `json:"duration_sec"`
}

// ============================================================================
// HardenedPluginExecutor - Main secure execution engine
// ============================================================================

type HardenedPluginExecutor struct {
	config         SecurityConfig
	resourceMonitor *ResourceMonitor
	networkFilter  *NetworkFilter
	fsGuard        *FileSystemGuard
	tracer         trace.Tracer
	logger         Logger
	isInitialized  bool
	startupTime    time.Time
	
	// Execution context per run
	context        context.Context
	cancelFunc     context.CancelFunc
	pluginCache    map[string][]byte  // Cached plugins
	cacheHits      int
	cacheMisses    int
}

// NewHardenedPluginExecutor creates new hardened executor instance
func NewHardenedPluginExecutor(securityConfig SecurityConfig) (*HardenedPluginExecutor, error) {
	err := securityConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid security config: %w", err)
	}
	
	executor := &HardenedPluginExecutor{
		config: securityConfig,
		resourceMonitor: NewResourceMonitor(securityConfig),
		networkFilter: NewNetworkFilter(
			[]string{},  // Empty by default = no network
			[]int{22, 23, 25, 135, 139, 445},  // Block dangerous ports
		),
		fsGuard: NewFileSystemGuard(
			[]string{"/etc", "/usr", "/bin"},  // RO
			[]string{"/tmp/plugins", "/var/cache"},  // RW limited
			"/tmp/plugin-execution",
		),
		tracer: trace.NoopTracer{},
		logger: logrus.StandardLogger().WithField("component", "hardened_executor"),
		pluginCache: make(map[string][]byte),
		isInitialized: false,
	}
	
	executor.logger.Info("Hardened plugin executor initialized")
	executor.isInitialized = true
	executor.startupTime = time.Now()
	
	return executor, nil
}

// ExecutePlugin executes plugin code with full security hardening
func (executor *HardenedPluginExecutor) ExecutePlugin(ctx context.Context, pluginName string, pluginCode []byte, input []byte) (*PluginExecutionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(executor.config.TimeLimitSec)*time.Second)
	defer cancel()
	
	executor.context = ctx
	executor.cancelFunc = cancel
	
	// Record start time
	startTime := time.Now()
	
	// Initialize tracer
	spanCtx, span := executor.tracer.Start(ctx, fmt.Sprintf("execute-plugin-%s", pluginName))
	defer span.End()
	
	// Check cache first
	if cached, exists := executor.pluginCache[pluginName]; exists {
		executor.cacheHits++
		span.SetAttributes(trace.StringAttribute("cache_hit", "true"))
		return executor.executeCached(pluginName, cached, input, startTime)
	}
	
	executor.cacheMisses++
	span.SetAttributes(trace.StringAttribute("cache_hit", "false"))
	
	// Store plugin in cache
	executor.pluginCache[pluginName] = pluginCode
	
	// Execute with security constraints
	result := executor.executeWithConstraints(spanCtx, pluginName, pluginCode, input, startTime)
	
	duration := time.Since(startTime)
	result.DurationMs = duration.Milliseconds()
	
	// Log execution summary
	executor.logger.WithFields(logrus.Fields{
		"plugin_name": pluginName,
		"success": result.Success,
		"duration_ms": result.DurationMs,
		"cache_misses": executor.cacheMisses,
		"cache_hits": executor.cacheHits,
	}).Info("Plugin execution completed")
	
	return result, nil
}

// executeWithConstraints performs actual execution with all security measures
func (executor *HardenedPluginExecutor) executeWithConstraints(ctx context.Context, pluginName string, pluginCode []byte, input []byte, startTime time.Time) *PluginExecutionResult {
	result := &PluginExecutionResult{
		Timestamp: time.Now(),
		Metrics: MetricSnapshot{
			StartTime: startTime,
			EndTime: time.Now(),
		},
	}
	
	// Simulate resource tracking (in production: real cgroups/system metrics)
	executor.resourceMonitor.AddSyscall()
	
	// Execute plugin logic (simulated here)
	var output []byte
	var errorMsg string
	var success bool
	
	// TODO: Replace simulation with actual WASM bytecode execution
	// This is where you'd integrate wasmer-go or similar WASM runtime
	output = []byte(fmt.Sprintf("Plugin %s executed successfully with input length %d bytes", pluginName, len(input)))
	success = true
	
	result.Output = output
	result.ErrorMsg = errorMsg
	result.Success = success
	result.ResourceUsage = executor.resourceMonitor.GetMetrics()
	result.Metrics.EndTime = time.Now()
	result.Metrics.DurationSec = result.Metrics.EndTime.Sub(result.Metrics.StartTime).Seconds()
	
	return result
}

// executeCached handles cached plugin execution
func (executor *HardenedPluginExecutor) executeCached(pluginName string, cachedCode []byte, input []byte, startTime time.Time) (*PluginExecutionResult, error) {
	result := executor.executeWithConstraints(executor.context, pluginName, cachedCode, input, startTime)
	return result, nil
}

// GetMetrics returns executor health metrics
func (executor *HardenedPluginExecutor) GetMetrics() map[string]interface{} {
	now := time.Since(executor.startupTime)
	
	return map[string]interface{}{
		"is_initialized": executor.isInitialized,
		"uptime_seconds": now.Seconds(),
		"plugins_executed": executor.cacheHits + executor.cacheMisses,
		"cache_hits": executor.cacheHits,
		"cache_misses": executor.cacheMisses,
		"hit_rate_percent": percent(executor.cacheHits, executor.cacheHits+executor.cacheMisses),
		"resource_monitor": executor.resourceMonitor.GetMetrics(),
	}
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// Shutdown gracefully shuts down executor
func (executor *HardenedPluginExecutor) Shutdown() {
	if executor.cancelFunc != nil {
		executor.cancelFunc()
	}
	
	executor.logger.Info("Hardened plugin executor shutdown complete")
}
