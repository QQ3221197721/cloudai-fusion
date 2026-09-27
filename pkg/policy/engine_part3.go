package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Hot Reloading - Dynamic Policy Updates
// ============================================================================

// ReloadEvent represents a single policy reload trigger
type ReloadEvent struct {
	Type       string                `json:"type"` // add/update/delete/refresh
	PolicyRef  string                `json:"policy_ref"`
	Namespace  string                `json:"namespace"`
	Source     string                `json:"source"`
	Timestamp  time.Time             `json:"timestamp"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// HotReloader manages dynamic policy updates without restart
type HotReloader struct {
	mu              sync.RWMutex
	config          *HotReloaderConfig
	pollingTicker   *time.Ticker
	watcher         *PolicyWatcher
	reloadCh        chan *ReloadEvent
	ctx             context.Context
	cancel          context.CancelFunc
	handlers        []EventHandler
	isRunning       bool
	lastReloadTime  time.Time
	reloadCount     int64
	eventsProcessed int64
	logger          *logrus.Logger
}

type HotReloaderConfig struct {
	Interval         time.Duration
	PollEnabled      bool
	WatcherEnabled   bool
	Validator        PolicyValidator
	EventChannelSize int
	PollPaths        []string
	WatchPath        string
}

// NewHotReloader creates hot reloader instance
func NewHotReloader(cfg *HotReloaderConfig) *HotReloader {
	if cfg == nil {
		cfg = &HotReloaderConfig{
			Interval:         5 * time.Second,
			PollEnabled:      true,
			WatcherEnabled:   false,
			Validator:        NewPolicyValidator(),
			EventChannelSize: 100,
			PollPaths:        make([]string, 0),
		}
	}

	return &HotReloader{
		config:      cfg,
		pollingTicker: nil,
		watcher:     nil,
		reloadCh:    make(chan *ReloadEvent, cfg.EventChannelSize),
		handlers:    make([]EventHandler, 0),
		logger:      logrus.New(),
	}
}

// Run starts the hot reloading mechanism
func (hr *HotReloader) Run(ctx context.Context) {
	hr.mu.Lock()
	if hr.isRunning {
		hr.mu.Unlock()
		return
	}
	
	hr.ctx, hr.cancel = context.WithCancel(ctx)
	hr.isRunning = true
	hr.mu.Unlock()

	if hr.config.PollEnabled {
		hr.pollingTicker = time.NewTicker(hr.config.Interval)
		go hr.pollLoop()
	}

	if hr.config.WatcherEnabled && hr.config.WatchPath != "" {
		var err error
		hr.watcher, err = NewPolicyWatcher(hr.config.WatchPath)
		if err != nil {
			logrus.Errorf("watcher initialization failed: %v", err)
		} else {
			go hr.watchLoop()
		}
	}

	hr.logger.Info("hot reloader started")
}

// Stop gracefully stops the hot reloader
func (hr *HotReloader) Stop() {
	hr.mu.Lock()
	defer hr.mu.Unlock()

	if !hr.isRunning {
		return
	}

	if hr.pollingTicker != nil {
		hr.pollingTicker.Stop()
	}

	if hr.watcher != nil {
		hr.watcher.Close()
	}

	if hr.cancel != nil {
		hr.cancel()
	}

	hr.isRunning = false
	hr.logger.Info("hot reloader stopped")
}

// GetHandler returns event handler for integration
func (hr *HotReloader) GetHandler() EventHandler {
	return EventHandlerFunc(func(event Event) {
		select {
		case hr.reloadCh <- &ReloadEvent{
			Type:      event.GetType(),
			PolicyRef: event.GetPolicyRef(),
			Namespace: event.GetNamespace(),
			Source:    "event-handler",
			Timestamp: time.Now(),
		}:
		default:
			logrus.Warn("reload channel full, dropping event")
		}
	})
}

// pollLoop monitors file system for changes
func (hr *HotReloader) pollLoop() {
	ticker := hr.pollingTicker
	if ticker == nil {
		return
	}

	for {
		select {
		case <-hr.ctx.Done():
			return
		case <-ticker.C:
			hr.checkAndReload()
		}
	}
}

// watchLoop handles FS events
func (hr *HotReloader) watchLoop() {
	if hr.watcher == nil {
		return
	}

	for {
		select {
		case <-hr.ctx.Done():
			return
		case event := <-hr.watcher.Events:
			go hr.processFsEvent(event)
		case err := <-hr.watcher.Errors:
			hr.logger.Errorf("watcher error: %v", err)
		}
	}
}

// checkAndReload validates and reloads changed policies
func (hr *HotReloader) checkAndReload() {
	changes := hr.detectChanges()
	if len(changes) == 0 {
		return
	}

	for _, change := range changes {
		event := &ReloadEvent{
			Type:      hr.determineEventType(change),
			PolicyRef: change.Path,
			Namespace: hr.extractNamespace(change.Path),
			Source:    "poller",
			Timestamp: time.Now(),
		}

		if hr.config.Validator != nil {
			valid, err := hr.config.Validator.Validate(change.Content)
			if !valid || err != nil {
				hr.logger.Warnf("validation failed '%s': %v", change.Path, err)
				continue
			}
		}

		atomicAddInt64(&hr.reloadCount, 1)
		hr.emitReloadEvent(event)
	}
}

// emitReloadEvent notifies all registered handlers
func (hr *HotReloader) emitReloadEvent(event *ReloadEvent) {
	now := time.Now()
	
	for _, handler := range hr.handlers {
		handler.Handle(Event{
			Type:           event.Type,
			PolicyRef:      event.PolicyRef,
			Namespace:      event.Namespace,
			Timestamp:      now,
			RawData:        event.Metadata,
		})
	}

	atomicAddInt64(&hr.eventsProcessed, 1)
	hr.lastReloadTime = now
}

// ============================================================================
// Metrics Collector - Performance Tracking
// ============================================================================

// MetricsCollector tracks policy engine performance
type MetricsCollector struct {
	mu              sync.RWMutex
	caches          []*PolicyCache
	evaluators      []*Evaluator
	logger          *logrus.Logger
	interval        time.Duration
	stopCh          chan struct{}
	customCollectors []MetricCollector
	counters       MetricsCounters
}

// MetricsCounters aggregates various metrics
type MetricsCounters struct {
	TotalReqs          int64
	AllowedReqs        int64
	DeniedReqs         int64
	WarnReqs           int64
	AuditReqs          int64
	CacheHits          int64
	CacheMisses        int64
	EvalErrors         int64
	TotalExecTimeNanos int64
}

// StartCollection begins metrics gathering
func (mc *MetricsCollector) StartCollection(ctx context.Context) {
	mc.stopCh = make(chan struct{})

	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.collect()
		}
	}
}

// Stop halts metrics collection
func (mc *MetricsCollector) Stop() {
	if mc.stopCh != nil {
		close(mc.stopCh)
	}
}

// collect gathers current metrics state
func (mc *MetricsCollector) collect() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	totalHitRate := float64(0)
	totalCaches := 0

	for _, cache := range mc.caches {
		stats := cache.Stats()
		totalHitRate += stats.HitRate
		totalCaches++
	}

	if totalCaches > 0 {
		avgHitRate := totalHitRate / float64(totalCaches)
		if avgHitRate < mc.minCacheHitRate*100 {
			mc.logger.Warnf("cache hit rate below threshold: %.2f%%", avgHitRate)
		}
	}

	mc.counters.TotalReqs = atomicLoadInt64(&mc.counters.TotalReqs) + 1
}

// Report generates comprehensive metrics summary
func (mc *MetricsCollector) Report() MetricReport {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	return MetricReport{
		Summary: SummaryData{
			TotalRequests: int(atomicLoadInt64(&mc.counters.TotalReqs)),
			AllowedCount:  int(atomicLoadInt64(&mc.counters.AllowedReqs)),
			DeniedCount:   int(atomicLoadInt64(&mc.counters.DeniedReqs)),
			WarnCount:     int(atomicLoadInt64(&mc.counters.WarnReqs)),
			AuditCount:    int(atomicLoadInt64(&mc.counters.AuditReqs)),
		},
		Performance: PerfMetrics{
			CacheHitRate:  calculateHitRate(),
			AvgEvalLatency: calculateAvgLatency(),
			P99Latency:     calculateP99(),
		},
		HealthStatus: "healthy",
		Timestamp:    time.Now(),
	}
}

// ============================================================================
// Decision Tree - Fast Path Routing
// ============================================================================

// DecisionTree provides optimized decision routing
type DecisionTree struct {
	mu            sync.RWMutex
	root          *TreeNode
	routeCache    map[string]*RouteEntry
 optimizing    bool
	optimizeTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
}

type RouteEntry struct {
	PathHash   string
	DecisionType string
	Priority   int
	Metrics    RouteMetrics
}

// TreeNode represents decision tree node
type TreeNode struct {
	predicate string
	children  map[string]*TreeNode
	decision  *Decision
	metrics   NodeMetrics
}

// BuildInitialDecisionTree constructs initial optimization tree
func (e *Engine) BuildInitialDecisionTree() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	tree := NewDecisionTree()
	
	entries := e.policyCache.Keys()
	for _, entryKey := range entries {
		entry, found := e.policyCache.Get(entryKey)
		if !found || entry == nil {
			continue
		}
		
		if len(entry.policies) == 0 {
			continue
		}
		
		pathHash := e.calculateHashForPolicy(entry.policies[0])
		route := &RouteEntry{
			PathHash:     pathHash,
			DecisionType: "allow",
			Priority:     1,
		}
		
		tree.Insert(route)
	}

	e.decisionTree = tree
	return nil
}

// FindOptimalRoute determines fastest execution path
func (dt *DecisionTree) FindOptimalRoute(key string) (*RouteEntry, bool) {
	dt.mu.RLock()
	defer dt.mu.RUnlock()

	if entry, exists := dt.routeCache[key]; exists {
		entry.Metrics.Hits++
		return entry, true
	}

	node := dt.findNodeByKey(key)
	if node == nil {
		return nil, false
	}

	entry := &RouteEntry{
		PathHash:     key,
		DecisionType: node.decision.DecisionType,
		Priority:     node.metrics.Priority,
		Metrics: RouteMetrics{
			Hits: 1,
		},
	}

	dt.routeCache[key] = entry
	return entry, true
}

// OptimizeLoop continuously optimizes route distribution
func (dt *DecisionTree) OptimizeLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dt.optimize()
		}
	}
}
