// Package edge provides enhanced offline capabilities for edge nodes.
// Implements local caching with predictive prefetching, CRDT-based
// offline-first data synchronization, mDNS edge device discovery,
// and OTA upgrade with A/B partition grayscale rollout.
package edge

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Local Cache & Predictive Prefetch
// ============================================================================

// CachePolicy defines eviction strategy
type CachePolicy string

const (
	CachePolicyLRU  CachePolicy = "lru"
	CachePolicyLFU  CachePolicy = "lfu"
	CachePolicyTTL  CachePolicy = "ttl"
	CachePolicyARC  CachePolicy = "arc" // Adaptive Replacement Cache
)

// CacheConfig configures the local edge cache
type CacheConfig struct {
	MaxSizeMB        int           `json:"max_size_mb"`
	Policy           CachePolicy   `json:"policy"`
	DefaultTTL       time.Duration `json:"default_ttl"`
	PrefetchEnabled  bool          `json:"prefetch_enabled"`
	PrefetchAheadSec int           `json:"prefetch_ahead_sec"` // prefetch N seconds before predicted access
	HotDataThreshold int           `json:"hot_data_threshold"` // access count to mark as hot
	CompressionLevel int           `json:"compression_level"`  // 0=none, 1=fast, 9=best
}

// DefaultCacheConfig returns production-ready cache defaults
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSizeMB:        512,
		Policy:           CachePolicyARC,
		DefaultTTL:       24 * time.Hour,
		PrefetchEnabled:  true,
		PrefetchAheadSec: 300, // 5 minutes ahead
		HotDataThreshold: 5,
		CompressionLevel: 1,
	}
}

// CacheEntry represents a single cache entry
type CacheEntry struct {
	Key          string      `json:"key"`
	SizeBytes    int64       `json:"size_bytes"`
	DataType     string      `json:"data_type"` // model_weight, config, feature_data, embedding
	CreatedAt    time.Time   `json:"created_at"`
	LastAccess   time.Time   `json:"last_access"`
	AccessCount  int         `json:"access_count"`
	TTL          time.Duration `json:"ttl"`
	IsHot        bool        `json:"is_hot"`
	IsPrefetched bool        `json:"is_prefetched"`
	Compressed   bool        `json:"compressed"`
	Checksum     string      `json:"checksum"`
}

// PrefetchPrediction represents a predicted future access
type PrefetchPrediction struct {
	Key              string    `json:"key"`
	PredictedAccess  time.Time `json:"predicted_access"`
	Confidence       float64   `json:"confidence"` // 0-1
	DataType         string    `json:"data_type"`
	EstimatedSizeBytes int64   `json:"estimated_size_bytes"`
}

// CacheStats provides cache performance metrics
type CacheStats struct {
	TotalEntries   int     `json:"total_entries"`
	UsedSizeMB     float64 `json:"used_size_mb"`
	MaxSizeMB      int     `json:"max_size_mb"`
	HitRate        float64 `json:"hit_rate_percent"`
	MissRate       float64 `json:"miss_rate_percent"`
	EvictionCount  int64   `json:"eviction_count"`
	PrefetchHits   int64   `json:"prefetch_hits"`
	HotDataCount   int     `json:"hot_data_count"`
	Hits           int64   `json:"hits"`
	Misses         int64   `json:"misses"`
}

// EdgeCache manages local caching and prefetching
type EdgeCache struct {
	config       CacheConfig
	entries      map[string]*CacheEntry
	accessLog    []AccessRecord // for prefetch prediction
	stats        CacheStats
	mu           sync.RWMutex
	logger       *logrus.Logger
}

// AccessRecord tracks an access event for pattern learning
type AccessRecord struct {
	Key       string    `json:"key"`
	Timestamp time.Time `json:"timestamp"`
}

// NewEdgeCache creates a new edge cache
func NewEdgeCache(cfg CacheConfig, logger *logrus.Logger) *EdgeCache {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &EdgeCache{
		config:    cfg,
		entries:   make(map[string]*CacheEntry),
		accessLog: make([]AccessRecord, 0, 10000),
		stats:     CacheStats{MaxSizeMB: cfg.MaxSizeMB},
		logger:    logger,
	}
}

// Get retrieves a cache entry and records access
func (c *EdgeCache) Get(key string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.stats.Misses++
		return nil, false
	}

	// Check TTL expiry
	if time.Since(entry.CreatedAt) > entry.TTL {
		delete(c.entries, key)
		c.stats.Misses++
		return nil, false
	}

	entry.LastAccess = time.Now().UTC()
	entry.AccessCount++
	if entry.AccessCount >= c.config.HotDataThreshold {
		entry.IsHot = true
	}

	c.stats.Hits++
	if entry.IsPrefetched {
		c.stats.PrefetchHits++
	}

	// Record access for prefetch prediction
	c.accessLog = append(c.accessLog, AccessRecord{Key: key, Timestamp: entry.LastAccess})
	if len(c.accessLog) > 10000 {
		c.accessLog = c.accessLog[5000:]
	}

	return entry, true
}

// Put stores an entry in the cache, evicting if necessary
func (c *EdgeCache) Put(key string, sizeBytes int64, dataType string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if we need to evict
	currentSize := c.currentSizeBytes()
	maxSize := int64(c.config.MaxSizeMB) * 1024 * 1024

	for currentSize+sizeBytes > maxSize {
		if !c.evictOne() {
			return fmt.Errorf("cache full, cannot accommodate %d bytes", sizeBytes)
		}
		currentSize = c.currentSizeBytes()
	}

	c.entries[key] = &CacheEntry{
		Key:        key,
		SizeBytes:  sizeBytes,
		DataType:   dataType,
		CreatedAt:  time.Now().UTC(),
		LastAccess: time.Now().UTC(),
		TTL:        c.config.DefaultTTL,
	}
	c.stats.TotalEntries = len(c.entries)

	return nil
}

// evictOne removes one entry based on cache policy
func (c *EdgeCache) evictOne() bool {
	if len(c.entries) == 0 {
		return false
	}

	var evictKey string
	switch c.config.Policy {
	case CachePolicyLRU, CachePolicyARC:
		// Evict least recently accessed non-hot entry
		// Use !After (<=) instead of Before (<) for Windows timer coarseness
		var oldest time.Time
		first := true
		for k, e := range c.entries {
			if e.IsHot {
				continue // protect hot data
			}
			if first || !e.LastAccess.After(oldest) {
				oldest = e.LastAccess
				evictKey = k
				first = false
			}
		}
	case CachePolicyLFU:
		// Evict least frequently used non-hot entry
		minAccess := math.MaxInt32
		for k, e := range c.entries {
			if e.IsHot {
				continue
			}
			if e.AccessCount < minAccess {
				minAccess = e.AccessCount
				evictKey = k
			}
		}
	case CachePolicyTTL:
		// Evict entry closest to expiry
		closestExpiry := time.Duration(math.MaxInt64)
		for k, e := range c.entries {
			remaining := e.TTL - time.Since(e.CreatedAt)
			if remaining < closestExpiry {
				closestExpiry = remaining
				evictKey = k
			}
		}
	}

	if evictKey == "" {
		// All entries are hot, evict LRU hot entry
		var oldest time.Time
		first := true
		for k, e := range c.entries {
			if first || !e.LastAccess.After(oldest) {
				oldest = e.LastAccess
				evictKey = k
				first = false
			}
		}
	}

	if evictKey != "" {
		delete(c.entries, evictKey)
		c.stats.EvictionCount++
		c.stats.TotalEntries = len(c.entries)
		return true
	}
	return false
}

// GeneratePrefetchPredictions analyzes access patterns to predict future needs
func (c *EdgeCache) GeneratePrefetchPredictions() []*PrefetchPrediction {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.config.PrefetchEnabled || len(c.accessLog) < 10 {
		return nil
	}

	// Frequency-based prediction: keys accessed frequently in recent window
	recentWindow := 1 * time.Hour
	now := time.Now().UTC()
	freqMap := make(map[string]int)
	for _, rec := range c.accessLog {
		if now.Sub(rec.Timestamp) < recentWindow {
			freqMap[rec.Key]++
		}
	}

	predictions := make([]*PrefetchPrediction, 0)
	prefetchTime := now.Add(time.Duration(c.config.PrefetchAheadSec) * time.Second)

	for key, freq := range freqMap {
		if freq >= c.config.HotDataThreshold {
			// Already cached? Skip
			if _, exists := c.entries[key]; exists {
				continue
			}
			confidence := math.Min(float64(freq)/10.0, 1.0)
			predictions = append(predictions, &PrefetchPrediction{
				Key:             key,
				PredictedAccess: prefetchTime,
				Confidence:      confidence,
				DataType:        "predicted",
			})
		}
	}

	// Sort by confidence descending
	sort.Slice(predictions, func(i, j int) bool {
		return predictions[i].Confidence > predictions[j].Confidence
	})

	return predictions
}

// GetStats returns cache performance statistics
func (c *EdgeCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total) * 100
		stats.MissRate = float64(stats.Misses) / float64(total) * 100
	}
	stats.UsedSizeMB = float64(c.currentSizeBytes()) / 1024 / 1024

	hotCount := 0
	for _, e := range c.entries {
		if e.IsHot {
			hotCount++
		}
	}
	stats.HotDataCount = hotCount

	return stats
}

func (c *EdgeCache) currentSizeBytes() int64 {
	total := int64(0)
	for _, e := range c.entries {
		total += e.SizeBytes
	}
	return total
}

// ============================================================================
// CRDT-Based Offline Data Synchronization
// ============================================================================

// CRDTType defines the type of CRDT data structure
type CRDTType string

const (
	CRDTGCounter    CRDTType = "g_counter"     // Grow-only counter
	CRDTPNCounter   CRDTType = "pn_counter"    // Positive-Negative counter
	CRDTLWWRegister CRDTType = "lww_register"  // Last-Writer-Wins register
	CRDTORSet       CRDTType = "or_set"        // Observed-Remove set
	CRDTMVRegister  CRDTType = "mv_register"   // Multi-Value register
)

// CRDTConfig configures the CRDT sync engine
type CRDTConfig struct {
	SyncIntervalSec   int    `json:"sync_interval_sec"`
	MaxPendingOps     int    `json:"max_pending_ops"`
	ConflictStrategy  string `json:"conflict_strategy"` // crdt_merge, last_write_wins
	CompactionEnabled bool   `json:"compaction_enabled"`
	GCIntervalMin     int    `json:"gc_interval_min"`
}

// DefaultCRDTConfig returns production-ready CRDT defaults
func DefaultCRDTConfig() CRDTConfig {
	return CRDTConfig{
		SyncIntervalSec:   30,
		MaxPendingOps:     10000,
		ConflictStrategy:  "crdt_merge",
		CompactionEnabled: true,
		GCIntervalMin:     60,
	}
}

// GCounter implements a grow-only distributed counter
type GCounter struct {
	Counts map[string]int64 `json:"counts"` // nodeID -> count
}

// NewGCounter creates a new G-Counter
func NewGCounter() *GCounter {
	return &GCounter{Counts: make(map[string]int64)}
}

// Increment increases the counter for a node
func (g *GCounter) Increment(nodeID string, delta int64) {
	g.Counts[nodeID] += delta
}

// Value returns the total counter value
func (g *GCounter) Value() int64 {
	total := int64(0)
	for _, v := range g.Counts {
		total += v
	}
	return total
}

// Merge merges another G-Counter, taking max per node
func (g *GCounter) Merge(other *GCounter) {
	for nodeID, count := range other.Counts {
		if count > g.Counts[nodeID] {
			g.Counts[nodeID] = count
		}
	}
}

// PNCounter implements a positive-negative distributed counter
type PNCounter struct {
	Positive *GCounter `json:"positive"`
	Negative *GCounter `json:"negative"`
}

// NewPNCounter creates a new PN-Counter
func NewPNCounter() *PNCounter {
	return &PNCounter{
		Positive: NewGCounter(),
		Negative: NewGCounter(),
	}
}

// Increment adds to the positive counter
func (pn *PNCounter) Increment(nodeID string, delta int64) {
	pn.Positive.Increment(nodeID, delta)
}

// Decrement adds to the negative counter
func (pn *PNCounter) Decrement(nodeID string, delta int64) {
	pn.Negative.Increment(nodeID, delta)
}

// Value returns P - N
func (pn *PNCounter) Value() int64 {
	return pn.Positive.Value() - pn.Negative.Value()
}

// Merge merges another PN-Counter
func (pn *PNCounter) Merge(other *PNCounter) {
	pn.Positive.Merge(other.Positive)
	pn.Negative.Merge(other.Negative)
}

// LWWRegister implements a Last-Writer-Wins register
type LWWRegister struct {
	Value     interface{} `json:"value"`
	Timestamp time.Time   `json:"timestamp"`
	NodeID    string      `json:"node_id"`
}

// Set updates the register value
func (r *LWWRegister) Set(nodeID string, value interface{}) {
	now := time.Now().UTC()
	if now.After(r.Timestamp) {
		r.Value = value
		r.Timestamp = now
		r.NodeID = nodeID
	}
}

// Merge applies LWW semantics
func (r *LWWRegister) Merge(other *LWWRegister) {
	if other.Timestamp.After(r.Timestamp) {
		r.Value = other.Value
		r.Timestamp = other.Timestamp
		r.NodeID = other.NodeID
	}
}

// ORSet implements an Observed-Remove set (add-wins semantics)
type ORSet struct {
	Elements map[string]*ORSetElement `json:"elements"`
}

// ORSetElement represents an element in the OR-Set
type ORSetElement struct {
	Value    interface{}       `json:"value"`
	AddTags  map[string]bool   `json:"add_tags"`  // unique tags for adds
	RemTags  map[string]bool   `json:"rem_tags"`  // unique tags for removes
}

// NewORSet creates a new OR-Set
func NewORSet() *ORSet {
	return &ORSet{Elements: make(map[string]*ORSetElement)}
}

// Add adds an element with a unique tag
func (s *ORSet) Add(key, tag string, value interface{}) {
	elem, ok := s.Elements[key]
	if !ok {
		elem = &ORSetElement{
			Value:   value,
			AddTags: make(map[string]bool),
			RemTags: make(map[string]bool),
		}
		s.Elements[key] = elem
	}
	elem.Value = value
	elem.AddTags[tag] = true
}

// Remove removes an element (observed-remove: only removes seen adds)
func (s *ORSet) Remove(key string) {
	elem, ok := s.Elements[key]
	if !ok {
		return
	}
	// Move all current add tags to remove tags
	for tag := range elem.AddTags {
		elem.RemTags[tag] = true
	}
}

// Contains checks if an element is in the set (add_tags - rem_tags non-empty)
func (s *ORSet) Contains(key string) bool {
	elem, ok := s.Elements[key]
	if !ok {
		return false
	}
	for tag := range elem.AddTags {
		if !elem.RemTags[tag] {
			return true
		}
	}
	return false
}

// Merge merges another OR-Set
func (s *ORSet) Merge(other *ORSet) {
	for key, otherElem := range other.Elements {
		elem, ok := s.Elements[key]
		if !ok {
			elem = &ORSetElement{
				Value:   otherElem.Value,
				AddTags: make(map[string]bool),
				RemTags: make(map[string]bool),
			}
			s.Elements[key] = elem
		}
		for tag := range otherElem.AddTags {
			elem.AddTags[tag] = true
		}
		for tag := range otherElem.RemTags {
			elem.RemTags[tag] = true
		}
	}
}

// Values returns all active elements in the set
func (s *ORSet) Values() []interface{} {
	result := make([]interface{}, 0)
	for key := range s.Elements {
		if s.Contains(key) {
			result = append(result, s.Elements[key].Value)
		}
	}
	return result
}

// CRDTSyncEngine manages CRDT-based offline-first synchronization
type CRDTSyncEngine struct {
	config     CRDTConfig
	counters   map[string]*PNCounter     // key -> PN counter
	registers  map[string]*LWWRegister   // key -> LWW register
	sets       map[string]*ORSet         // key -> OR-Set
	pendingOps []CRDTOperation
	mu         sync.RWMutex
	logger     *logrus.Logger
}

// CRDTOperation represents a pending CRDT operation to sync
type CRDTOperation struct {
	ID        string      `json:"id"`
	Type      CRDTType    `json:"type"`
	Key       string      `json:"key"`
	NodeID    string      `json:"node_id"`
	Value     interface{} `json:"value"`
	Timestamp time.Time   `json:"timestamp"`
}

// NewCRDTSyncEngine creates a new CRDT sync engine
func NewCRDTSyncEngine(cfg CRDTConfig, logger *logrus.Logger) *CRDTSyncEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &CRDTSyncEngine{
		config:     cfg,
		counters:   make(map[string]*PNCounter),
		registers:  make(map[string]*LWWRegister),
		sets:       make(map[string]*ORSet),
		pendingOps: make([]CRDTOperation, 0),
		logger:     logger,
	}
}

// IncrementCounter increments a distributed counter
func (e *CRDTSyncEngine) IncrementCounter(key, nodeID string, delta int64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	counter, ok := e.counters[key]
	if !ok {
		counter = NewPNCounter()
		e.counters[key] = counter
	}

	if delta >= 0 {
		counter.Increment(nodeID, delta)
	} else {
		counter.Decrement(nodeID, -delta)
	}

	e.pendingOps = append(e.pendingOps, CRDTOperation{
		ID:        fmt.Sprintf("op-%d", len(e.pendingOps)),
		Type:      CRDTPNCounter,
		Key:       key,
		NodeID:    nodeID,
		Value:     delta,
		Timestamp: time.Now().UTC(),
	})
}

// SetRegister sets a LWW register value
func (e *CRDTSyncEngine) SetRegister(key, nodeID string, value interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()

	reg, ok := e.registers[key]
	if !ok {
		reg = &LWWRegister{}
		e.registers[key] = reg
	}
	reg.Set(nodeID, value)

	e.pendingOps = append(e.pendingOps, CRDTOperation{
		ID:        fmt.Sprintf("op-%d", len(e.pendingOps)),
		Type:      CRDTLWWRegister,
		Key:       key,
		NodeID:    nodeID,
		Value:     value,
		Timestamp: time.Now().UTC(),
	})
}

// AddToSet adds an element to an OR-Set
func (e *CRDTSyncEngine) AddToSet(key, elemKey, tag string, value interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()

	set, ok := e.sets[key]
	if !ok {
		set = NewORSet()
		e.sets[key] = set
	}
	set.Add(elemKey, tag, value)
}

// MergeRemote merges remote CRDT state received during sync
func (e *CRDTSyncEngine) MergeRemote(remoteOps []CRDTOperation) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	merged := 0
	for _, op := range remoteOps {
		switch op.Type {
		case CRDTPNCounter, CRDTGCounter:
			counter, ok := e.counters[op.Key]
			if !ok {
				counter = NewPNCounter()
				e.counters[op.Key] = counter
			}
			if delta, ok := op.Value.(float64); ok {
				if delta >= 0 {
					counter.Increment(op.NodeID, int64(delta))
				} else {
					counter.Decrement(op.NodeID, int64(-delta))
				}
			}
			merged++

		case CRDTLWWRegister:
			reg, ok := e.registers[op.Key]
			if !ok {
				reg = &LWWRegister{}
				e.registers[op.Key] = reg
			}
			remote := &LWWRegister{
				Value:     op.Value,
				Timestamp: op.Timestamp,
				NodeID:    op.NodeID,
			}
			reg.Merge(remote)
			merged++
		}
	}

	e.logger.WithField("merged_ops", merged).Debug("Merged remote CRDT operations")
	return merged
}

// DrainPendingOps returns and clears pending operations for sync
func (e *CRDTSyncEngine) DrainPendingOps() []CRDTOperation {
	e.mu.Lock()
	defer e.mu.Unlock()

	ops := make([]CRDTOperation, len(e.pendingOps))
	copy(ops, e.pendingOps)
	e.pendingOps = e.pendingOps[:0]
	return ops
}

// GetCounterValue returns the value of a distributed counter
func (e *CRDTSyncEngine) GetCounterValue(key string) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	counter, ok := e.counters[key]
	if !ok {
		return 0
	}
	return counter.Value()
}

// GetRegisterValue returns the value of a LWW register
func (e *CRDTSyncEngine) GetRegisterValue(key string) (interface{}, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	reg, ok := e.registers[key]
	if !ok {
		return nil, false
	}
	return reg.Value, true
}

// ============================================================================
// mDNS Edge Device Discovery
// ============================================================================

// MDNSConfig configures mDNS service discovery
type MDNSConfig struct {
	ServiceName     string `json:"service_name"`      // e.g., "_cloudai._tcp"
	Domain          string `json:"domain"`             // e.g., "local."
	Port            int    `json:"port"`
	BrowseIntervalSec int `json:"browse_interval_sec"`
	TTLSec          int    `json:"ttl_sec"`
	InstancePrefix  string `json:"instance_prefix"`    // e.g., "cloudai-edge-"
}

// DefaultMDNSConfig returns production-ready mDNS defaults
func DefaultMDNSConfig() MDNSConfig {
	return MDNSConfig{
		ServiceName:     "_cloudai-edge._tcp",
		Domain:          "local.",
		Port:            8082,
		BrowseIntervalSec: 30,
		TTLSec:          120,
		InstancePrefix:  "cloudai-edge-",
	}
}

// DiscoveredDevice represents an edge device found via mDNS
type DiscoveredDevice struct {
	InstanceName string            `json:"instance_name"`
	HostName     string            `json:"host_name"`
	IPAddresses  []string          `json:"ip_addresses"`
	Port         int               `json:"port"`
	TXTRecords   map[string]string `json:"txt_records"`
	DiscoveredAt time.Time         `json:"discovered_at"`
	LastSeenAt   time.Time         `json:"last_seen_at"`
	IsAlive      bool              `json:"is_alive"`
	NodeID       string            `json:"node_id,omitempty"` // linked edge node ID
}

// DeviceDiscovery manages mDNS-based edge device discovery
type DeviceDiscovery struct {
	config   MDNSConfig
	devices  map[string]*DiscoveredDevice // instanceName -> device
	mu       sync.RWMutex
	logger   *logrus.Logger
}

// NewDeviceDiscovery creates a new mDNS device discovery manager
func NewDeviceDiscovery(cfg MDNSConfig, logger *logrus.Logger) *DeviceDiscovery {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &DeviceDiscovery{
		config:  cfg,
		devices: make(map[string]*DiscoveredDevice),
		logger:  logger,
	}
}

// RegisterDevice registers a discovered device (from mDNS browse callback)
func (d *DeviceDiscovery) RegisterDevice(device *DiscoveredDevice) {
	d.mu.Lock()
	defer d.mu.Unlock()

	existing, ok := d.devices[device.InstanceName]
	if ok {
		existing.LastSeenAt = time.Now().UTC()
		existing.IsAlive = true
		existing.IPAddresses = device.IPAddresses
		return
	}

	device.DiscoveredAt = time.Now().UTC()
	device.LastSeenAt = device.DiscoveredAt
	device.IsAlive = true
	d.devices[device.InstanceName] = device

	d.logger.WithFields(logrus.Fields{
		"instance": device.InstanceName,
		"host":     device.HostName,
		"ips":      device.IPAddresses,
		"port":     device.Port,
	}).Info("Discovered edge device via mDNS")
}

// ListDevices returns all discovered devices
func (d *DeviceDiscovery) ListDevices(aliveOnly bool) []*DiscoveredDevice {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*DiscoveredDevice, 0, len(d.devices))
	for _, dev := range d.devices {
		if aliveOnly && !dev.IsAlive {
			continue
		}
		result = append(result, dev)
	}
	return result
}

// PruneStale marks devices as not alive if not seen within TTL
func (d *DeviceDiscovery) PruneStale() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	ttl := time.Duration(d.config.TTLSec) * time.Second
	now := time.Now().UTC()
	pruned := 0

	for _, dev := range d.devices {
		if dev.IsAlive && now.Sub(dev.LastSeenAt) > ttl {
			dev.IsAlive = false
			pruned++
			d.logger.WithField("instance", dev.InstanceName).Debug("Device marked stale")
		}
	}
	return pruned
}

// LinkToNode associates a discovered device with an edge node
func (d *DeviceDiscovery) LinkToNode(instanceName, nodeID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	dev, ok := d.devices[instanceName]
	if !ok {
		return fmt.Errorf("device %s not found", instanceName)
	}
	dev.NodeID = nodeID
	return nil
}

// StartBrowse starts mDNS browsing in the background
func (d *DeviceDiscovery) StartBrowse(ctx context.Context) {
	interval := time.Duration(d.config.BrowseIntervalSec) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	d.logger.WithFields(logrus.Fields{
		"service":  d.config.ServiceName,
		"domain":   d.config.Domain,
		"interval": interval,
	}).Info("Starting mDNS device discovery browse loop")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.PruneStale()
			// In production: trigger mDNS browse query
			// dns-sd.Browse(d.config.ServiceName, d.config.Domain, callback)
		}
	}
}

// ============================================================================
// OTA Upgrade — A/B Partition Grayscale Rollout
// ============================================================================

// OTAConfig configures over-the-air upgrade management
type OTAConfig struct {
	MaxConcurrentUpgrades int     `json:"max_concurrent_upgrades"`
	RollbackTimeoutMin    int     `json:"rollback_timeout_min"`
	HealthCheckIntervalSec int    `json:"health_check_interval_sec"`
	MinHealthyPercent     float64 `json:"min_healthy_percent"`  // minimum healthy nodes to continue rollout
	GrayscaleStages       []float64 `json:"grayscale_stages"`  // e.g., [0.01, 0.05, 0.25, 0.50, 1.0]
	ABEnabled             bool    `json:"ab_enabled"`           // A/B partition support
	AutoRollbackEnabled   bool    `json:"auto_rollback_enabled"`
}

// DefaultOTAConfig returns production-ready OTA defaults
func DefaultOTAConfig() OTAConfig {
	return OTAConfig{
		MaxConcurrentUpgrades:  10,
		RollbackTimeoutMin:     30,
		HealthCheckIntervalSec: 60,
		MinHealthyPercent:      95.0,
		GrayscaleStages:        []float64{0.01, 0.05, 0.25, 0.50, 1.0}, // 1% → 5% → 25% → 50% → 100%
		ABEnabled:              true,
		AutoRollbackEnabled:    true,
	}
}

// OTAPartition represents an A/B firmware partition
type OTAPartition string

const (
	PartitionA       OTAPartition = "A"
	PartitionB       OTAPartition = "B"
)

// FirmwareRelease represents a firmware/software release for OTA
type FirmwareRelease struct {
	ID             string    `json:"id"`
	Version        string    `json:"version"`
	Description    string    `json:"description"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	MinVersion     string    `json:"min_version,omitempty"`  // minimum required current version
	Components     []string  `json:"components"`             // e.g., ["agent", "runtime", "model-server"]
	ReleaseNotes   string    `json:"release_notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	IsStable       bool      `json:"is_stable"`
}

// RolloutPlan defines the grayscale rollout strategy
type RolloutPlan struct {
	ID               string    `json:"id"`
	ReleaseID        string    `json:"release_id"`
	CurrentStage     int       `json:"current_stage"`      // index into GrayscaleStages
	StagePercent     float64   `json:"stage_percent"`
	TotalNodes       int       `json:"total_nodes"`
	TargetNodes      int       `json:"target_nodes"`       // nodes to upgrade at this stage
	UpgradedNodes    int       `json:"upgraded_nodes"`
	HealthyNodes     int       `json:"healthy_nodes"`
	FailedNodes      int       `json:"failed_nodes"`
	RolledBackNodes  int       `json:"rolled_back_nodes"`
	State            string    `json:"state"`               // pending, rolling, paused, complete, rolled_back
	StartedAt        time.Time `json:"started_at"`
	LastStageAt      time.Time `json:"last_stage_at"`
	Error            string    `json:"error,omitempty"`
}

// NodeUpgradeStatus tracks per-node upgrade progress
type NodeUpgradeStatus struct {
	NodeID         string       `json:"node_id"`
	ReleaseID      string       `json:"release_id"`
	CurrentVersion string       `json:"current_version"`
	TargetVersion  string       `json:"target_version"`
	ActivePartition OTAPartition `json:"active_partition"`
	StagingPartition OTAPartition `json:"staging_partition"`
	State          string       `json:"state"` // pending, downloading, installing, verifying, active, rolled_back, failed
	Progress       float64      `json:"progress_percent"`
	HealthScore    float64      `json:"health_score"` // 0-100 after upgrade
	Error          string       `json:"error,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    *time.Time   `json:"completed_at,omitempty"`
}

// OTAManager manages over-the-air upgrades with A/B grayscale rollout
type OTAManager struct {
	config       OTAConfig
	releases     map[string]*FirmwareRelease    // releaseID -> release
	rollouts     map[string]*RolloutPlan        // rolloutID -> plan
	nodeStatuses map[string]*NodeUpgradeStatus  // nodeID -> status
	mu           sync.RWMutex
	logger       *logrus.Logger
}

// NewOTAManager creates a new OTA upgrade manager
func NewOTAManager(cfg OTAConfig, logger *logrus.Logger) *OTAManager {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &OTAManager{
		config:       cfg,
		releases:     make(map[string]*FirmwareRelease),
		rollouts:     make(map[string]*RolloutPlan),
		nodeStatuses: make(map[string]*NodeUpgradeStatus),
		logger:       logger,
	}
}

// RegisterRelease registers a new firmware release
func (o *OTAManager) RegisterRelease(release *FirmwareRelease) {
	o.mu.Lock()
	defer o.mu.Unlock()

	release.CreatedAt = time.Now().UTC()
	o.releases[release.ID] = release

	o.logger.WithFields(logrus.Fields{
		"release": release.ID,
		"version": release.Version,
		"size_mb": float64(release.SizeBytes) / 1024 / 1024,
	}).Info("Registered new firmware release for OTA")
}

// CreateRollout creates a grayscale rollout plan
func (o *OTAManager) CreateRollout(ctx context.Context, releaseID string, totalNodes int) (*RolloutPlan, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	release, ok := o.releases[releaseID]
	if !ok {
		return nil, fmt.Errorf("release %s not found", releaseID)
	}

	if len(o.config.GrayscaleStages) == 0 {
		return nil, fmt.Errorf("no grayscale stages configured")
	}

	firstStage := o.config.GrayscaleStages[0]
	targetNodes := int(math.Ceil(float64(totalNodes) * firstStage))
	if targetNodes < 1 {
		targetNodes = 1
	}

	plan := &RolloutPlan{
		ID:           fmt.Sprintf("rollout-%s-%d", releaseID, time.Now().Unix()),
		ReleaseID:    releaseID,
		CurrentStage: 0,
		StagePercent: firstStage * 100,
		TotalNodes:   totalNodes,
		TargetNodes:  targetNodes,
		State:        "pending",
		StartedAt:    time.Now().UTC(),
		LastStageAt:  time.Now().UTC(),
	}

	o.rollouts[plan.ID] = plan

	o.logger.WithFields(logrus.Fields{
		"rollout":      plan.ID,
		"release":      release.Version,
		"total_nodes":  totalNodes,
		"first_target": targetNodes,
		"stage":        fmt.Sprintf("%.0f%%", firstStage*100),
	}).Info("Created grayscale rollout plan")

	return plan, nil
}

// AdvanceStage moves to the next grayscale stage after health validation
func (o *OTAManager) AdvanceStage(rolloutID string) (*RolloutPlan, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	plan, ok := o.rollouts[rolloutID]
	if !ok {
		return nil, fmt.Errorf("rollout %s not found", rolloutID)
	}

	if plan.State != "rolling" && plan.State != "pending" {
		return nil, fmt.Errorf("rollout in state %s, cannot advance", plan.State)
	}

	// Validate health before advancing
	if plan.UpgradedNodes > 0 {
		healthyPercent := float64(plan.HealthyNodes) / float64(plan.UpgradedNodes) * 100
		if healthyPercent < o.config.MinHealthyPercent {
			if o.config.AutoRollbackEnabled {
				plan.State = "rolled_back"
				plan.Error = fmt.Sprintf("health check failed: %.1f%% < %.1f%% threshold", healthyPercent, o.config.MinHealthyPercent)
				o.logger.WithField("rollout", rolloutID).Warn("Auto-rollback triggered due to health check failure")
				return plan, fmt.Errorf("auto-rollback: %s", plan.Error)
			}
			return nil, fmt.Errorf("health %.1f%% below threshold %.1f%%", healthyPercent, o.config.MinHealthyPercent)
		}
	}

	// Move to next stage
	nextStage := plan.CurrentStage + 1
	if nextStage >= len(o.config.GrayscaleStages) {
		plan.State = "complete"
		o.logger.WithField("rollout", rolloutID).Info("Rollout complete - all stages finished")
		return plan, nil
	}

	stagePercent := o.config.GrayscaleStages[nextStage]
	targetNodes := int(math.Ceil(float64(plan.TotalNodes) * stagePercent))

	plan.CurrentStage = nextStage
	plan.StagePercent = stagePercent * 100
	plan.TargetNodes = targetNodes
	plan.State = "rolling"
	plan.LastStageAt = time.Now().UTC()

	o.logger.WithFields(logrus.Fields{
		"rollout": rolloutID,
		"stage":   nextStage,
		"percent": fmt.Sprintf("%.0f%%", stagePercent*100),
		"target":  targetNodes,
	}).Info("Advanced rollout to next stage")

	return plan, nil
}

// StartNodeUpgrade initiates A/B partition upgrade on an edge node
func (o *OTAManager) StartNodeUpgrade(nodeID, releaseID, currentVersion string, activePartition OTAPartition) (*NodeUpgradeStatus, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	release, ok := o.releases[releaseID]
	if !ok {
		return nil, fmt.Errorf("release %s not found", releaseID)
	}

	// Determine staging partition (opposite of active)
	var staging OTAPartition
	if activePartition == PartitionA {
		staging = PartitionB
	} else {
		staging = PartitionA
	}

	status := &NodeUpgradeStatus{
		NodeID:           nodeID,
		ReleaseID:        releaseID,
		CurrentVersion:   currentVersion,
		TargetVersion:    release.Version,
		ActivePartition:  activePartition,
		StagingPartition: staging,
		State:            "downloading",
		Progress:         0,
		StartedAt:        time.Now().UTC(),
	}

	o.nodeStatuses[nodeID] = status

	o.logger.WithFields(logrus.Fields{
		"node":    nodeID,
		"release": release.Version,
		"active":  activePartition,
		"staging": staging,
	}).Info("Started A/B partition OTA upgrade")

	return status, nil
}

// UpdateNodeProgress updates download/install progress
func (o *OTAManager) UpdateNodeProgress(nodeID string, progress float64, state string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	status, ok := o.nodeStatuses[nodeID]
	if !ok {
		return fmt.Errorf("no upgrade in progress for node %s", nodeID)
	}

	status.Progress = progress
	if state != "" {
		status.State = state
	}

	if state == "active" {
		now := time.Now().UTC()
		status.CompletedAt = &now
		// Swap partitions
		status.ActivePartition, status.StagingPartition = status.StagingPartition, status.ActivePartition
	}

	return nil
}

// RollbackNode rolls back an edge node to the previous partition
func (o *OTAManager) RollbackNode(nodeID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	status, ok := o.nodeStatuses[nodeID]
	if !ok {
		return fmt.Errorf("no upgrade found for node %s", nodeID)
	}

	// Swap back to original partition
	status.ActivePartition, status.StagingPartition = status.StagingPartition, status.ActivePartition
	status.State = "rolled_back"

	o.logger.WithFields(logrus.Fields{
		"node":     nodeID,
		"active":   status.ActivePartition,
		"reverted": status.CurrentVersion,
	}).Warn("Node OTA upgrade rolled back")

	return nil
}

// ReportHealth reports post-upgrade health score for a node
func (o *OTAManager) ReportHealth(nodeID string, healthScore float64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	status, ok := o.nodeStatuses[nodeID]
	if !ok {
		return fmt.Errorf("no upgrade found for node %s", nodeID)
	}

	status.HealthScore = healthScore

	// Update rollout stats
	for _, plan := range o.rollouts {
		if plan.ReleaseID == status.ReleaseID && plan.State == "rolling" {
			if healthScore >= 80 {
				plan.HealthyNodes++
			} else {
				plan.FailedNodes++
			}
		}
	}

	return nil
}

// GetRolloutStatus returns current rollout status
func (o *OTAManager) GetRolloutStatus(rolloutID string) *RolloutPlan {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.rollouts[rolloutID]
}

// GetNodeUpgradeStatus returns upgrade status for a node
func (o *OTAManager) GetNodeUpgradeStatus(nodeID string) *NodeUpgradeStatus {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.nodeStatuses[nodeID]
}

// ListReleases returns all registered releases
func (o *OTAManager) ListReleases() []*FirmwareRelease {
	o.mu.RLock()
	defer o.mu.RUnlock()

	result := make([]*FirmwareRelease, 0, len(o.releases))
	for _, r := range o.releases {
		result = append(result, r)
	}
	return result
}

// ============================================================================
// Offline Hub — Unified Offline Capabilities Access
// ============================================================================

// OfflineHub aggregates all offline capability subsystems
type OfflineHub struct {
	Cache           *EdgeCache
	CRDTSync        *CRDTSyncEngine
	DeviceDiscovery *DeviceDiscovery
	OTA             *OTAManager
	logger          *logrus.Logger
}

// OfflineHubConfig holds all offline capability configuration
type OfflineHubConfig struct {
	Cache      CacheConfig  `json:"cache"`
	CRDT       CRDTConfig   `json:"crdt"`
	MDNS       MDNSConfig   `json:"mdns"`
	OTA        OTAConfig    `json:"ota"`
}

// DefaultOfflineHubConfig returns production-ready defaults
func DefaultOfflineHubConfig() OfflineHubConfig {
	return OfflineHubConfig{
		Cache: DefaultCacheConfig(),
		CRDT:  DefaultCRDTConfig(),
		MDNS:  DefaultMDNSConfig(),
		OTA:   DefaultOTAConfig(),
	}
}

// NewOfflineHub creates and initializes all offline capability subsystems
func NewOfflineHub(cfg OfflineHubConfig, logger *logrus.Logger) *OfflineHub {
	return &OfflineHub{
		Cache:           NewEdgeCache(cfg.Cache, logger),
		CRDTSync:        NewCRDTSyncEngine(cfg.CRDT, logger),
		DeviceDiscovery: NewDeviceDiscovery(cfg.MDNS, logger),
		OTA:             NewOTAManager(cfg.OTA, logger),
		logger:          logger,
	}
}

// GetStatus returns a unified status of all offline subsystems
func (h *OfflineHub) GetStatus() map[string]interface{} {
	cacheStats := h.Cache.GetStats()
	devices := h.DeviceDiscovery.ListDevices(true)

	return map[string]interface{}{
		"cache": map[string]interface{}{
			"entries":  cacheStats.TotalEntries,
			"used_mb":  cacheStats.UsedSizeMB,
			"hit_rate": cacheStats.HitRate,
		},
		"crdt_pending_ops":    len(h.CRDTSync.pendingOps),
		"discovered_devices":  len(devices),
		"ota_releases":        len(h.OTA.releases),
		"ota_active_rollouts": len(h.OTA.rollouts),
	}
}
