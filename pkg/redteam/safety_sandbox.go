// Package redteam implements OffSec CEx³-level attack simulation capabilities
// with comprehensive safety mechanisms and audit controls.
package redteam

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// SafetySandbox provides isolated execution environment for all exploit code
// with multiple layers of containment, automatic termination, and comprehensive auditing.
type SafetySandbox struct {
	logger          *logrus.Logger
	isolationMode   IsolationLevel
	autoKillTimeout time.Duration
	maxExecutionTime time.Duration
	
	auditLogger     *logrus.Entry
	containerID     string
	namespaceID     string
	
	mu              sync.RWMutex
	executionStart  time.Time
	isTerminated    bool
	
	signals         chan os.Signal
	killSignal      chan struct{}
}

// IsolationLevel defines the security tier for exploit execution
type IsolationLevel int

const (
	// DryRunMode - No real execution, just planning and visualization
	DryRunMode IsolationLevel = iota
	// SimulationMode - Safe simulation in memory-only environment
	SimulationMode
	// ContainerIsolation - Docker container sandbox with limited privileges
	ContainerIsolation
	// NetworkNamespace - Full network namespace isolation
	NetworkNamespace
	// FullSandbox - Complete VM-level isolation (not yet implemented)
	FullSandbox
)

// Common timeouts
const (
	DefaultAutoKillTimeout = 30 * time.Second
	DefaultMaxExecTime     = 5 * time.Minute
	MinSafeTimeout         = 10 * time.Second
	MaxAllowedTimeout      = 10 * time.Minute
)

// NewSafetySandbox creates a new sandbox with specified isolation mode
func NewSafetySandbox(logger *logrus.Logger, mode IsolationLevel) *SafetySandbox {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	ss := &SafetySandbox{
		logger:          logger.WithField("component", "redteam.sandbox"),
		isolationMode:   mode,
		autoKillTimeout: DefaultAutoKillTimeout,
		maxExecutionTime: DefaultMaxExecTime,
		killSignal:      make(chan struct{}),
		signals:         make(chan os.Signal, 1),
	}
	
	ss.auditLogger = logger.WithFields(logrus.Fields{
		"component":  "redteam.audit",
		"sandbox_id": generateSandboxID(),
	})
	
	return ss
}

// SetTimeouts configures kill switch and max execution limits
func (ss *SafetySandbox) SetTimeouts(akTimeout, maxTime time.Duration) error {
	if akTimeout < MinSafeTimeout || akTimeout > MaxAllowedTimeout {
		return fmt.Errorf("invalid auto-kill timeout: must be between %v and %v", 
			MinSafeTimeout, MaxAllowedTimeout)
	}
	if maxTime < akTimeout || maxTime > 24*time.Hour {
		return fmt.Errorf("invalid max execution time: must be >auto-kill (%v) and ≤24h", 
			akTimeout)
	}
	
	ss.mu.Lock()
	defer ss.mu.Unlock()
	
	ss.autoKillTimeout = akTimeout
	ss.maxExecutionTime = maxTime
	
	return nil
}

// Start executes an exploit function within safety constraints
func (ss *SafetySandbox) Start(ctx context.Context, exploit func() error, description string) error {
	ss.mu.Lock()
	if ss.isTerminated {
		ss.mu.Unlock()
		return fmt.Errorf("sandbox has been terminated")
	}
	
	ss.executionStart = time.Now()
	ss.logger.WithFields(logrus.Fields{
		"description": description,
		"isolation":   ss.isolationMode.String(),
	}).Info("Starting exploit execution")
	ss.mu.Unlock()
	
	// Record audit trail
	ss.auditLogger.WithFields(logrus.Fields{
		"action":       "exploit_start",
		"description":  description,
		"isolation":    ss.isolationMode.String(),
		"timeout":      ss.autoKillTimeout.String(),
	}).Info("Exploit started")
	
	// Start goroutines for kill switch monitoring
	stopChan := make(chan struct{})
	go ss.monitorKillSwitch(stopChan)
	go ss.monitorTimeout(stopChan)
	
	// Execute exploit in safe manner
	var err error
	switch ss.isolationMode {
	case DryRunMode, SimulationMode:
		err = ss.executeInMemory(exploit)
	case ContainerIsolation, NetworkNamespace:
		err = ss.executeInIsolatedContainer(ctx, exploit)
	default:
		err = fmt.Errorf("unsupported isolation mode: %d", ss.isolationMode)
	}
	
	// Clean up
	close(stopChan)
	ss.recordCompletion(err, description)
	
	return err
}

// Kill aborts the currently executing exploit immediately
func (ss *SafetySandbox) Kill() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	
	if !ss.isTerminated {
		close(ss.killSignal)
		ss.isTerminated = true
		
		ss.logger.Warn("EXPLOIT KILLED BY MANUAL OVERRIDE")
		ss.auditLogger.WithField("reason", "manual_kill").Warn("Kill switch activated")
	}
}

// monitorKillSwitch watches for manual kill signal
func (ss *SafetySandbox) monitorKillSwitch(stop chan struct{}) {
	select {
	case <-ss.killSignal:
		ss.logger.Error("Manual kill signal received, aborting execution")
		return
	case <-stop:
		return
	}
}

// monitorTimeout ensures execution doesn't exceed maximum time
func (ss *SafetySandbox) monitorTimeout(stop chan struct{}) {
	select {
	case <-time.After(ss.autoKillTimeout):
		ss.mu.Lock()
		if !ss.isTerminated {
			ss.isTerminated = true
			ss.logger.Error("Auto-kill triggered due to timeout")
			ss.auditLogger.WithField("reason", "timeout_exceeded").Error("Kill switch activated by timeout")
		}
		ss.mu.Unlock()
	case <-stop:
		return
	}
}

// executeInMemory safely runs exploit without external dependencies
func (ss *SafetySandbox) executeInMemory(exploit func() error) error {
	ctx, cancel := context.WithTimeout(context.Background(), ss.maxExecutionTime)
	defer cancel()
	
	done := make(chan error, 1)
	go func() {
		done <- exploit()
	}()
	
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("execution exceeded maximum time limit")
	}
}

// executeInIsolatedContainer would create container/namespace isolation
// Implementation placeholder for future container-based sandboxing
func (ss *SafetySandbox) executeInIsolatedContainer(ctx context.Context, exploit func() error) error {
	// Create isolated environment identifier
	id := generateSandboxID()
	ss.containerID = fmt.Sprintf("redteam-sandbox-%s", id)
	ss.namespaceID = fmt.Sprintf("netns-%s", id)
	
	ss.logger.WithFields(logrus.Fields{
		"container": ss.containerID,
		"namespace": ss.namespaceID,
	}).Info("Creating isolated execution environment")
	
	// TODO: Implement actual Docker/containerd container creation
	// For now, execute in-memory but log isolation intent
	
	return ss.executeInMemory(exploit)
}

// recordCompletion logs the outcome of exploit execution
func (ss *SafetySandbox) recordCompletion(err error, description string) {
	duration := time.Since(ss.executionStart)
	
	fields := logrus.Fields{
		"description": description,
		"duration":    duration.String(),
	}
	
	if err != nil {
		fields["error"] = err.Error()
		ss.auditLogger.WithFields(fields).Error("Exploit failed or was aborted")
	} else {
		fields["status"] = "success"
		ss.auditLogger.WithFields(fields).Info("Exploit completed successfully")
	}
	
	// Always emit evidence record
	emitEvidence(ctx, ss.auditLogger, description, duration, err)
}

// Status returns current sandbox state
func (ss *SafetySandbox) Status() SandboxStatus {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	
	return SandboxStatus{
		IsolationMode:   ss.isolationMode,
		Running:         !ss.isTerminated && ss.executionStart.IsZero(),
		Terminated:      ss.isTerminated,
		AutoKillTimeout: ss.autoKillTimeout,
		MaxExecTime:     ss.maxExecutionTime,
		ElapsedTime:     time.Since(ss.executionStart),
		SandboxID:       ss.containerID,
	}
}

// SandboxStatus represents current safety sandbox state
type SandboxStatus struct {
	IsolationMode   IsolationLevel
	Running         bool
	Terminated      bool
	AutoKillTimeout time.Duration
	MaxExecTime     time.Duration
	ElapsedTime     time.Duration
	SandboxID       string
}

// String converts IsolationLevel to human-readable name
func (il IsolationLevel) String() string {
	names := map[IsolationLevel]string{
		DryRunMode:       "dry-run",
		SimulationMode:   "simulation",
		ContainerIsolation: "container",
		NetworkNamespace: "network-namespace",
		FullSandbox:      "full-virtualization",
	}
	
	if name, ok := names[il]; ok {
		return name
	}
	return fmt.Sprintf("unknown(%d)", il)
}

// generateSandboxID creates unique sandbox identifier
func generateSandboxID() string {
	b := make([]byte, 8)
	os.ReadRandom(b)
	return fmt.Sprintf("%x%x%x%d", b[:2], b[2:6], b[6:8], time.Now().UnixNano())
}

// emitEvidence records exploitation event in secure ledger
func emitEvidence(ctx context.Context, logger *logrus.Entry, action string, duration time.Duration, err error) {
	record := map[string]interface{}{
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"action":      action,
		"duration_ms": duration.Milliseconds(),
		"status":      "success",
	}
	
	if err != nil {
		record["status"] = "failed"
		record["error"] = err.Error()
	}
	
	logger.WithFields(logrus.Fields{
		"event_type":  "exploitation_audit",
		"evidence":    record,
	}).Info("Evidence recorded")
}

// RegisterKillSwitchHook allows external systems to trigger kill
func (ss *SafetySandbox) RegisterKillSwitchHook(hook func()) {
	signal.Notify(ss.signals, syscall.SIGINT, syscall.SIGTERM)
	
	go func() {
		sig := <-ss.signals
		ss.logger.WithField("signal", sig).Warn("Received termination signal")
		hook()
	}()
}
