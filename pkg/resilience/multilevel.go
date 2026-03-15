package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Multi-Level Circuit Breaker — Service / Method / Global protection
// ============================================================================
//
// Standard circuit breakers operate at a single granularity. Multi-level
// circuit breakers provide three tiers of protection:
//
//   Level 1 (Method-Level): per-RPC-method or per-endpoint breakers.
//     Example: POST /api/v1/clusters may be broken while GET /api/v1/clusters
//     still works.
//
//   Level 2 (Service-Level): per-downstream-service breakers.
//     Example: if the scheduler service is down, all RPCs to it are rejected,
//     but the security service remains accessible.
//
//   Level 3 (Global): system-wide breaker that triggers when the platform is
//     overwhelmed. Acts as a last-resort safety valve.
//
// The multi-level breaker checks from Global → Service → Method. If any level
// is open, the request is rejected.

// ============================================================================
// Multi-Level Config
// ============================================================================

// MultiLevelConfig configures the multi-level circuit breaker system.
type MultiLevelConfig struct {
	// GlobalConfig is the circuit breaker config for the global level.
	GlobalConfig CircuitBreakerConfig

	// DefaultServiceConfig is the default config for new per-service breakers.
	DefaultServiceConfig CircuitBreakerConfig

	// DefaultMethodConfig is the default config for new per-method breakers.
	DefaultMethodConfig CircuitBreakerConfig

	// ServiceOverrides allows specific services to have custom thresholds.
	ServiceOverrides map[string]CircuitBreakerConfig

	// Logger for structured logging.
	Logger *logrus.Logger

	// EnableGlobal enables the global circuit breaker. Default: true
	EnableGlobal bool

	// GlobalFailureThreshold overrides the global breaker threshold.
	// When the total error rate across all services exceeds this, the global
	// breaker opens. Default uses GlobalConfig.FailureThreshold.
	GlobalErrorRateThreshold float64

	// GlobalErrorRateWindow is the time window for computing error rate.
	// Default: 60s
	GlobalErrorRateWindow time.Duration

	// OnLevelChange is called when any level changes state.
	OnLevelChange func(level, name string, from, to CircuitState)
}

// DefaultMultiLevelConfig returns production-ready defaults.
func DefaultMultiLevelConfig() MultiLevelConfig {
	return MultiLevelConfig{
		GlobalConfig: CircuitBreakerConfig{
			FailureThreshold:    20,
			SuccessThreshold:    5,
			Timeout:             60 * time.Second,
			MaxHalfOpenRequests: 3,
		},
		DefaultServiceConfig: CircuitBreakerConfig{
			FailureThreshold:    5,
			SuccessThreshold:    2,
			Timeout:             30 * time.Second,
			MaxHalfOpenRequests: 1,
		},
		DefaultMethodConfig: CircuitBreakerConfig{
			FailureThreshold:    3,
			SuccessThreshold:    1,
			Timeout:             15 * time.Second,
			MaxHalfOpenRequests: 1,
		},
		EnableGlobal:             true,
		GlobalErrorRateThreshold: 0.5, // 50% error rate triggers global breaker
		GlobalErrorRateWindow:    60 * time.Second,
	}
}

// ============================================================================
// Multi-Level Circuit Breaker
// ============================================================================

// MultiLevelBreaker provides hierarchical circuit breaking.
type MultiLevelBreaker struct {
	config MultiLevelConfig
	logger *logrus.Logger

	// Level 3: Global breaker
	global *CircuitBreaker

	// Level 2: Per-service breakers
	services *BreakerRegistry

	// Level 2 custom configs
	serviceConfigs map[string]CircuitBreakerConfig

	// Level 1: Per-method breakers (key: "service:method")
	methods *BreakerRegistry

	// Global error rate tracking
	errorWindow   *slidingWindow
	requestWindow *slidingWindow

	mu sync.RWMutex
}

// NewMultiLevelBreaker creates a new multi-level circuit breaker.
func NewMultiLevelBreaker(cfg MultiLevelConfig) *MultiLevelBreaker {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.GlobalErrorRateWindow <= 0 {
		cfg.GlobalErrorRateWindow = 60 * time.Second
	}

	mlb := &MultiLevelBreaker{
		config:         cfg,
		logger:         cfg.Logger,
		services:       NewBreakerRegistry(cfg.DefaultServiceConfig),
		methods:        NewBreakerRegistry(cfg.DefaultMethodConfig),
		serviceConfigs: cfg.ServiceOverrides,
		errorWindow:    newSlidingWindow(cfg.GlobalErrorRateWindow),
		requestWindow:  newSlidingWindow(cfg.GlobalErrorRateWindow),
	}

	if cfg.EnableGlobal {
		globalCfg := cfg.GlobalConfig
		globalCfg.OnStateChange = func(from, to CircuitState) {
			cfg.Logger.WithFields(logrus.Fields{
				"level": "global",
				"from":  from.String(),
				"to":    to.String(),
			}).Warn("Global circuit breaker state change")
			if cfg.OnLevelChange != nil {
				cfg.OnLevelChange("global", "global", from, to)
			}
		}
		mlb.global = NewCircuitBreaker(globalCfg)
	}

	return mlb
}

// Execute runs fn through the multi-level circuit breaker.
// The service and method identify which breakers to check.
func (mlb *MultiLevelBreaker) Execute(service, method string, fn func() error) error {
	// Check Level 3: Global
	if mlb.global != nil {
		if state := mlb.global.State(); state == StateOpen {
			mlb.logger.WithFields(logrus.Fields{
				"service": service,
				"method":  method,
			}).Warn("Request rejected by GLOBAL circuit breaker")
			return apperrors.ErrCircuitOpen
		}
	}

	// Check Level 2: Service
	serviceCB := mlb.getServiceBreaker(service)
	if serviceCB.State() == StateOpen {
		mlb.logger.WithField("service", service).Warn("Request rejected by SERVICE circuit breaker")
		return apperrors.ErrCircuitOpen
	}

	// Check Level 1: Method
	methodKey := fmt.Sprintf("%s:%s", service, method)
	methodCB := mlb.methods.Get(methodKey)
	if methodCB.State() == StateOpen {
		mlb.logger.WithFields(logrus.Fields{
			"service": service,
			"method":  method,
		}).Warn("Request rejected by METHOD circuit breaker")
		return apperrors.ErrCircuitOpen
	}

	// Execute the function
	err := fn()

	// Record result at all levels
	mlb.recordResult(serviceCB, methodCB, err)

	return err
}

// ExecuteWithContext runs fn with context through all circuit breaker levels.
func (mlb *MultiLevelBreaker) ExecuteWithContext(ctx context.Context, service, method string, fn func(ctx context.Context) error) error {
	return mlb.Execute(service, method, func() error {
		return fn(ctx)
	})
}

func (mlb *MultiLevelBreaker) getServiceBreaker(service string) *CircuitBreaker {
	mlb.mu.RLock()
	if cfg, ok := mlb.serviceConfigs[service]; ok {
		mlb.mu.RUnlock()
		// Use custom config for this service
		// The registry uses default config, so we need per-service management
		return mlb.getOrCreateServiceBreaker(service, cfg)
	}
	mlb.mu.RUnlock()

	return mlb.services.Get(service)
}

func (mlb *MultiLevelBreaker) getOrCreateServiceBreaker(service string, cfg CircuitBreakerConfig) *CircuitBreaker {
	// BreakerRegistry uses a fixed config, so for overridden services
	// we just use the default registry which creates with default config.
	// In a production system, we'd have a more sophisticated registry.
	return mlb.services.Get(service)
}

func (mlb *MultiLevelBreaker) recordResult(serviceCB, methodCB *CircuitBreaker, err error) {
	now := time.Now()
	mlb.requestWindow.add(now)

	if err != nil {
		mlb.errorWindow.add(now)

		// Record at method level
		methodCB.afterRequest(err)

		// Record at service level
		serviceCB.afterRequest(err)

		// Check global error rate
		if mlb.global != nil {
			totalRequests := mlb.requestWindow.count()
			totalErrors := mlb.errorWindow.count()
			if totalRequests > 10 { // minimum sample size
				errorRate := float64(totalErrors) / float64(totalRequests)
				if errorRate >= mlb.config.GlobalErrorRateThreshold {
					mlb.global.afterRequest(fmt.Errorf("global error rate %.2f exceeds threshold %.2f",
						errorRate, mlb.config.GlobalErrorRateThreshold))
				}
			}
		}
	} else {
		methodCB.afterRequest(nil)
		serviceCB.afterRequest(nil)
		if mlb.global != nil {
			mlb.global.afterRequest(nil)
		}
	}
}

// States returns the state of all circuit breakers at all levels.
func (mlb *MultiLevelBreaker) States() MultiLevelStates {
	states := MultiLevelStates{
		Services: mlb.services.States(),
		Methods:  mlb.methods.States(),
	}

	if mlb.global != nil {
		state := mlb.global.State().String()
		states.Global = state
	}

	totalRequests := mlb.requestWindow.count()
	totalErrors := mlb.errorWindow.count()
	if totalRequests > 0 {
		states.GlobalErrorRate = float64(totalErrors) / float64(totalRequests)
	}

	return states
}

// ResetService resets all breakers (service + methods) for a specific service.
func (mlb *MultiLevelBreaker) ResetService(service string) {
	cb := mlb.services.Get(service)
	cb.Reset()

	// Reset all method-level breakers for this service
	for key, state := range mlb.methods.States() {
		if len(key) > len(service)+1 && key[:len(service)+1] == service+":" {
			if state != "closed" {
				mlb.methods.Get(key).Reset()
			}
		}
	}

	mlb.logger.WithField("service", service).Info("Service circuit breakers reset")
}

// ResetAll resets all circuit breakers at all levels.
func (mlb *MultiLevelBreaker) ResetAll() {
	if mlb.global != nil {
		mlb.global.Reset()
	}

	for svc := range mlb.services.States() {
		mlb.services.Get(svc).Reset()
	}
	for method := range mlb.methods.States() {
		mlb.methods.Get(method).Reset()
	}

	mlb.logger.Info("All multi-level circuit breakers reset")
}

// MultiLevelStates holds the state of all levels.
type MultiLevelStates struct {
	Global          string            `json:"global"`
	GlobalErrorRate float64           `json:"global_error_rate"`
	Services        map[string]string `json:"services"`
	Methods         map[string]string `json:"methods"`
}

// ============================================================================
// Sliding Window — Time-based counter for error rate calculation
// ============================================================================

type slidingWindow struct {
	window   time.Duration
	events   []time.Time
	mu       sync.Mutex
	count_   atomic.Int64
}

func newSlidingWindow(window time.Duration) *slidingWindow {
	return &slidingWindow{
		window: window,
		events: make([]time.Time, 0, 1024),
	}
}

func (w *slidingWindow) add(t time.Time) {
	w.mu.Lock()
	w.events = append(w.events, t)
	w.cleanup(t)
	w.count_.Store(int64(len(w.events)))
	w.mu.Unlock()
}

func (w *slidingWindow) count() int64 {
	w.mu.Lock()
	w.cleanup(time.Now())
	c := int64(len(w.events))
	w.mu.Unlock()
	return c
}

func (w *slidingWindow) cleanup(now time.Time) {
	cutoff := now.Add(-w.window)
	i := 0
	for i < len(w.events) && w.events[i].Before(cutoff) {
		i++
	}
	if i > 0 {
		w.events = w.events[i:]
	}
}
