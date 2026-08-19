// Package edgeautonomy - Delta sync with Merkle Tree for efficient data synchronization
package edge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"strconv"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// MERKLE TREE BASED DELTA SYNCHRONIZATION SYSTEM
// IMPLEMENTS EFFICIENT CHANGE DETECTION AND SYNC!
// ============================================================================

// DeltaSyncManager orchestrates distributed data synchronization using Merkle Trees
type DeltaSyncManager struct {
	mu       sync.RWMutex
	logger   *logrus.Logger
	
	// Root Merkle tree
	rootTree *SyncMerkleTree
	
	// Node registry
	nodes map[string]*DeltaEdgeNode
	
	// Sync state tracking
	syncSessions map[string]*SyncSession
	
	// Metrics
	metrics *DeltaMetrics
	
	// Configuration
	config SyncConfig
}

// DeltaEdgeNode represents an edge computing node in the delta sync system
type DeltaEdgeNode struct {
	ID             string            `json:"id"`
	Address        string            `json:"address"`
	Port           int               `json:"port"`
	Status         NodeStatus        `json:"status"`
	Version        time.Time         `json:"version"`
	LastSeen       time.Time         `json:"last_seen"`
	DataHashes     []string          `json:"data_hashes,omitempty"`
	MerkleRoot     string            `json:"merkle_root"`
	
	// Capabilities
	Capabilities   NodeCapabilities  `json:"capabilities"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

// NodeStatus describes node health
type NodeStatus string

const (
	StatusOnline    NodeStatus = "online"
	StatusOffline   NodeStatus = "offline"
	StatusSyncing   NodeStatus = "syncing"
	StatusDegraded  NodeStatus = "degraded"
)

// NodeCapabilities describes node capabilities
type NodeCapabilities struct {
	MaxDataSizeMB int `json:"max_data_size_mb"`
	SupportsMerge bool `json:"supports_merge"`
	EncryptionKey bool `json:"encryption_key"`
}

// SyncSession tracks active synchronization session
type SyncSession struct {
	ID              string            `json:"id"`
	SourceNode      string            `json:"source_node"`
	DestNode        string            `json:"dest_node"`
	Status          SessionStatus     `json:"status"`
	StartTime       time.Time         `json:"start_time"`
	EndTime         time.Time         `json:"end_time,omitempty"`
	BytesTransferred int64            `json:"bytes_transferred"`
	ChangesDetected int               `json:"changes_detected"`
	Errors          []string          `json:"errors,omitempty"`
	
	// Delta information
	ChangedBlocks   []BlockDelta      `json:"changed_blocks,omitempty"`
	UnchangedBlocks []string          `json:"unchanged_blocks,omitempty"`
}

// SessionStatus describes sync session status
type SessionStatus string

const (
	SessionPending  SessionStatus = "pending"
	SessionActive   SessionStatus = "active"
	SessionComplete SessionStatus = "complete"
	SessionFailed   SessionStatus = "failed"
)

// BlockDelta represents changed block in sync
type BlockDelta struct {
	BlockID    string `json:"block_id"`
	OldHash    string `json:"old_hash"`
	NewHash    string `json:"new_hash"`
	Size       int64  `json:"size_bytes"`
	Timestamp  int64  `json:"timestamp"`
}

// ChangeResult describes the outcome of a vector-clock-based change
type ChangeResult struct {
	Applied    bool   `json:"applied"`
	Key        string `json:"key,omitempty"`
	OldValue   interface{} `json:"old_value,omitempty"`
	NewValue   interface{} `json:"new_value,omitempty"`
	Skipped    bool   `json:"skipped,omitempty"`
	Reason     string `json:"reason,omitempty"`
	IsNew      bool   `json:"is_new,omitempty"`
	Conflicted bool   `json:"conflicted,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	Winner     *VectorClockChange `json:"winner,omitempty"`
}

// SyncConfig defines sync parameters
type SyncConfig struct {
	BlocksizeKB         int           `json:"block_size_kb"`
	ParallelSyncWorkers int           `json:"parallel_workers"`
	CompressionEnabled  bool          `json:"compression_enabled"`
	EncryptionAlgorithm string        `json:"encryption_algorithm"`
	RetryAttempts       int           `json:"retry_attempts"`
	TimeoutSec          int           `json:"timeout_sec"`
}

// ============================================================================
// CORE MERKLE TREE IMPLEMENTATION
// ============================================================================

// SyncMerkleTree implements Merkle tree for data integrity verification in delta sync
type SyncMerkleTree struct {
	root     hash.Hash
	leaves   [][]byte
	height   int
	hashes   [][]byte
	size     int
	
	hashCache map[string][]byte
}

// NewSyncMerkleTree creates new merkle tree for delta sync
func NewSyncMerkleTree(config SyncConfig, logger *logrus.Logger) *SyncMerkleTree {
	tree := &SyncMerkleTree{
		root:      sha256.New(),
		leaves:    make([][]byte, 0),
		hashes:    make([][]byte, 0),
		size:      config.BlocksizeKB * 1024, // Convert to bytes
		hashCache: make(map[string][]byte),
	}
	
	return tree
}

// AddLeaf adds data leaf to tree
func (mt *SyncMerkleTree) AddLeaf(data []byte) {
	// Hash the leaf correctly using SHA256
	leafHash := sha256.Sum256(data)
	
	// Cache the hash
	key := fmt.Sprintf("%d", len(mt.leaves))
	mt.hashCache[key] = leafHash[:]
	
	// Store leaf and hash
	mt.leaves = append(mt.leaves, data)
	mt.hashes = append(mt.hashes, leafHash[:])
}

// GetRoot returns root hash of tree
func (mt *SyncMerkleTree) GetRoot() []byte {
	if len(mt.hashes) == 0 {
		return nil
	}
	
	if len(mt.hashes) == 1 {
		return mt.hashes[0]
	}
	
	// Build tree bottom-up
	hashes := make([][]byte, len(mt.hashes))
	copy(hashes, mt.hashes)
	
	for len(hashes) > 1 {
		newHashes := make([][]byte, 0)
		
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				// Combine two hashes
				combined := append(hashes[i], hashes[i+1]...)
				hash := mt.hash(combined)
				newHashes = append(newHashes, hash)
			} else {
				// Odd number of hashes, duplicate last one
				hash := mt.hash(append(hashes[i], hashes[i]...))
				newHashes = append(newHashes, hash)
			}
		}
		
		hashes = newHashes
	}
	
	return hashes[0]
}

// VerifyBlock verifies if a specific block belongs to the tree
func (mt *SyncMerkleTree) VerifyBlock(blockIndex int, blockHash []byte, proof [][]byte) bool {
	if blockIndex >= len(mt.hashes) {
		return false
	}
	
	// Start with block hash
	currentHash := make([]byte, len(blockHash))
	copy(currentHash, blockHash)
	
	// Apply proof path
	for _, proofHash := range proof {
		// Determine position in proof (left or right sibling)
		if blockIndex%2 == 0 {
			// Current is left child
			currentHash = mt.hash(append(currentHash, proofHash...))
		} else {
			// Current is right child
			currentHash = mt.hash(append(proofHash, currentHash...))
		}
		
		blockIndex /= 2
	}
	
	// Compare with root
	rootHash := mt.GetRoot()
	return string(currentHash) == string(rootHash)
}

// hash computes SHA-256 hash of data
func (mt *SyncMerkleTree) hash(data []byte) []byte {
	result := sha256.Sum256(data)
	return result[:]
}

// ============================================================================
// DELTA SYNCHRONIZATION FUNCTIONS WITH VECTOR CLOCK SUPPORT
// ============================================================================

// VectorClockChange represents a versioned change with causal ordering
type VectorClockChange struct {
	Key       string                 `json:"key"`
	Value     interface{}            `json:"value"`
	Timestamp time.Time              `json:"timestamp"`
	NodeID    string                 `json:"node_id"`
	Clock     map[string]int         `json:"clock"`
	Operation string                 `json:"operation"` // create, update, delete
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// ChangeVector tracks changes per-key with their vector clocks
type ChangeVector struct {
	Changes []*VectorClockChange
}

// Apply applies a single change with vector-clock-aware merging
func (cv *ChangeVector) Apply(change *VectorClockChange) (*ChangeResult, error) {
	for i, existing := range cv.Changes {
		if existing.Key == change.Key {
			// Same key: compare vector clocks
			cmp := compareClocks(existing.Clock, change.Clock)
			switch cmp {
			case 1: // Existing is newer -> skip incoming
				return &ChangeResult{Applied: false, Skipped: true, Reason: "existing_newer"}, nil
			case -1: // Incoming is newer -> apply
				cv.Changes[i] = change
				return &ChangeResult{Applied: true, Key: change.Key, OldValue: existing.Value, NewValue: change.Value}, nil
			case 2: // Concurrent -> conflict resolution needed
				resolved := resolveConflict(existing, change)
				cv.Changes[i] = resolved.Winner
				return &ChangeResult{
					Applied:   resolved.Applied,
					Conflicted: true,
					Resolution: resolved.Resolution,
					Winner:     resolved.Winner,
				}, nil
			default: // Equal -> no-op
				return &ChangeResult{Applied: false, Skipped: true, Reason: "equal"}, nil
			}
		}
	}
	// New key -> append
	cv.Changes = append(cv.Changes, change)
	return &ChangeResult{Applied: true, Key: change.Key, IsNew: true}, nil
}

// Merge merges another change vector
func (cv *ChangeVector) Merge(other *ChangeVector) int {
	merged := 0
	for _, change := range other.Changes {
		result, _ := cv.Apply(change)
		if result.Applied {
			merged++
		}
	}
	return merged
}

// CompareWith returns comparison between two change vectors
func (cv *ChangeVector) CompareWith(other *ChangeVector) int {
	// Simple lexicographic comparison of change sets
	for i := 0; i < max(len(cv.Changes), len(other.Changes)); i++ {
		var c1, c2 *VectorClockChange
		if i < len(cv.Changes) {
			c1 = cv.Changes[i]
		}
		if i < len(other.Changes) {
			c2 = other.Changes[i]
		}
		if c1 == nil {
			return -1
		}
		if c2 == nil {
			return 1
		}
		// Compare keys
		if c1.Key < c2.Key {
			return -1
		}
		if c1.Key > c2.Key {
			return 1
		}
	}
	return 0
}

type ClockComparison int

const (
	ClockBefore ClockComparison = -1
	ClockAfter  ClockComparison = 1
	ClockEqual  ClockComparison = 0
	ClockConcurrent ClockComparison = 2
)

// compareClocks compares two vector clocks
func compareClocks(a, b map[string]int) ClockComparison {
	hasLess := false
	hasGreater := false
	
	allKeys := make(map[string]bool)
	for k := range a {
		allKeys[k] = true
	}
	for k := range b {
		allKeys[k] = true
	}
	
	for key := range allKeys {
		aVal := a[key]
		bVal := b[key]
		
		if aVal < bVal {
			hasLess = true
		} else if aVal > bVal {
			hasGreater = true
		}
		
		if hasLess && hasGreater {
			return ClockConcurrent
		}
	}
	
	if hasLess {
		return ClockBefore
	}
	if hasGreater {
		return ClockAfter
	}
	return ClockEqual
}

type ConflictResolution struct {
	Winner      *VectorClockChange
	Lost        *VectorClockChange
	Resolution  string
	Applied     bool
}

// resolveConflict resolves concurrent write conflicts using LWW.
// Ties on identical timestamps are broken deterministically by NodeID so
// that the same inputs always yield the same winner (required for testable
// convergence across replicas).
func resolveConflict(c1, c2 *VectorClockChange) *ConflictResolution {
	c1Wins := c1.Timestamp.After(c2.Timestamp)
	if c1.Timestamp.Equal(c2.Timestamp) {
		// Deterministic tie-break: higher NodeID wins
		c1Wins = c1.NodeID > c2.NodeID
	}
	if c1Wins {
		return &ConflictResolution{
			Winner:     c1,
			Lost:       c2,
			Resolution: "last_writer_wins",
			Applied:    true,
		}
	}
	return &ConflictResolution{
		Winner:     c2,
		Lost:       c1,
		Resolution: "last_writer_wins",
		Applied:    true,
	}
}

// NewDeltaSyncManager creates sync manager
func NewDeltaSyncManager(config SyncConfig, logger *logrus.Logger) (*DeltaSyncManager, error) {
	manager := &DeltaSyncManager{
		logger:         logger,
		rootTree:       NewSyncMerkleTree(config, logger),
		nodes:          make(map[string]*DeltaEdgeNode),
		syncSessions:   make(map[string]*SyncSession),
		metrics:        NewDeltaMetrics(),
		config:         config,
	}
	
	return manager, nil
}

// RegisterNode registers edge node in network
func (ds *DeltaSyncManager) RegisterNode(node *DeltaEdgeNode) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	
	// Check if node already exists
	if existing, ok := ds.nodes[node.ID]; ok {
		ds.logger.WithFields(logrus.Fields{
			"node": node.ID,
			"status": existing.Status,
		}).Warn("Node already registered")
		return fmt.Errorf("node %s already exists", node.ID)
	}
	
	node.Status = StatusOnline
	node.LastSeen = time.Now()
	node.Version = time.Now()
	
	ds.nodes[node.ID] = node
	ds.metrics.RecordNodeRegistered(node.ID)
	
	ds.logger.WithField("node", node.ID).Info("Edge node registered")
	return nil
}

// ComputeMerkleRoot computes root hash from data blocks
func (ds *DeltaSyncManager) ComputeMerkleRoot(ctx context.Context, nodeID string, blocks [][]byte) ([]byte, error) {
	node, exists := ds.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}
	
	// Clear existing leaves
	ds.rootTree.leaves = make([][]byte, 0)
	ds.rootTree.hashes = make([][]byte, 0)
	
	// Add all blocks as leaves
	for _, block := range blocks {
		ds.rootTree.AddLeaf(block)
	}
	
	// Compute and store root
	rootHash := ds.rootTree.GetRoot()
	node.MerkleRoot = fmt.Sprintf("%x", rootHash)
	
	ds.logger.WithFields(logrus.Fields{
		"node": nodeID,
		"blocks": len(blocks),
		"root": fmt.Sprintf("%x", rootHash[:16]),
	}).Info("Merkle root computed")
	
	return rootHash, nil
}

// DetectChanges detects delta between two Merkle trees using real hash comparison
func (ds *DeltaSyncManager) DetectChanges(ctx context.Context, sourceNode, destNode string) ([]*BlockDelta, error) {
	source, exists := ds.nodes[sourceNode]
	if !exists {
		return nil, fmt.Errorf("source node %s not found", sourceNode)
	}
	
	dest, exists := ds.nodes[destNode]
	if !exists {
		return nil, fmt.Errorf("destination node %s not found", destNode)
	}
	
	// Compare Merkle roots first (fast check)
	if source.MerkleRoot == dest.MerkleRoot {
		ds.logger.WithFields(logrus.Fields{
			"source": sourceNode,
			"dest": destNode,
		}).Debug("No changes detected")
		
		return []*BlockDelta{}, nil
	}
	
	// Roots differ - perform real block-level hash comparison
	changedBlocks := ds.calculateBlockDeltas(source, dest)
	
	ds.logger.WithFields(logrus.Fields{
		"source": sourceNode,
		"dest": destNode,
		"changes": len(changedBlocks),
	}).Info("Delta changes detected")
	
	return changedBlocks, nil
}

// compareDataHashes performs real comparison of block hashes between nodes
func (ds *DeltaSyncManager) compareDataHashes(source, dest *DeltaEdgeNode) []*BlockDelta {
	if len(source.DataHashes) != len(dest.DataHashes) {
		ds.logger.Warnf("Hash count mismatch: source=%d, dest=%d", len(source.DataHashes), len(dest.DataHashes))
	}
	
	changedBlocks := make([]*BlockDelta, 0)
	maxLen := max(len(source.DataHashes), len(dest.DataHashes))
	
	for i := 0; i < maxLen; i++ {
		var oldHash, newHash string
		var size int64 = 1024 // default block size
		
		if i < len(source.DataHashes) {
			oldHash = source.DataHashes[i]
		} else {
			oldHash = ""
		}
		
		if i < len(dest.DataHashes) {
			newHash = dest.DataHashes[i]
			if sizeBytes, ok := dest.Metadata[fmt.Sprintf("block_%d_size", i)]; ok {
				if v, err := strconv.ParseInt(sizeBytes, 10, 64); err == nil {
					size = v
				}
			}
		} else {
			newHash = ""
		}
		
		if oldHash != newHash {
			changedBlocks = append(changedBlocks, &BlockDelta{
				BlockID:   fmt.Sprintf("block_%d", i),
				OldHash:   oldHash,
				NewHash:   newHash,
				Size:      size,
				Timestamp: time.Now().UnixNano(),
			})
		}
	}
	
	return changedBlocks
}

// CalculateDeltaFromData calculates deltas directly from raw data slices
func (ds *DeltaSyncManager) CalculateDeltaFromData(dataHashes []string) []*BlockDelta {
	if dataHashes == nil {
		return []*BlockDelta{}
	}
	
	deltas := make([]*BlockDelta, 0, len(dataHashes))
	for i, hash := range dataHashes {
		deltas = append(deltas, &BlockDelta{
			BlockID:   fmt.Sprintf("block_%d", i),
			OldHash:   "",
			NewHash:   hash,
			Size:      1024,
			Timestamp: time.Now().UnixNano(),
		})
	}
	
	return deltas
}

// calculateBlockDeltas returns actual changed block list by comparing hash arrays
func (ds *DeltaSyncManager) calculateBlockDeltas(source, dest *DeltaEdgeNode) []*BlockDelta {
	return ds.compareDataHashes(source, dest)
}

// StartSync initiates synchronized data transfer
func (ds *DeltaSyncManager) StartSync(ctx context.Context, sourceNode, destNode string) (*SyncSession, error) {
	// Validate nodes
	_, exists := ds.nodes[sourceNode]
	if !exists {
		return nil, fmt.Errorf("source node %s not found", sourceNode)
	}
	
	_, exists = ds.nodes[destNode]
	if !exists {
		return nil, fmt.Errorf("destination node %s not found", destNode)
	}
	
	// Create sync session
	session := &SyncSession{
		ID:             fmt.Sprintf("sync_%s_%s_%d", sourceNode, destNode, time.Now().UnixNano()),
		SourceNode:     sourceNode,
		DestNode:       destNode,
		Status:         SessionActive,
		StartTime:      time.Now(),
		BytesTransferred: 0,
		ChangesDetected: 0,
	}
	
	ds.mu.Lock()
	ds.syncSessions[session.ID] = session
	ds.mu.Unlock()
	
	ds.metrics.RecordSyncStarted(session.ID)
	return session, nil
}

// CompleteSync finishes synchronization session
func (ds *DeltaSyncManager) CompleteSync(ctx context.Context, sessionID string, bytesSent int64, changesMade int) error {
	session, exists := ds.syncSessions[sessionID]
	if !exists {
		return fmt.Errorf("sync session %s not found", sessionID)
	}
	
	session.Status = SessionComplete
	session.EndTime = time.Now()
	session.BytesTransferred = bytesSent
	session.ChangesDetected = changesMade
	
	ds.metrics.RecordSyncCompleted(session.ID, session.Duration())
	
	ds.logger.WithFields(logrus.Fields{
		"session": sessionID,
		"bytes": bytesSent,
		"changes": changesMade,
		"duration": session.Duration().String(),
	}).Info("Sync completed successfully")
	
	return nil
}

// Helper functions
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
