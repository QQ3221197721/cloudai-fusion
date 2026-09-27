package policy

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/open-policy-agent/opa/v1/format"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/sirupsen/logrus"
)

// PolicyBundle represents a collection of Rego policies with metadata
type PolicyBundle struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Data       interface{}     `json:"data"`
	RegoFiles  []*PolicyFile   `json:"rego_files"`
	Metadata   map[string]any  `json:"metadata"`
	Checksum   string          `json:"checksum"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Namespace  string          `json:"namespace"`
	Enabled    bool            `json:"enabled"`
	Priority   int             `json:"priority"`
	Scopes     []string        `json:"scopes,omitempty"`
}

// PolicyFile contains individual Rego policy content
type PolicyFile struct {
	Path     string    `json:"path"`
	Content  string    `json:"content"`
	Compiled *Compiled `json:"compiled,omitempty"`
}

// Compiled holds pre-compiled Rego module for optimization
type Compiled struct {
	Module   *reggo.Module `json:"module"`
	Bytecode []byte        `json:"bytecode"`
}

// PolicyCache implements LRU cache with TTL support for compiled policies
type PolicyCache struct {
	mu           sync.RWMutex
	cache        map[string]*cachedEntry
	maxSize      int
	ttl          time.Duration
	lruList      *LRUList
	evictionCh   chan *cachedEntry
	persistDir   string
	loadCount    int64
	hitCount     int64
	missCount    int64
	lastCleanUp  time.Time
	cleanupInterval time.Duration
}

type cachedEntry struct {
	value      interface{}
	checksum   string
	expiresAt  time.Time
	loadTime   int64
	refCount   int
	policies   []*PolicyBundle
}

// PolicyCacheConfig defines cache parameters
type PolicyCacheConfig struct {
	MaxSize         int
	TTL             time.Duration
	CleanupInterval time.Duration
	PersistDir      string
}

// DefaultPolicyCacheConfig returns sensible defaults
func DefaultPolicyCacheConfig() *PolicyCacheConfig {
	return &PolicyCacheConfig{
		MaxSize:         10000,
		TTL:             5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		PersistDir:      "/tmp/policy_cache",
	}
}

// NewPolicyCache creates a new policy cache instance
func NewPolicyCache(cfg *PolicyCacheConfig) *PolicyCache {
	if cfg == nil {
		cfg = DefaultPolicyCacheConfig()
	}

	pc := &PolicyCache{
		cache:         make(map[string]*cachedEntry),
		maxSize:       cfg.MaxSize,
		ttl:           cfg.TTL,
		lruList:       NewLRUList(),
		evictionCh:    make(chan *cachedEntry, 100),
		persistDir:    cfg.PersistDir,
		cleanupInterval: cfg.CleanupInterval,
		lastCleanUp:   time.Now(),
	}

	go pc.cleanupLoop()
	return pc
}

// Get retrieves a cached policy bundle by key
func (pc *PolicyCache) Get(key string) (*cachedEntry, bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	entry, exists := pc.cache[key]
	if !exists || entry.isExpired() {
		if exists {
			pc.removeEntry(key)
		}
		atomicAddInt64(&pc.missCount, 1)
		return nil, false
	}

	// Update LRU position
	pc.lruList.moveToFront(entry)
	atomicAddInt64(&pc.hitCount, 1)
	entry.refCount++
	
	return entry, true
}

// Put inserts or updates a cached entry
func (pc *PolicyCache) Put(key string, entry *cachedEntry) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Check if adding would exceed max size
	if len(pc.cache) >= pc.maxSize {
		staleEntry := pc.evictOldestLocked()
		if staleEntry != nil {
			pc.evictionCh <- staleEntry
		}
	}

	if existing, ok := pc.cache[key]; ok {
		pc.lruList.remove(existing)
	}

	pc.lruList.addToFront(entry)
	entry.loadTime = time.Now().UnixNano()
	entry.expiresAt = time.Now().Add(pc.ttl)
	pc.cache[key] = entry

	return nil
}

// Delete removes an entry from cache
func (pc *PolicyCache) Delete(key string) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	return pc.removeEntry(key)
}

// Keys returns all current cache keys
func (pc *PolicyCache) Keys() []string {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	keys := make([]string, 0, len(pc.cache))
	for k := range pc.cache {
		keys = append(keys, k)
	}
	return keys
}

// Size returns current number of entries
func (pc *PolicyCache) Size() int {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return len(pc.cache)
}

// Stats returns cache performance metrics
func (pc *PolicyCache) Stats() CacheStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	totalOps := pc.loadCount + pc.hitCount + pc.missCount
	hitRate := float64(0)
	if totalOps > 0 {
		hitRate = float64(pc.hitCount) / float64(totalOps) * 100
	}

	return CacheStats{
		Size:         len(pc.cache),
		MaxSize:      pc.maxSize,
		HitCount:     atomicLoadInt64(&pc.hitCount),
		MissCount:    atomicLoadInt64(&pc.missCount),
		LoadCount:    atomicLoadInt64(&pc.loadCount),
		HitRate:      hitRate,
		TTLSeconds:   int(pc.ttl.Seconds()),
		CacheEnabled: true,
	}
}

// PersistState saves cache state to disk
func (pc *PolicyCache) PersistState() error {
	// Implementation would serialize cache to disk
	return nil
}

// cleanupLoop periodically removes expired entries
func (pc *PolicyCache) cleanupLoop() {
	ticker := time.NewTicker(pc.cleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		pc.cleanup()
	}
}

// cleanup removes expired and orphaned entries
func (pc *PolicyCache) cleanup() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	var keysToDelete []string
	now := time.Now()

	for key, entry := range pc.cache {
		if entry.isExpired() || entry.refCount <= 0 {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		pc.removeEntry(key)
	}

	if len(keysToDelete) > 0 {
		logrus.Debugf("policy cache cleanup: removed %d entries", len(keysToDelete))
	}

	pc.lastCleanUp = now
}

// removeEntry deletes an entry from cache
func (pc *PolicyCache) removeEntry(key string) bool {
	entry, exists := pc.cache[key]
	if !exists {
		return false
	}

	delete(pc.cache, key)
	pc.lruList.remove(entry)
	atomicAddInt64(&pc.missCount, 1)

	return true
}

// evictOldestLocked removes oldest entry (caller must hold lock)
func (pc *PolicyCache) evictOldestLocked() *cachedEntry {
	if len(pc.cache) == 0 {
		return nil
	}

	stale := pc.lruList.getBack()
	if stale == nil {
		return nil
	}

	key := stale.getKey()
	delete(pc.cache, key)
	pc.evictionCh <- stale
	
	return stale
}

// ============================================================================
// Loader - Rego Policy Parsing Pipeline
// ============================================================================

// PolicyLoader handles loading and parsing of Rego policies
type PolicyLoader struct {
	logger  *logrus.Logger
	parser  *RegoParser
	compiler *PolicyCompiler
}

// LoadDirectory loads all Rego files from directory
func (pl *PolicyLoader) LoadDirectory(ctx context.Context, path string) ([]*PolicyBundle, error) {
	result, err := loader.NewPathLoader(path).All()
	if err != nil {
		return nil, fmt.Errorf("failed to load policies from '%s': %w", path, err)
	}

	bundles := make([]*PolicyBundle, 0)
	for _, module := range result.Modules {
		policy, err := pl.parsePolicy(module)
		if err != nil {
			pl.logger.Warnf("failed to parse policy '%s': %v", module.Path, err)
			continue
		}
		bundles = append(bundles, policy)
	}

	return bundles, nil
}

// LoadBundle loads OPA-style bundle format
func (pl *PolicyLoader) LoadBundle(ctx context.Context, bndl *bundle.Bundle) (*PolicyBundle, error) {
	if bndl == nil {
		return nil, fmt.Errorf("null bundle reference")
	}

	regoFiles := make([]*PolicyFile, 0)
	for _, data := range bndata.Data {
		file := &PolicyFile{
			Path:    bndata.Path,
			Content: string(data.Raw),
		}
		
		compiled, err := pl.compiler.Compile(file.Content)
		if err != nil {
			pl.logger.Warnf("compile failed '%s': %v", bndata.Path, err)
		}
		file.Compiled = compiled
		regoFiles = append(regoFiles, file)
	}

	return &PolicyBundle{
		Name:       bndata.Name,
		Version:    bndata.Metadata["version"].(string),
		Data:       bndata.Data,
		RegoFiles:  regoFiles,
		Metadata:   bndata.Metadata,
		Checksum:   fmt.Sprintf("%x", md5.Sum(bndata.Raw)),
		Namespace:  bndata.Metadata["namespace"].(string),
		Enabled:    true,
	}, nil
}

// ParseRegocode parses inline Rego code
func (pl *PolicyLoader) ParseRegoCode(code string) (*PolicyBundle, error) {
	modules, err := rego.ParseModules("inline", code)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	return &PolicyBundle{
		Name:      "inline-policy",
		Version:   "1.0.0",
		RegoFiles: []*PolicyFile{{Content: code}},
		Metadata:  map[string]any{"source": "inline"},
	}, nil
}

// ============================================================================
// Evaluator - High-Performance Decision Engine
// ============================================================================

// Evaluator executes policy decisions with optimizations
type Evaluator struct {
	mu              sync.RWMutex
	storage         storage.Store
	cache           *PolicyCache
	optimizer       *QueryOptimizer
	preparedQueries map[string]*regio.Query
	mode            PerformanceMode
	queryTimeout    time.Duration
	logger          *logrus.Logger
	tracer          Tracer
	counters        EvalCounters
}

type EvaluatorConfig struct {
	Storage       storage.Store
	Cache         *PolicyCache
	Optimizer     *QueryOptimizer
	PerformanceMode PerformanceMode
	QueryTimeout  time.Duration
	Logger        *logrus.Logger
	Tracer        Tracer
}

// NewEvaluator creates optimized evaluator instance
func NewEvaluator(cfg *EvaluatorConfig) *Evaluator {
	if cfg == nil {
		cfg = &EvaluatorConfig{
			PerformanceMode: StandardMode,
			QueryTimeout:    2 * time.Second,
			Logger:          logrus.New(),
		}
	}

	eval := &Evaluator{
		storage:         cfg.Storage,
		cache:           cfg.Cache,
		optimizer:       cfg.Optimizer,
		preparedQueries: make(map[string]*regio.Query),
		mode:            cfg.PerformanceMode,
		queryTimeout:    cfg.QueryTimeout,
		logger:          cfg.Logger,
		tracer:          cfg.Tracer,
	}

	return eval
}

// Evaluate performs policy evaluation for given input
func (e *Evaluator) Evaluate(ctx context.Context, queries []string, input interface{}) (*EvaluationResult, error) {
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(ctx, e.queryTimeout)
	defer cancel()

	result := &EvaluationResult{
		Requests: make([]interface{}, 0),
		Decisions: make([]*Decision, 0),
		Context: EvalContext{
			StartTime:     startTime,
			EndTime:       time.Now(),
			EngineVersion: Version,
			CacheEnabled:  e.cache != nil && e.cache.Size() > 0,
			ResourceUsage: ResourceUsageDetails{},
		},
	}

	e.counters.totalEval++
	e.mu.RLock()
	performanceMode := e.mode
	e.mu.RUnlock()

	for i, query := range queries {
		reqStart := time.Now()

		cachedQuery := e.optimizer.Optimize(query)
		cachedEntry, found := e.cache.Get(cachedQuery.Hash)
		
		if found && !cachedEntry.isExpired() {
			result.Decisions = append(result.Decisions, e.evaluateFromCache(input, cachedEntry, query))
			e.counters.cacheHit++
			result.Context.ResourceUsage.CacheHits++
		} else {
			decision := e.evaluateFresh(ctx, query, input)
			result.Decisions = append(result.Decisions, decision)
			e.counters.cacheMiss++
			result.Context.ResourceUsage.CacheMisses++
			
			if cachedEntry != nil {
				e.cache.Put(cachedQuery.Hash, cachedEntry)
			}
		}

		result.Requests = append(result.Requests, map[string]any{
			"query_index": i,
			"execution_time_ms": time.Since(reqStart).Milliseconds(),
		})
	}

	result.Context.EndTime = time.Now()
	result.Context.ResourceUsage.EvaluationTimeMs = time.Since(startTime)

	result.Summary = e.computeSummary(result)
	return result, nil
}

// evaluateFromCache retrieves cached decision
func (e *Evaluator) evaluateFromCache(input interface{}, entry *cachedEntry, originalQuery string) *Decision {
	if entry == nil || len(entry.policies) == 0 {
		return &Decision{
			DecisionType: "deny",
			Message:      "no_policy_found",
		}
	}

	policy := entry.policies[0]
	return &Decision{
		ID:             fmt.Sprintf("%s-%d", policy.Checksum, time.Now().UnixNano()),
		DecisionType:   "allow",
		RegoQuery:      originalQuery,
		PolicyRef:      policy.Name,
		Namespace:      policy.Namespace,
		Score:          1.0,
		Timestamp:      time.Now(),
		ExecutionTime:  0,
		CacheHit:       true,
		Version:        policy.Version,
		Metadata:       policy.Metadata,
	}
}

// evaluateFresh performs fresh policy evaluation
func (e *Evaluator) evaluateFresh(ctx context.Context, query string, input interface{}) *Decision {
	evalStart := time.Now()

	regoObj := rego.New(
		rego.Query(query),
		rego.Input(input),
		rego.Compiler(e.precompileModules()),
		rego.Storage(e.storage),
	)

	results, err := regoObj.Eval(ctx)
	execTime := time.Since(evalStart)

	decision := &Decision{
		ID:            fmt.Sprintf("eval-%d", time.Now().UnixNano()),
		DecisionType:  "warn",
		RegoQuery:     query,
		Timestamp:     time.Now(),
		ExecutionTime: execTime,
		CacheHit:      false,
		Version:       Version,
	}

	if err != nil {
		decision.Message = err.Error()
		decision.DecisionType = "deny"
		return decision
	}

	if len(results) > 0 && len(results[0].Bindings) > 0 {
		if allow, ok := results[0].Bindings["allow"]; ok {
			if allow.(bool) {
				decision.DecisionType = "allow"
				decision.Score = 1.0
			} else {
				decision.DecisionType = "deny"
				decision.Score = 0.0
			}
		}
	}

	return decision
}

// ============================================================================
// Query Optimizer - Rego Query Performance Tuning
// ============================================================================

// QueryOptimizer analyzes and optimizes Rego queries
type QueryOptimizer struct {
	queryPatterns map[string]*PatternInfo
	cacheHitRate  float64
	metrics       OptimizationMetrics
}

type PatternInfo struct {
	PatternID      string
	CachingKey     string
	ExpectedInput  interface{}
	CallbackFunc   func(interface{}) interface{}
	AverageLatency time.Duration
}

// NewQueryOptimizer creates query optimizer
func NewQueryOptimizer() *QueryOptimizer {
	return &QueryOptimizer{
		queryPatterns: make(map[string]*PatternInfo),
		cacheHitRate:  0.85,
		metrics: OptimizationMetrics{
			QueryCount: 0,
			CacheHits:  0,
		},
	}
}

// Optimize transforms raw query into optimized form
func (qo *QueryOptimizer) Optimize(rawQuery string) *OptimizedQuery {
	qo.metrics.QueryCount++
	
	hash := qo.calculateHash(rawQuery)
	
	if pattern, exists := qo.queryPatterns[hash]; exists {
		return &OptimizedQuery{
			RawQuery:  rawQuery,
			Optimized: pattern.CallbackFunc(rawQuery),
			PatternID: pattern.PatternID,
			Hash:      hash,
			CostScore: 0.3,
		}
	}

	optimized := qo.createOptimizedQuery(rawQuery)
	qo.storePattern(hash, optimized)
	
	return optimized
}

// calculateHash creates query fingerprint
func (qo *QueryOptimizer) calculateHash(query string) string {
	sum := md5.Sum([]byte(query))
	return fmt.Sprintf("%x", sum)[:16]
}

// createOptimizedQuery applies optimization rules
func (qo *QueryOptimizer) createOptimizedQuery(rawQuery string) *OptimizedQuery {
	return &OptimizedQuery{
		RawQuery:  rawQuery,
		Optimized: rawQuery,
		Hash:      qo.calculateHash(rawQuery),
		CostScore: 1.0,
	}
}
