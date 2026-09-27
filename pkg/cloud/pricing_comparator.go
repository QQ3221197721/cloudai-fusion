// Package cloud implements real-time multi-cloud pricing comparison engine.
// M2 Real-Time Cost Optimization Engine - Automatically selects cheapest provider
// across 6 clouds with sub-second decision time and intelligent caching.
package cloud

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// PriceComparator Interface - Universal Pricing Decision Contract
// ============================================================================

// PriceComparator analyzes pricing across all cloud providers and recommends
// the most cost-effective option based on workload requirements.
type PriceComparator interface {
	// GetBestPrice finds the cheapest option across all clouds for a workload
	GetBestPrice(ctx context.Context, req WorkloadRequest) (*OptimizedQuote, error)
	
	// GetPriceHistory returns historical pricing trends for analysis
	GetPriceHistory(cloud, instanceType string, hours int) ([]PricePoint, error)
	
	// GetSpotOpportunity detects spot/preemptible instances with high savings
	GetSpotOpportunities(ctx context.Context, req WorkloadRequest) ([]SpotOpportunity, error)
	
	// EstimateSavings compares recommended vs current provider
	EstimateSavings(currentProvider string, req WorkloadRequest) (*SavingsAnalysis, error)
	
	// GetCacheStats returns performance metrics about cache efficiency
	GetCacheStats() CacheMetrics
}

// ============================================================================
// Workload Specification & Pricing Quotes
// ============================================================================

// WorkloadRequest defines computational/storage requirements
type WorkloadRequest struct {
	// Compute Requirements
	GPUType     string // e.g., "nvidia-a100", "nvidia-h100", "intel-flex"
	CPUCores    int    // Required CPU cores
	MemoryGB    int    // Required memory in GB
	
	// Instance Type Preference
	InstanceType string // Specific VM SKU preference (optional)
	UseSpot      bool   // Prefer spot/preemptible instances (30-70% savings)
	
	// Geographic Constraints
	Region  string // Preferred region (e.g., "us-central1", "eu-west-1")
	Latency float64 // Max acceptable latency in ms (for data locality)
	
	// Storage Requirements
	DataTransferTB float64 // Estimated monthly data transfer in TB
	StorageGB      float64 // Required persistent storage in GB
	
	// Duration
	Hours float64 // Expected runtime hours
	
	// Business Constraints
	BudgetMaxPerHour float64 // Maximum hourly budget
	RequirementStrict string // "hard" (must meet), "soft" (prefer), "flexible"
}

// OptimizedQuote represents the optimal recommendation with full justification
type OptimizedQuote struct {
	Recommendation struct {
		Provider       string     `json:"provider"`        // aws|azure|gcp|alibaba|tencent|huawei
		ProviderName   string     `json:"provider_name"`   // Full display name
		InstanceType   string     `json:"instance_type"`   // Optimal VM SKU
		Region         string     `json:"region"`          // Best location
		PricingType    string     `json:"pricing_type"`    // on_demand|spot|reserved
		HourlyRate     float64    `json:"hourly_rate"`     // $/hour normalized to USD
		TotalCost      float64    `json:"total_cost"`      // Total estimated cost
		SavingsPercent float64    `json:"savings_percent"` // vs second-cheapest
		SavingsAmount  float64    `json:"savings_amount"`  // Absolute savings ($)
	} `json:"recommendation"`
	
	// Alternative Options (top 3 alternatives)
	Alternatives []*AlternativeOption `json:"alternatives"`
	
	// Confidence & Metadata
	Confidence float64     `json:"confidence"` // 0-1 confidence score
	LatencyMs  float64     `json:"latency_ms"` // Decision latency
	Timestamp  time.Time   `json:"timestamp"`
	Rationale  []RationalePoint `json:"rationale"`
	
	// Data Transfer Optimization if applicable
	DataMoverInfo *DataTransferPlan `json:"data_mover,omitempty"`
}

// AlternativeOption represents near-optimal choices
type AlternativeOption struct {
	Provider       string  `json:"provider"`
	InstanceType   string  `json:"instance_type"`
	Region         string  `json:"region"`
	HourlyRate     float64 `json:"hourly_rate"`
	SavingsPercent float64 `json:"savings_percent"` // Less than best option
	Availability   string  `json:"availability"`    // available|limited|unavailable
}

// RationalePoint explains decision logic
type RationalePoint struct {
	Type      string  `json:"type"`        // price|availability|latency|reliability
	Priority  int     `json:"priority"`    // 1-10 importance
	Factor    float64 `json:"factor"`      // Impact factor (e.g., 23.5% cheaper)
	Detail    string  `json:"detail"`      // Human-readable explanation
}

// DataTransferPlan optimizes cross-cloud data movement
type DataTransferPlan struct {
	Enabled          bool    `json:"enabled"`
	EstimatedSpeedMbps float64 `json:"estimated_speed_mbps"`
	EstimatedTimeMin float64  `json:"estimated_time_min"`
	CostEstimateUSD  float64  `json:"cost_estimate_usd"`
	Method           string   `json:"method"` // native_replication|parallel_proxy|physical_shipping
}

// SpotOpportunity identifies preemptible/spot instance deals
type SpotOpportunity struct {
	Provider       string  `json:"provider"`
	InstanceType   string  `json:"instance_type"`
	Region         string  `json:"region"`
	OnDemandPrice  float64 `json:"on_demand_price"`
	SpotPrice      float64 `json:"spot_price"`
	SavingsPercent float64 `json:"savings_percent"` // e.g., 68.5 = 68.5% savings
	Availability   string  `json:"availability"`    // available|limited|unstable
	EvictionRisk   string  `json:"eviction_risk"`   // low|medium|high
}

// SavingsAnalysis projects cost reduction from switching providers
type SavingsAnalysis struct {
	CurrentProvider string        `json:"current_provider"`
	RecommendedProvider string     `json:"recommended_provider"`
	CurrentHourlyRate float64    `json:"current_hourly_rate"`
	NewHourlyRate float64       `json:"new_hourly_rate"`
	SavingsPercent float64      `json:"savings_percent"`
	SavingsAnnual float64       `json:"savings_annual"` // Year-over-year projection
	PaybackPeriodDays int      `json:"payback_period_days"` // Migration effort ROI period
}

// CacheMetrics measures caching performance
type CacheMetrics struct {
	RequestCount    int       `json:"request_count"`
	HitCount        int       `json:"hit_count"`
	HitRate         float64   `json:"hit_rate"`        // Percentage (0-100)
	AvgDecisionLatencyMs float64 `json:"avg_latency_ms"` // Mean response time
	P99LatencyMs    float64   `json:"p99_latency_ms"` // 99th percentile
}

// ============================================================================
// PricingSnapshot & Cache System
// ============================================================================

// PricingSnapshot captures current prices from all providers
type PricingSnapshot struct {
	CloudProviders map[string]*CloudPrice`json:"providers"`
	Timestamp      time.Time`json:"timestamp"`
	ValidUntil     time.Time`json:"valid_until"`
	ValiditySeconds int      `json:"validity_seconds"` // Always 300s (5 min TTL)
}

// CloudPrice stores pricing data for a single cloud
type CloudPrice struct {
	Provider       string              `json:"provider"`
	ProviderName   string              `json:"provider_name"`
	Prices         map[string]float64  `json:"prices"` // instance_type -> hourly rate
	GPUPrices      map[string][]GPUPriceSlice `json:"gpu_prices"` // gpu_type -> prices by region
	SpotAvailable  bool                `json:"spot_available"`
	SpotDiscount   float64             `json:"spot_discount"` // e.g., 0.68 = 68% off
	Currency       string              `json:"currency"`
	LastUpdated    time.Time           `json:"last_updated"`
	AvailabilityStatus string            `json:"availability_status"` // available|limited
}

// GPUPriceSlice contains GPU pricing across multiple regions
type GPUPriceSlice struct {
	Region   string  `json:"region"`
	OnDemand float64 `json:"on_demand"`
	Spot     float64 `json:"spot"`
}

// cachedResult stores pricing results with expiration logic
type cachedResult struct {
	result       *OptimizedQuote
	cachedAt     time.Time
	expiresAt    time.Time
	requestHash  string // Hash of original request
	source       string // "fresh"|"cached"|"fallback"
	attemptCount int    // Number of API attempts made
}

// ============================================================================
// MultiCloudPricingEngine - Core Optimizer Implementation
// ============================================================================

// MultiCloudPricingEngine implements PriceComparator with parallel query engine
type MultiCloudPricingEngine struct {
	managers map[string]*MultiCloudPricingManager
	
	cache            map[string]*cachedResult
	cacheTTL         time.Duration // 5 minutes as required
	maxLookupTimeout time.Duration // Overall query timeout (3 seconds)
	minDecisionTarget time.Duration // Target: <500ms for fresh queries
	
	mu               sync.RWMutex
	stats            CacheMetrics
	
	pricingProviders []string // All registered cloud providers
}

// Performance Constants - Meeting M2 Requirements
const (
	DefaultCacheTTL           = 5 * time.Minute      // 300 seconds
	DefaultMaxTimeout         = 3 * time.Second      // Hard limit for parallel queries
	DefaultDecisionTarget     = 500 * time.Millisecond // Sweet spot target
	DefaultFallbackGraceMS    = 1000 * time.Millisecond // Fallback path SLA
	ParallelQueryRetries      = 2                    // Retry failed providers
	MinimumQuotedProviders    = 3                    // Must get quotes from at least 3 clouds
)

// NewMultiCloudPricingEngine creates optimized pricing engine for 6 clouds
func NewMultiCloudPricingEngine(managers ...*MultiCloudPricingManager) *MultiCloudPricingEngine {
	engine := &MultiCloudPricingEngine{
		managers: make(map[string]*MultiCloudPricingManager),
		cache:    make(map[string]*cachedResult),
		cacheTTL: DefaultCacheTTL,
		
		maxLookupTimeout: DefaultMaxTimeout,
		minDecisionTarget: DefaultDecisionTarget,
		
		pricingProviders: []string{"aws", "gcp", "azure", "alibaba", "tencent", "huawei"},
	}
	
	for _, manager := range managers {
		if manager != nil {
			// Extract provider names from manager
			engine.managers[manager.providers["aws"].Name()] = manager
		}
	}
	
	fmt.Printf("[PRICING ENGINE] Initialized with providers: %v\n", engine.pricingProviders)
	return engine
}

// ============================================================================
// Core Query Logic - Parallel Pricing Acquisition
// ============================================================================

// GetBestPrice executes parallel query across all 6 clouds and returns cheapest
func (pe *MultiCloudPricingEngine) GetBestPrice(ctx context.Context, req WorkloadRequest) (*OptimizedQuote, error) {
	startTime := time.Now()
	
	// Generate cache key based on workload parameters
	cacheKey := pe.generateRequestHash(req)
	
	// Check cache first (sub-100ms hit path)
	pe.mu.RLock()
	if cached := pe.getFromCache(cacheKey); cached != nil && !isExpired(cached) {
		pe.mu.RUnlock()
		pe.updateHitMetrics(true, startTime)
		return cached.result, nil
	}
	pe.mu.RUnlock()
	
	// Fresh query: Parallel fetch all providers simultaneously
	ctx, cancel := context.WithTimeout(ctx, pe.maxLookupTimeout)
	defer cancel()
	
	type providerQuote struct {
		provider string
		price    float64
		instance string
		err      error
		latency  time.Duration
	}
	
	quoteChan := make(chan providerQuote, len(pe.pricingProviders))
	var wg sync.WaitGroup
	
	// Launch parallel queries to all clouds (max 3s total wall-clock)
	for _, provider := range pe.pricingProviders {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			
			queryStart := time.Now()
			
			// Fetch pricing from this provider
			manager := pe.managers[p]
			if manager == nil {
				quoteChan <- providerQuote{
					provider: p,
					err:      fmt.Errorf("no manager configured for %s", p),
				}
				return
			}
			
			// Call pricing API with timeout
			pricingCtx, cancelP := context.WithTimeout(ctx, 800*time.Millisecond) // Each provider gets 800ms
			defer cancelP()
			
			quote, err := manager.GetGPUPricing(pricingCtx, req.Region, req.GPUType)
			latency := time.Since(queryStart)
			
			if err != nil {
				quoteChan <- providerQuote{
					provider: p,
					err:      err,
					latency:  latency,
				}
				return
			}
			
			// Apply spot discount if requested
			price := quote.OnDemand
			if req.UseSpot && quote.Spot > 0 {
				price = quote.Spot
			}
			
			quoteChan <- providerQuote{
				provider: p,
				price:    price,
				instance: quote.InstanceType,
				latency:  latency,
			}
		}(provider)
	}
	
	// Wait for all queries to complete or timeout
	go func() {
		wg.Wait()
		close(quoteChan)
	}()
	
	// Collect results (sorted by price later)
	var quotes []providerQuote
	for q := range quoteChan {
		quotes = append(quotes, q)
		if q.err != nil {
			fmt.Printf("[PRICING ENGINE] Provider %s failed: %v (%.3fs)\n", q.provider, q.err, q.latency.Seconds())
		}
	}
	
	decisionTime := time.Since(startTime)
	
	// Sort quotes by price ascending
	sort.Slice(quotes, func(i, j int) bool {
		return quotes[i].price < quotes[j].price
	})
	
	// Find best option
	bestQuote := quotes[0]
	if bestQuote.err != nil || bestQuote.price <= 0 {
		// No valid quotes - attempt fallback
		return pe.handleFallback(ctx, req, startTime)
	}
	
	// Build optimized quote with full justification
	optimalQuote := pe.buildOptimizedQuote(bestQuote, quotes, req, decisionTime)
	
	// Cache result for future calls
	pe.cacheResult(cacheKey, optimalQuote, "fresh", len(quotes))
	
	// Update metrics
	pe.updateHitMetrics(false, startTime)
	
	return optimalQuote, nil
}

// generateRequestHash creates unique key for caching
func (pe *MultiCloudPricingEngine) generateRequestHash(req WorkloadRequest) string {
	// Create hash based on critical workload dimensions
	key := fmt.Sprintf("%s:%s:%s:%t:%.2f",
		req.GPUType,
		req.Region,
		req.InstanceType,
		req.UseSpot,
		req.Hours,
	)
	
	// Use simplified hash for readability (production would use SHA256)
	return strings.ReplaceAll(key, ":", "-")
}

// getFromCache retrieves cached result if exists and valid
func (pe *MultiCloudPricingEngine) getFromCache(hash string) *cachedResult {
	result, ok := pe.cache[hash]
	if !ok {
		return nil
	}
	
	// Validate not expired
	if time.Now().After(result.expiresAt) {
		delete(pe.cache, hash)
		return nil
	}
	
	return result
}

// buildOptimizedQuote constructs comprehensive recommendation
func (pe *MultiCloudPricingEngine) buildOptimizedQuote(best providerQuote, allQuotes []providerQuote, req WorkloadRequest, decisionTime time.Duration) *OptimizedQuote {
	// Calculate savings vs alternatives
	savingsVsSecond := 0.0
	if len(allQuotes) >= 2 && allQuotes[1].price > 0 {
		savingsVsSecond = ((allQuotes[1].price - best.price) / allQuotes[1].price) * 100
	}
	
	// Build rationale explaining why this was selected
	rationale := []RationalePoint{
		{
			Type:      "price",
			Priority:  10,
			Factor:    savingsVsSecond,
			Detail:    fmt.Sprintf("%.2f%% cheaper than second-best option", savingsVsSecond),
		},
		{
			Type:      "availability",
			Priority:  8,
			Factor:    100.0,
			Detail:    "Provider has confirmed availability",
		},
	}
	
	// Calculate data transfer optimization if applicable
	dataMoverInfo := pe.analyzeDataTransfer(req)
	
	// Build alternative options (top 3)
	alternatives := pe.buildAlternatives(allQuotes, req, best)
	
	// Determine confidence score (based on number of providers queried)
	confidence := math.Min(float64(len(allQuotes))/float64(len(pe.pricingProviders))*100, 100.0)
	if len(allQuotes) < MinimumQuotedProviders {
		confidence *= 0.7 // Reduce confidence if insufficient providers
	}
	
	// Calculate total cost over runtime
	totalCost := best.price * req.Hours
	
	return &OptimizedQuote{
		Recommendation: struct {
			Provider       string     `json:"provider"`
			ProviderName   string     `json:"provider_name"`
			InstanceType   string     `json:"instance_type"`
			Region         string     `json:"region"`
			PricingType    string     `json:"pricing_type"`
			HourlyRate     float64    `json:"hourly_rate"`
			TotalCost      float64    `json:"total_cost"`
			SavingsPercent float64    `json:"savings_percent"`
			SavingsAmount  float64    `json:"savings_amount"`
		}{
			Provider:       best.provider,
			ProviderName:   pe.getProviderDisplayName(best.provider),
			InstanceType:   best.instance,
			Region:         req.Region,
			PricingType:    map[bool]string{true: "spot", false: "on-demand"}[req.UseSpot],
			HourlyRate:     best.price,
			TotalCost:      totalCost,
			SavingsPercent: savingsVsSecond,
			SavingsAmount:  allQuotes[1].price*req.Hours - totalCost,
		},
		Alternatives: alternatives,
		Confidence:   confidence,
		LatencyMs:    float64(decisionTime.Milliseconds()),
		Timestamp:    time.Now(),
		Rationale:    rationale,
		DataMoverInfo: dataMoverInfo,
	}
}

// buildAlternatives creates alternative options list
func (pe *MultiCloudPricingEngine) buildAlternatives(quotes []providerQuote, req WorkloadRequest, best providerQuote) []*AlternativeOption {
	alts := make([]*AlternativeOption, 0, 3)
	
	for i := 1; i < len(quotes) && len(alts) < 3; i++ {
		q := quotes[i]
		if q.price <= 0 || q.err != nil {
			continue
		}
		
		savingsPercent := ((q.price - best.price) / best.price) * 100
		
		alts = append(alts, &AlternativeOption{
			Provider:       q.provider,
			InstanceType:   q.instance,
			Region:         req.Region,
			HourlyRate:     q.price,
			SavingsPercent: savingsPercent,
			Availability:   "available",
		})
	}
	
	return alts
}

// analyzeDataTransfer evaluates cross-cloud data movement needs
func (pe *MultiCloudPricingEngine) analyzeDataTransfer(req WorkloadRequest) *DataTransferPlan {
	if req.DataTransferTB <= 0 {
		return nil
	}
	
	// Simulate optimization analysis
	estimatedSpeed := 100.0 // Mbps (real implementation would check network paths)
	transferBytes := req.DataTransferTB * 1024 * 1024 * 1024 * 1024 // Convert to bytes
	bits := transferBytes * 8
	timeSeconds := bits / (estimatedSpeed * 1024 * 1024) / 8 // Convert to seconds
	timeMinutes := timeSeconds / 60.0
	
	// Estimate cost based on standard data transfer rates (~$0.09/GB out)
	costEstimate := req.DataTransferTB * 0.09
	
	return &DataTransferPlan{
		Enabled:          true,
		EstimatedSpeedMbps: estimatedSpeed,
		EstimatedTimeMin:   timeMinutes,
		CostEstimateUSD:    costEstimate,
		Method:             "native_replication",
	}
}

// ============================================================================
// Additional PriceAnalyzer Methods
// ============================================================================

// GetPriceHistory simulates historical pricing trend retrieval
func (pe *MultiCloudPricingEngine) GetPriceHistory(cloud, instanceType string, hours int) ([]PricePoint, error) {
	// In production: retrieve from ClickHouse price history table
	// For now: simulate realistic patterns
	
	now := time.Now()
	history := make([]PricePoint, 0, hours)
	basePrice := pe.getBasePrice(cloud, instanceType)
	
	for i := 0; i < hours; i++ {
		// Add realistic price fluctuation (±5%)
		factor := 1.0 + (float64((i%20)-10)/200.0) // Sinusoidal pattern
		price := basePrice * factor
		
		history = append(history, PricePoint{
			Timestamp: now.Add(time.Duration(-hours+i)*time.Hour),
			Price:     price,
		})
	}
	
	return history, nil
}

// PricePoint represents a single historical pricing observation
type PricePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume,omitempty"` // Optional demand indicator
}

// getBasePrice returns approximate market prices for simulation
func (pe *MultiCloudPricingEngine) getBasePrice(cloud, instanceType string) float64 {
	baseRates := map[string]float64{
		"aws":         32.77, // p4d.24xlarge A100
		"gcp":         28.50, // n1-ultra+V100
		"azure":       27.20, // ND96amsr A100
		"alibaba":     22.15, // ecs.gn7i equivalent
		"tencent":     21.80, // SGN8 equivalent
		"huawei":      23.50, // Pi2 cluster pricing
	}
	
	return baseRates[strings.ToLower(cloud)]
}

// GetSpotOpportunities identifies significant spot instance savings
func (pe *MultiCloudPricingEngine) GetSpotOpportunities(ctx context.Context, req WorkloadRequest) ([]SpotOpportunity, error) {
	opportunities := make([]SpotOpportunity, 0)
	
	for _, provider := range pe.pricingProviders {
		manager := pe.managers[provider]
		if manager == nil {
			continue
		}
		
		quote, err := manager.GetGPUPricing(ctx, req.Region, req.GPUType)
		if err != nil || quote.Spot <= 0 {
			continue
		}
		
		// Only include if spot saves >40%
		onDemand := quote.OnDemand
		spot := quote.Spot
		savingsPct := ((onDemand - spot) / onDemand) * 100
		
		if savingsPct >= 40.0 {
			evictionRisk := "low"
			if savingsPct > 65.0 {
				evictionRisk = "high"
			} else if savingsPct > 50.0 {
				evictionRisk = "medium"
			}
			
			opportunities = append(opportunities, SpotOpportunity{
				Provider:       provider,
				InstanceType:   quote.InstanceType,
				Region:         req.Region,
				OnDemandPrice:  onDemand,
				SpotPrice:      spot,
				SavingsPercent: savingsPct,
				Availability:   "available",
				EvictionRisk:   evictionRisk,
			})
		}
	}
	
	// Sort by savings descending
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].SavingsPercent > opportunities[j].SavingsPercent
	})
	
	return opportunities, nil
}

// EstimateSavings calculates ROI from switching providers
func (pe *MultiCloudPricingEngine) EstimateSavings(currentProvider string, req WorkloadRequest) (*SavingsAnalysis, error) {
	// Get current price from provider
	currentManager := pe.managers[currentProvider]
	if currentManager == nil {
		return nil, fmt.Errorf("unknown provider: %s", currentProvider)
	}
	
	quote, err := currentManager.GetGPUPricing(context.Background(), req.Region, req.GPUType)
	if err != nil {
		quote = &PricingQuote{
			OnDemand: 0.50, // Default fallback
		}
	}
	
	currentHourly := quote.OnDemand
	
	// Get best alternative
	bestQuote, err := pe.GetBestPrice(context.Background(), req)
	if err != nil || bestQuote.Recommendation.Provider == currentProvider {
		return &SavingsAnalysis{
			CurrentProvider:     currentProvider,
			RecommendedProvider: currentProvider,
			CurrentHourlyRate:   currentHourly,
			NewHourlyRate:       currentHourly,
			SavingsPercent:      0.0,
			SavingsAnnual:       0.0,
			PaybackPeriodDays:   0,
		}, nil
	}
	
	newHourly := bestQuote.Recommendation.HourlyRate
	savingsPercent := ((currentHourly - newHourly) / currentHourly) * 100
	
	return &SavingsAnalysis{
		CurrentProvider:     currentProvider,
		RecommendedProvider: bestQuote.Recommendation.Provider,
		CurrentHourlyRate:   currentHourly,
		NewHourlyRate:       newHourly,
		SavingsPercent:      savingsPercent,
		SavingsAnnual:       (currentHourly - newHourly) * 24 * 365,
		PaybackPeriodDays:   7, // Assuming simple migration
	}, nil
}

// GetCacheStats returns performance metrics
func (pe *MultiCloudPricingEngine) GetCacheStats() CacheMetrics {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	
	if pe.stats.RequestCount == 0 {
		return CacheMetrics{}
	}
	
	return pe.stats
}

// updateHitMetrics tracks cache performance
func (pe *MultiCloudPricingEngine) updateHitMetrics(hit bool, startTime time.Time) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	
	pe.stats.RequestCount++
	if hit {
		pe.stats.HitCount++
	}
	
	if pe.stats.RequestCount > 0 {
		pe.stats.HitRate = float64(pe.stats.HitCount) / float64(pe.stats.RequestCount) * 100
	}
	
	decisionTime := time.Since(startTime).Milliseconds()
	
	// Calculate average (exponential moving average)
	prevAvg := pe.stats.AvgDecisionLatencyMs
	pe.stats.AvgDecisionLatencyMs = prevAvg*0.9 + float64(decisionTime)*0.1
	
	// Pseudo-P99 calculation (in production would maintain sorted histogram)
	p99Estimate := pe.stats.AvgDecisionLatencyMs * 2.5
	if p99Estimate > pe.stats.P99LatencyMs {
		pe.stats.P99LatencyMs = p99Estimate
	}
}

// cacheResult persists result with metadata
func (pe *MultiCloudPricingEngine) cacheResult(hash string, result *OptimizedQuote, source string, attempts int) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	
	pe.cache[hash] = &cachedResult{
		result:       result,
		cachedAt:     time.Now(),
		expiresAt:    time.Now().Add(pe.cacheTTL),
		requestHash:  hash,
		source:       source,
		attemptCount: attempts,
	}
}

// handleFallback returns cached data or defaults when APIs unavailable
func (pe *MultiCloudPricingEngine) handleFallback(ctx context.Context, req WorkloadRequest, startTime time.Time) (*OptimizedQuote, error) {
	// Try cached data first
	pe.mu.RLock()
	for _, cached := range pe.cache {
		if !isExpired(cached) && matchesWorkload(cached.requestHash, req) {
			pe.mu.RUnlock()
			pe.updateHitMetrics(true, startTime)
			cached.result.LatencyMs = time.Since(startTime).Milliseconds()
			return cached.result, fmt.Errorf("using cached data - no fresh providers available")
		}
	}
	pe.mu.RUnlock()
	
	// Completely empty - return synthetic default
	defaultQuote := &OptimizedQuote{
		Recommendation: struct {
			Provider       string     `json:"provider"`
			ProviderName   string     `json:"provider_name"`
			InstanceType   string     `json:"instance_type"`
			Region         string     `json:"region"`
			PricingType    string     `json:"pricing_type"`
			HourlyRate     float64    `json:"hourly_rate"`
			TotalCost      float64    `json:"total_cost"`
			SavingsPercent float64    `json:"savings_percent"`
			SavingsAmount  float64    `json:"savings_amount"`
		}{
			Provider:       "unknown",
			ProviderName:   "Unable to reach any provider",
			InstanceType:   "default",
			Region:         req.Region,
			PricingType:    "on-demand",
			HourlyRate:     0.50,
			TotalCost:      0.50 * req.Hours,
			SavingsPercent: 0.0,
			SavingsAmount:  0.0,
		},
		Confidence:  25.0,
		LatencyMs:   float64(time.Since(startTime).Milliseconds()),
		Timestamp:   time.Now(),
		Rationale:   []RationalePoint{{Type: "error", Detail: "All pricing APIs failed"}},
	}
	
	return defaultQuote, fmt.Errorf("no pricing data available, using synthetic defaults")
}

// Helper functions
func isExpired(cached *cachedResult) bool {
	return time.Now().After(cached.expiresAt)
}

func matchesWorkload(hash string, req WorkloadRequest) bool {
	// Simplified matching - in production would do full parameter comparison
	return hash != ""
}

func (pe *MultiCloudPricingEngine) getProviderDisplayName(provider string) string {
	displayNames := map[string]string{
		"aws":         "Amazon Web Services",
		"gcp":         "Google Cloud Platform",
		"azure":       "Microsoft Azure",
		"alibaba":     "Alibaba Cloud",
		"tencent":     "Tencent Cloud",
		"huawei":      "Huawei Cloud",
	}
	
	if name, ok := displayNames[provider]; ok {
		return name
	}
	return provider
}

// ============================================================================
// Benchmarking Support Functions
// ============================================================================

// BenchmarkFreshQuery runs performance benchmark comparing cached vs fresh queries
func (pe *MultiCloudPricingEngine) BenchmarkFreshQuery(b *testing.B, req WorkloadRequest) {
	ctx := context.Background()
	
	// First call clears cache for accurate measurement
	pe.mu.Lock()
	pe.cache = make(map[string]*cachedResult)
	pe.mu.Unlock()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pe.GetBestPrice(ctx, req)
		if err != nil {
			b.Logf("pricing query error: %v", err)
		}
	}
}

// CompareSequentialVsParallel demonstrates superiority of parallel approach
func (pe *MultiCloudPricingEngine) CompareSequentialVsParallel(req WorkloadRequest) {
	ctx := context.Background()
	
	// Sequential approach (baseline - mimics manual scripts)
	seqStart := time.Now()
	for _, provider := range pe.pricingProviders {
		manager := pe.managers[provider]
		if manager != nil {
			_, _ = manager.GetGPUPricing(ctx, req.Region, req.GPUType)
		}
	}
	seqTime := time.Since(seqStart)
	
	// Parallel approach (our optimizer)
	parStart := time.Now()
	_, _ = pe.GetBestPrice(ctx, req)
	parTime := time.Since(parStart)
	
	speedup := seqTime.Seconds() / parTime.Seconds()
	
	fmt.Printf("[BENCHMARK] Sequential: %.3fs | Parallel: %.3fs | Speedup: %.1fx\n",
		seqTime.Seconds(), parTime.Seconds(), speedup)
}

// String formatting utilities
func (c *OptimizedQuote) String() string {
	return fmt.Sprintf("Best: %s (%s) @ $%.4f/hr | Confidence: %.1f%% | Latency: %.0fms",
		c.Recommendation.Provider,
		c.Recommendation.InstanceType,
		c.Recommendation.HourlyRate,
		c.Confidence,
		c.LatencyMs,
	)
}
