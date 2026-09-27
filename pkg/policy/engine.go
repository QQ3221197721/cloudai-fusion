package policy

import (
	"context"
	"crypto/md5"
	"fmt"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/compile"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage"
	"github.com/open-policy-agent/opa/v1/storage/inmemory"
	"github.com/open-policy-agent/opa/v1/util"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Core Type Definitions - Unified Policy Engine Architecture
// ============================================================================

// Engine represents the core policy evaluation engine integrating OPA/Gatekeeper
type Engine struct {
	mu               sync.RWMutex
	storage          storage.Store
	policyCache      *PolicyCache
	evaluator        *Evaluator
	hotReloader      *HotReloader
	metrics          *MetricsCollector
	queryOptimizer   *QueryOptimizer
	namespaceIndex   map[string]*NamespaceConfig
	inheritanceTree  *InheritanceTree
	versionManager   *VersionManager
	wasmCompiler     *WasmCompiler
	eventHandlers    []EventHandler
	decisionTree     *DecisionTree
	cacheStrategy    CacheStrategy
	defaultPolicy    *PolicyBundle
	performanceMode  PerformanceMode
	maxEvaluationLat time.Duration
	minCacheHitRate  float64
}

// NamespaceConfig defines namespace-scoped policy configuration and inheritance
type NamespaceConfig struct {
	Name              string
	PolicyRefs        []string
	InheritFromParent bool
	OverridePolicies  []*PolicyBundle
	Labels            map[string]string
	Taints            []string
	Tolerations       []map[string]string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Decision represents a single policy decision with metadata
type Decision struct {
	ID             string          `json:"id"`
	DecisionType   string          `json:"type"` // allow, deny, warn, audit
	RegoQuery      string          `json:"query"`
	PolicyRef      string          `json:"policy_ref"`
	Namespace      string          `json:"namespace,omitempty"`
	Subject        interface{}     `json:"subject"`
	Metadata       interface{}     `json:"metadata"`
	Score          float64         `json:"score"`
	Remediation    string          `json:"remediation,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
	ExecutionTime  time.Duration   `json:"execution_time"`
	CacheHit       bool            `json:"cache_hit"`
	Version        string          `json:"version"`
}

// EvaluationResult aggregates multiple decisions from a single evaluation
type EvaluationResult struct {
	Requests     []interface{} `json:"requests"`
	Decisions    []*Decision   `json:"decisions"`
	Summary      SummaryData   `json:"summary"`
	Performance  PerfMetrics   `json:"performance"`
	Error        error         `json:"error,omitempty"`
	Warnings     []string      `json:"warnings,omitempty"`
	Context      EvalContext   `json:"context"`
}

// SummaryData provides high-level statistics about evaluation
type SummaryData struct {
	TotalRequests int       `json:"total_requests"`
	AllowedCount  int       `json:"allowed"`
	DeniedCount   int       `json:"denied"`
	WarnCount     int       `json:"warn"`
	AuditCount    int       `json:"audit"`
	AvgScore      float64   `json:"avg_score"`
	Failures      []Failure `json:"failures,omitempty"`
}

// Failure represents an individual evaluation failure with context
type Failure struct {
	RequestID   string                 `json:"request_id"`
	Query       string                 `json:"query"`
	PolicyRef   string                 `json:"policy_ref"`
	Namespace   string                 `json:"namespace"`
	Message     string                 `json:"message"`
	Suggestions []string               `json:"suggestions,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

// EvalContext captures evaluation environment details
type EvalContext struct {
	StartTime      time.Time            `json:"start_time"`
	EndTime        time.Time            `json:"end_time"`
	EngineVersion  string               `json:"engine_version"`
	NamespaceMode  string               `json:"namespace_mode"`
	CacheEnabled   bool                 `json:"cache_enabled"`
	PerformanceMode PerformanceMode      `json:"performance_mode"`
	ResourceUsage  ResourceUsageDetails `json:"resource_usage"`
	TraceSteps     []TraceStep          `json:"trace_steps,omitempty"`
}

// ResourceUsageDetails tracks resource consumption during evaluation
type ResourceUsageDetails struct {
	MemoryBytes      uint64        `json:"memory_bytes"`
	Goroutines       int           `json:"goroutines"`
	EvaluationTimeMs time.Duration `json:"evaluation_time_ms"`
	CacheHits        int           `json:"cache_hits"`
	CacheMisses      int           `json:"cache_misses"`
}

// TraceStep captures detailed execution trace information
type TraceStep struct {
	StepNumber   int           `json:"step"`
	Operation    string        `json:"operation"`
	Target       string        `json:"target"`
	StartTime    time.Time     `json:"start_time"`
	EndTime      time.Time     `json:"end_time"`
	Duration     time.Duration `json:"duration"`
	Result       string        `json:"result"`
	Metadata     interface{}   `json:"metadata,omitempty"`
}

// ============================================================================
// Engine Construction and Initialization
// ============================================================================

// EngineConfig defines configuration options for policy engine
type EngineConfig struct {
	StorageBackend      string                    `json:"storage_backend"`
	StorageEndpoint     string                    `json:"storage_endpoint"`
	CacheEnabled        bool                      `json:"cache_enabled"`
	CacheMaxSize        int                       `json:"cache_max_size"`
	CacheTTLSeconds     int                       `json:"cache_ttl_seconds"`
	HotReloadEnabled    bool                      `json:"hot_reload_enabled"`
	HotReloadIntervalMs int                       `json:"hot_reload_interval"`
	PerformanceMode     PerformanceMode           `json:"performance_mode"`
	DefaultNamespace    string                    `json:"default_namespace"`
	Logger              *logrus.Logger
	Tracer              Tracer
	EventHandlers       []EventHandler
}

// DefaultEngineConfig returns a sensible default configuration
func DefaultEngineConfig() *EngineConfig {
	return &EngineConfig{
		StorageBackend:      "in-memory",
		StorageEndpoint:     "",
		CacheEnabled:        true,
		CacheMaxSize:        10000,
		CacheTTLSeconds:     300,
		HotReloadEnabled:    true,
		HotReloadIntervalMs: 5000,
		PerformanceMode:     StandardMode,
		DefaultNamespace:    "default",
		Logger:              logrus.New(),
		Tracer:              nil,
		EventHandlers:       make([]EventHandler, 0),
	}
}

// NewEngine creates a new policy engine with the specified configuration
func NewEngine(cfg *EngineConfig) (*Engine, error) {
	if cfg == nil {
		cfg = DefaultEngineConfig()
	}

	engine := &Engine{
		storage:          nil,
		policyCache:      nil,
		evaluator:        nil,
		hotReloader:      nil,
		metrics:          nil,
		queryOptimizer:   nil,
		namespaceIndex:   make(map[string]*NamespaceConfig),
		inheritanceTree:  NewInheritanceTree(),
		versionManager:   NewVersionManager(),
		wasmCompiler:     NewWasmCompiler(),
		eventHandlers:    make([]EventHandler, 0),
		decisionTree:     nil,
		cacheStrategy:    nil,
		defaultPolicy:    nil,
		performanceMode:  cfg.PerformanceMode,
		maxEvaluationLat: 500 * time.Millisecond,
		minCacheHitRate:  0.7,
	}

	var err error

	// Initialize storage backend
	switch cfg.StorageBackend {
	case "in-memory":
		engine.storage = inmemory.NewStore()
	case "postgres":
		engine.storage, err = NewPostgresStorage(cfg.StorageEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize postgres storage: %w", err)
		}
	case "etcd":
		engine.storage, err = NewEtcdStorage(cfg.StorageEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize etcd storage: %w", err)
		}
	default:
		engine.storage = inmemory.NewStore()
		cfg.Logger.Warnf("unknown storage backend '%s', using in-memory", cfg.StorageBackend)
	}

	// Initialize policy cache
	engine.policyCache = NewPolicyCache(PolicyCacheConfig{
		MaxSize: cfg.CacheMaxSize,
		TTL:     time.Duration(cfg.CacheTTLSeconds) * time.Second,
	})

	// Initialize evaluator with query optimizer
	engine.evaluator = NewEvaluator(&EvaluatorConfig{
		Storage:       engine.storage,
		Cache:         engine.policyCache,
		Optimizer:     NewQueryOptimizer(),
		PerformanceMode: cfg.PerformanceMode,
		Logger:        cfg.Logger,
		Tracer:        cfg.Tracer,
	})

	// Initialize hot reloader if enabled
	if cfg.HotReloadEnabled {
		engine.hotReloader = NewHotReloader(&HotReloaderConfig{
			Interval:         time.Duration(cfg.HotReloadIntervalMs) * time.Millisecond,
			PollEnabled:      true,
			WatcherEnabled:   false,
			Validator:        NewPolicyValidator(),
			EventChannel:     make(chan ReloadEvent, 100),
		})
		
		engine.eventHandlers = append(engine.eventHandlers, engine.hotReloader.GetHandler())
	}

	// Initialize metrics collector
	engine.metrics = NewMetricsCollector(
		engine.policyCache,
		engine.evaluator,
		cfg.Logger,
	)

	// Register event handlers
	for _, handler := range cfg.EventHandlers {
		engine.RegisterEventHandler(handler)
	}

	// Initialize default namespace configuration
	err = engine.InitializeDefaultNamespace(cfg.DefaultNamespace)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize default namespace: %w", err)
	}

	// Build initial decision tree
	err = engine.BuildInitialDecisionTree()
	if err != nil {
		cfg.Logger.Warnf("initial decision tree build failed: %v", err)
	}

	cfg.Logger.Info("policy engine initialized successfully")
	return engine, nil
}

// ============================================================================
// Lifecycle Management - Start/Stop/Shutdown
// ============================================================================

// Start initializes all background processes and starts hot-reload monitoring
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.mu.RLock()
	logger := e.metrics.logger
	e.mu.RUnlock()

	logger.Info("starting policy engine...")

	// Start hot reload mechanism if configured
	if e.hotReloader != nil {
		go e.hotReloader.Run(ctx)
		logger.Info("hot reload started")
	}

	// Start metrics collection
	go e.metrics.StartCollection(ctx)
	logger.Info("metrics collection started")

	// Start decision tree optimization
	go e.decisionTree.OptimizeLoop(ctx)
	logger.Info("decision tree optimizer started")

	return nil
}

// Stop gracefully shuts down all background processes
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.hotReloader != nil {
		e.hotReloader.Stop()
	}

	if e.metrics != nil {
		e.metrics.Stop()
	}

	if e.decisionTree != nil {
		e.decisionTree.Shutdown()
	}

	return nil
}

// Shutdown performs a full cleanup including persisting state
func (e *Engine) Shutdown(ctx context.Context) error {
	e.Stop()
	
	// Persist cache state
	if err := e.policyCache.PersistState(); err != nil {
		return fmt.Errorf("failed to persist cache: %w", err)
	}

	// Save decision tree state
	if e.decisionTree != nil {
		e.decisionTree.SaveToFile("/tmp/decision_tree_state.json")
	}

	return nil
}
