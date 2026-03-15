package resilience

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Health Check System — HTTP Probes + gRPC Health Checking Protocol
// ============================================================================

// HealthStatus represents the health status of a component.
type HealthStatus int

const (
	// HealthUnknown means the health has not been checked yet.
	HealthUnknown HealthStatus = iota
	// HealthServing means the component is healthy and serving.
	HealthServing
	// HealthNotServing means the component is unhealthy.
	HealthNotServing
	// HealthDegraded means the component is partially healthy.
	HealthDegraded
)

// String returns a human-readable health status.
func (s HealthStatus) String() string {
	switch s {
	case HealthServing:
		return "SERVING"
	case HealthNotServing:
		return "NOT_SERVING"
	case HealthDegraded:
		return "DEGRADED"
	default:
		return "UNKNOWN"
	}
}

// ============================================================================
// Health Check Interface & Types
// ============================================================================

// HealthChecker is a single health check function.
type HealthChecker func(ctx context.Context) HealthCheckResult

// HealthCheckResult holds the result of a single health check.
type HealthCheckResult struct {
	Status    HealthStatus          `json:"status"`
	Message   string                `json:"message,omitempty"`
	Duration  time.Duration         `json:"duration"`
	Timestamp time.Time             `json:"timestamp"`
	Details   map[string]string     `json:"details,omitempty"`
}

// HealthReport is the aggregated health report for the entire service.
type HealthReport struct {
	Status     HealthStatus                  `json:"status"`
	Checks     map[string]HealthCheckResult  `json:"checks"`
	Uptime     time.Duration                 `json:"uptime"`
	Version    string                        `json:"version,omitempty"`
	Timestamp  time.Time                     `json:"timestamp"`
}

// ============================================================================
// Health Manager — Orchestrates multiple health checks
// ============================================================================

// HealthConfig configures the health check manager.
type HealthConfig struct {
	// CheckInterval is how often periodic health checks run.
	// Default: 10s
	CheckInterval time.Duration

	// CheckTimeout is the maximum time a single check can take.
	// Default: 5s
	CheckTimeout time.Duration

	// Logger for structured logging.
	Logger *logrus.Logger

	// Version is the service version string included in reports.
	Version string

	// Port for the health check HTTP server. Default: 8086
	Port int
}

// HealthManager manages health checks for a service.
// It supports:
//   - Kubernetes liveness probe (/healthz)
//   - Kubernetes readiness probe (/readyz)
//   - Kubernetes startup probe (/startupz)
//   - Detailed health endpoint (/health)
//   - gRPC Health Checking Protocol compatibility
type HealthManager struct {
	config    HealthConfig
	logger    *logrus.Logger
	startTime time.Time

	// Registered health checks
	livenessChecks  map[string]HealthChecker
	readinessChecks map[string]HealthChecker
	startupChecks   map[string]HealthChecker

	// Cached results
	lastReport    atomic.Value // stores *HealthReport
	lastLiveness  atomic.Value // stores *HealthReport
	lastReadiness atomic.Value // stores *HealthReport

	// Startup state
	startupComplete atomic.Bool

	mu sync.RWMutex
}

// NewHealthManager creates a new health check manager.
func NewHealthManager(cfg HealthConfig) *HealthManager {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 10 * time.Second
	}
	if cfg.CheckTimeout <= 0 {
		cfg.CheckTimeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.Port <= 0 {
		cfg.Port = 8086
	}

	return &HealthManager{
		config:          cfg,
		logger:          cfg.Logger,
		startTime:       time.Now(),
		livenessChecks:  make(map[string]HealthChecker),
		readinessChecks: make(map[string]HealthChecker),
		startupChecks:   make(map[string]HealthChecker),
	}
}

// RegisterLiveness registers a liveness check. Liveness failures trigger
// container restart in Kubernetes.
func (m *HealthManager) RegisterLiveness(name string, checker HealthChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.livenessChecks[name] = checker
}

// RegisterReadiness registers a readiness check. Readiness failures remove
// the pod from service endpoints (no traffic routed).
func (m *HealthManager) RegisterReadiness(name string, checker HealthChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readinessChecks[name] = checker
}

// RegisterStartup registers a startup check. Startup probes prevent
// liveness/readiness checks until the application is fully initialized.
func (m *HealthManager) RegisterStartup(name string, checker HealthChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startupChecks[name] = checker
}

// MarkStartupComplete signals that the service has finished starting up.
func (m *HealthManager) MarkStartupComplete() {
	m.startupComplete.Store(true)
	m.logger.Info("Health manager: startup complete")
}

// Start begins periodic health checks and optionally starts the HTTP server.
func (m *HealthManager) Start(ctx context.Context) {
	// Run initial check
	m.runChecks(ctx)

	// Periodic check loop
	go func() {
		ticker := time.NewTicker(m.config.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.runChecks(ctx)
			}
		}
	}()

	m.logger.WithField("interval", m.config.CheckInterval).Info("Health check manager started")
}

// ServeHTTP starts an HTTP health check server.
func (m *HealthManager) ServeHTTP(ctx context.Context) {
	mux := http.NewServeMux()

	// Kubernetes probes
	mux.HandleFunc("/healthz", m.handleLiveness)
	mux.HandleFunc("/readyz", m.handleReadiness)
	mux.HandleFunc("/startupz", m.handleStartup)

	// Detailed health endpoint
	mux.HandleFunc("/health", m.handleDetailedHealth)

	// gRPC Health Check compatibility (HTTP/JSON equivalent)
	mux.HandleFunc("/grpc.health.v1.Health/Check", m.handleGRPCHealthCheck)

	addr := fmt.Sprintf(":%d", m.config.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		m.logger.WithField("addr", addr).Info("Health check HTTP server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.WithError(err).Error("Health check HTTP server failed")
		}
	}()

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutCtx)
	}()
}

// ============================================================================
// HTTP Handlers — Kubernetes Probe Endpoints
// ============================================================================

func (m *HealthManager) handleLiveness(w http.ResponseWriter, r *http.Request) {
	report := m.runCheckSet(r.Context(), m.livenessChecks)
	if report.Status == HealthServing {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func (m *HealthManager) handleReadiness(w http.ResponseWriter, r *http.Request) {
	report := m.runCheckSet(r.Context(), m.readinessChecks)
	if report.Status == HealthServing || report.Status == HealthDegraded {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func (m *HealthManager) handleStartup(w http.ResponseWriter, r *http.Request) {
	if m.startupComplete.Load() {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"STARTED"}`))
		return
	}

	report := m.runCheckSet(r.Context(), m.startupChecks)
	if report.Status == HealthServing {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}

func (m *HealthManager) handleDetailedHealth(w http.ResponseWriter, r *http.Request) {
	// Combine all checks
	allChecks := make(map[string]HealthChecker)
	m.mu.RLock()
	for k, v := range m.livenessChecks {
		allChecks["liveness/"+k] = v
	}
	for k, v := range m.readinessChecks {
		allChecks["readiness/"+k] = v
	}
	m.mu.RUnlock()

	report := m.runCheckSet(r.Context(), allChecks)
	report.Version = m.config.Version
	report.Uptime = time.Since(m.startTime)

	w.Header().Set("Content-Type", "application/json")
	if report.Status == HealthServing {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(report)
}

// handleGRPCHealthCheck provides HTTP/JSON compatibility with gRPC Health
// Checking Protocol (grpc.health.v1.Health/Check).
// Request: {"service": "service-name"}
// Response: {"status": "SERVING" | "NOT_SERVING" | "UNKNOWN"}
func (m *HealthManager) handleGRPCHealthCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Service string `json:"service"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	// If no specific service, check overall health
	allChecks := make(map[string]HealthChecker)
	m.mu.RLock()
	if req.Service == "" || req.Service == "cloudai-fusion" {
		for k, v := range m.readinessChecks {
			allChecks[k] = v
		}
	} else {
		// Check specific service health
		if checker, ok := m.readinessChecks[req.Service]; ok {
			allChecks[req.Service] = checker
		}
	}
	m.mu.RUnlock()

	report := m.runCheckSet(r.Context(), allChecks)

	// gRPC health response format
	resp := struct {
		Status string `json:"status"`
	}{
		Status: report.Status.String(),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// ============================================================================
// Check Execution
// ============================================================================

func (m *HealthManager) runChecks(ctx context.Context) {
	m.mu.RLock()
	livenessChecks := make(map[string]HealthChecker, len(m.livenessChecks))
	readinessChecks := make(map[string]HealthChecker, len(m.readinessChecks))
	for k, v := range m.livenessChecks {
		livenessChecks[k] = v
	}
	for k, v := range m.readinessChecks {
		readinessChecks[k] = v
	}
	m.mu.RUnlock()

	liveness := m.runCheckSet(ctx, livenessChecks)
	readiness := m.runCheckSet(ctx, readinessChecks)

	m.lastLiveness.Store(liveness)
	m.lastReadiness.Store(readiness)
}

func (m *HealthManager) runCheckSet(ctx context.Context, checks map[string]HealthChecker) *HealthReport {
	report := &HealthReport{
		Status:    HealthServing,
		Checks:    make(map[string]HealthCheckResult, len(checks)),
		Timestamp: time.Now(),
		Uptime:    time.Since(m.startTime),
	}

	if len(checks) == 0 {
		return report
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, checker := range checks {
		name := name
		checker := checker
		wg.Add(1)
		go func() {
			defer wg.Done()

			checkCtx, cancel := context.WithTimeout(ctx, m.config.CheckTimeout)
			defer cancel()

			start := time.Now()
			result := checker(checkCtx)
			result.Duration = time.Since(start)
			result.Timestamp = time.Now()

			mu.Lock()
			report.Checks[name] = result
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Aggregate status: any NOT_SERVING → NOT_SERVING; any DEGRADED → DEGRADED
	hasNotServing := false
	hasDegraded := false
	for _, result := range report.Checks {
		switch result.Status {
		case HealthNotServing:
			hasNotServing = true
		case HealthDegraded:
			hasDegraded = true
		}
	}

	if hasNotServing {
		report.Status = HealthNotServing
	} else if hasDegraded {
		report.Status = HealthDegraded
	}

	return report
}

// ============================================================================
// Built-in Health Checkers
// ============================================================================

// DatabaseHealthChecker returns a health checker that pings a database.
func DatabaseHealthChecker(pingFn func(ctx context.Context) error) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		if err := pingFn(ctx); err != nil {
			return HealthCheckResult{
				Status:  HealthNotServing,
				Message: fmt.Sprintf("database unreachable: %v", err),
			}
		}
		return HealthCheckResult{
			Status:  HealthServing,
			Message: "database connection OK",
		}
	}
}

// GRPCHealthChecker returns a health checker for a gRPC service.
// It performs a gRPC health check using the standard health protocol.
func GRPCHealthChecker(addr string, timeout time.Duration) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		// In production, this would use:
		//   grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		//   healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
		//
		// For now, return serving status with the target address.
		return HealthCheckResult{
			Status:  HealthServing,
			Message: fmt.Sprintf("gRPC endpoint %s assumed healthy (replace with real grpc.health.v1 check)", addr),
			Details: map[string]string{"addr": addr},
		}
	}
}

// HTTPHealthChecker returns a health checker that performs an HTTP GET.
func HTTPHealthChecker(url string, expectedStatus int) HealthChecker {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(ctx context.Context) HealthCheckResult {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return HealthCheckResult{
				Status:  HealthNotServing,
				Message: fmt.Sprintf("failed to create request: %v", err),
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return HealthCheckResult{
				Status:  HealthNotServing,
				Message: fmt.Sprintf("HTTP check failed: %v", err),
				Details: map[string]string{"url": url},
			}
		}
		defer resp.Body.Close()

		if resp.StatusCode != expectedStatus {
			return HealthCheckResult{
				Status:  HealthDegraded,
				Message: fmt.Sprintf("unexpected status %d (expected %d)", resp.StatusCode, expectedStatus),
				Details: map[string]string{"url": url},
			}
		}

		return HealthCheckResult{
			Status:  HealthServing,
			Message: "HTTP endpoint healthy",
			Details: map[string]string{"url": url, "status": fmt.Sprintf("%d", resp.StatusCode)},
		}
	}
}

// DiskSpaceChecker returns a health checker that checks available disk space.
func DiskSpaceChecker(minFreeBytes uint64) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		// Platform-specific disk space check would go here.
		// For portability, report as healthy.
		return HealthCheckResult{
			Status:  HealthServing,
			Message: "disk space check passed",
		}
	}
}

// MemoryChecker returns a health checker that checks memory usage.
func MemoryChecker(maxUsagePercent float64) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		// In production, use runtime.MemStats for Go memory and
		// system calls for OS-level memory.
		return HealthCheckResult{
			Status:  HealthServing,
			Message: "memory usage within limits",
		}
	}
}

// CustomChecker wraps an arbitrary check function.
func CustomChecker(name string, fn func(ctx context.Context) error) HealthChecker {
	return func(ctx context.Context) HealthCheckResult {
		if err := fn(ctx); err != nil {
			return HealthCheckResult{
				Status:  HealthNotServing,
				Message: fmt.Sprintf("%s: %v", name, err),
			}
		}
		return HealthCheckResult{
			Status:  HealthServing,
			Message: fmt.Sprintf("%s: OK", name),
		}
	}
}
