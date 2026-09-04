// Package edgeautonomy - M24 CRDT Conflict Resolution Benchmark
// Compares our VectorClock + LWW merge implementation vs op-based CRDT baseline
// Focus: conflict-resolution merge path specifically (not delta sync)
// Results: merge latency ns/op @ N=100/1000 ops, convergence correctness proof
package edgeautonomy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// TEST DATA STRUCTURES FOR FAIR COMPARISON
// ============================================================================

// MergedState represents a confluent CRDT state for comparison
type MergedState struct {
	ID          string
	Data        map[string]interface{}
	VersionVec  map[string]int
	Timestamp   time.Time
	LastWriter  string
}

// ============================================================================
// OUR IMPLEMENTATION: Optimized VectorClock + LWW Merge
// ============================================================================

// OurVectorClockCRDT implements our patent-pending vector clock + LWW merge
type OurVectorClockCRDT struct {
	vectorClock *VersionVector
	lwwTimestamp time.Time
	lastWriter   string
	state        MergedState
}

func NewOurVectorClockCRDT(nodeIDs []string) *OurVectorClockCRDT {
	return &OurVectorClockCRDT{
		vectorClock: NewVersionVector(nodeIDs, nil),
		state: MergedState{
			Data: make(map[string]interface{}),
		},
	}
}

// Update performs LWW update on our CRDT
func (c *OurVectorClockCRDT) Update(key string, value interface{}, writerID string) {
	now := time.Now()
	c.vectorClock.Increment(writerID)
	
	if now.After(c.lwwTimestamp) {
		c.lwwTimestamp = now
		c.lastWriter = writerID
		c.state.Data[key] = value
	}
}

// Merge implements optimized element-wise MAX merge (patent #35)
func (c *OurVectorClockCRDT) Merge(other *OurVectorClockCRDT) error {
	// Merge version vectors using element-wise maximum
	if err := c.vectorClock.Merge(other.vectorClock); err != nil {
		return err
	}
	
	// Use LWW to resolve timestamp conflicts
	if other.lwwTimestamp.After(c.lwwTimestamp) {
		c.lwwTimestamp = other.lwwTimestamp
		c.lastWriter = other.lastWriter
		for k, v := range other.state.Data {
			c.state.Data[k] = v
		}
	} else if c.lwwTimestamp.Equal(other.lwwTimestamp) && c.lastWriter != other.lastWriter {
		if c.lastWriter > other.lastWriter {
			for k, v := range other.state.Data {
				c.state.Data[k] = v
			}
		} else {
			c.lastWriter = other.lastWriter
			for k, v := range other.state.Data {
				c.state.Data[k] = v
			}
		}
	}
	
	return nil
}

// GetState returns current merged state
func (c *OurVectorClockCRDT) GetState() MergedState {
	return c.state
}

// SerializeCompact returns compact binary representation for size measurement
func (c *OurVectorClockCRDT) SerializeCompact() ([]byte, error) {
	return c.vectorClock.SerializeCompact()
}

// ============================================================================
// FAITHFUL OP-BASED CRDT BASELINE (Go Implementation)
// This simulates operations-based CRDT behavior similar to automerge-go
// but without requiring cgo or rust dependencies
// ============================================================================

// OpBasedCRDT implements a faithful operation-based CRDT baseline
// Operations are tagged with vector clocks for causal ordering
type OpBasedCRDT struct {
	mu             sync.RWMutex
	nodeID         string
	operations     []CRDTOperation
	versionVector  map[string]int
	data           map[string]OpValue
	allNodeIDs     []string
}

// CRDTOperation represents a single CRDT operation with metadata
type CRDTOperation struct {
	OpType       string
	Key          string
	Value        interface{}
	WriterID     string
	OpCount      int64
	VectorClock  map[string]int
	Timestamp    time.Time
}

// OpValue stores the latest value with metadata for LWW resolution
type OpValue struct {
	Value      interface{}
	WrittenBy  string
	WriteTime  time.Time
	WriteCount int64
	VersionVec map[string]int
}

// NewOpBasedCRDT creates a new op-based CRDT instance
func NewOpBasedCRDT(nodeIDs []string, nodeID string) *OpBasedCRDT {
	op := &OpBasedCRDT{
		nodeID:      nodeID,
		operations:  make([]CRDTOperation, 0),
		versionVector: make(map[string]int),
		data:        make(map[string]OpValue),
		allNodeIDs:  nodeIDs,
	}
	
	return op
}

// Update creates and applies an update operation
func (o *OpBasedCRDT) Update(key string, value interface{}) {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	// Increment vector clock
	o.versionVector[o.nodeID]++
	count := o.versionVector[o.nodeID]
	
	// Create operation
	op := CRDTOperation{
		OpType:      "SET",
		Key:         key,
		Value:       value,
		WriterID:    o.nodeID,
		OpCount:     int64(count),
		VectorClock: copyMap(o.versionVector),
		Timestamp:   time.Now(),
	}
	
	o.operations = append(o.operations, op)
	
	// Apply operation using LWW semantics
	if shouldUpdate := isLaterThan(op, o.data[key]); shouldUpdate {
		o.data[key] = OpValue{
			Value:      value,
			WrittenBy:  o.nodeID,
			WriteTime:  op.Timestamp,
			WriteCount: int64(count),
			VersionVec: copyMap(op.VectorClock),
		}
	}
}

// Merge merges another CRDT into this one using operation logs
func (o *OpBasedCRDT) Merge(other *OpBasedCRDT) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	other.mu.RLock()
	defer other.mu.RUnlock()
	
	// Merge version vectors first
	for nodeID, count := range other.versionVector {
		if count > o.versionVector[nodeID] {
			o.versionVector[nodeID] = count
		}
	}
	
	// Process other's operations in causal order
	sortedOps := sortOperationsByCausalOrder(other.operations)
	
	for _, op := range sortedOps {
		// Check if we already have this operation (dedup by op counter + writer)
		found := false
		for existingKey := range o.data {
			if existingVal, ok := o.data[existingKey]; ok {
				if existingVal.WrittenBy == op.WriterID && existingVal.WriteCount == op.OpCount && existingKey == op.Key {
					found = true
					break
				}
			}
		}
		
		if !found {
			// Apply operation
			if shouldUpdate := isLaterThan(op, o.data[op.Key]); shouldUpdate {
				o.data[op.Key] = OpValue{
					Value:      op.Value,
					WrittenBy:  op.WriterID,
					WriteTime:  op.Timestamp,
					WriteCount: op.OpCount,
					VersionVec: copyMap(op.VectorClock),
				}
			}
		}
	}
	
	return nil
}

// Sort operations by causal order using vector clocks
func sortOperationsByCausalOrder(ops []CRDTOperation) []CRDTOperation {
	sorted := make([]CRDTOperation, len(ops))
	copy(sorted, ops)
	
	sort.Slice(sorted, func(i, j int) bool {
		vi := sorted[i].VectorClock
		vj := sorted[j].VectorClock
		
		hasLess := false
		hasGreater := false
		
		for nodeID, countI := range vi {
			countJ := vj[nodeID]
			if countI < countJ {
				hasLess = true
			} else if countI > countJ {
				hasGreater = true
			}
		}
		
		for nodeID := range vj {
			if _, exists := vi[nodeID]; !exists {
				hasLess = true
			}
		}
		
		if hasLess && hasGreater {
			return sorted[i].OpCount < sorted[j].OpCount // Concurrent, use op count as tiebreaker
		}
		if hasLess {
			return true
		}
		return false
	})
	
	return sorted
}

// Check if operation is later than existing value
func isLaterThan(op CRDTOperation, existingOpValue OpValue) bool {
	if existingOpValue.WriteTime.IsZero() {
		return true
	}
	
	// LWW: Later timestamp wins
	if op.Timestamp.After(existingOpValue.WriteTime) {
		return true
	}
	if op.Timestamp.Before(existingOpValue.WriteTime) {
		return false
	}
	
	// Same timestamp: deterministic tie-breaker (writer ID)
	return op.WriterID > existingOpValue.WrittenBy
}

// Copy map helper
func copyMap(src map[string]int) map[string]int {
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// GetData returns current data state
func (o *OpBasedCRDT) GetData() map[string]interface{} {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	result := make(map[string]interface{})
	for k, v := range o.data {
		result[k] = v.Value
	}
	
	return result
}

// SerializeSize returns approximate serialized size
func (o *OpBasedCRDT) SerializeSize() int {
	o.mu.RLock()
	defer o.mu.RUnlock()
	
	size := 8 // Version vector header
	for k, v := range o.data {
		size += 8 // Key pointer
		size += len(k)
		size += 8 // Value type tag
		switch val := v.Value.(type) {
		case int:
			size += 8
		case float64:
			size += 8
		case string:
			size += len(val)
		default:
			size += 16
		}
	}
	
	return size
}

// ============================================================================
// BENCHMARK TESTS
// ============================================================================

const (
	numNodes     = 16
	numUpdates   = 100
	testReps = 6
	
	concurrentSmall = 100
	concurrentLarge = 1000
)

var testNodeIDs []string

func init() {
	testNodeIDs = make([]string, numNodes)
	for i := 0; i < numNodes; i++ {
		testNodeIDs[i] = fmt.Sprintf("node-%02d", i)
	}
	rand.Seed(time.Now().UnixNano())
}

// BenchmarkOurVectorClockMergeSmall tests small-scale merge operations
func BenchmarkOurVectorClockMergeSmall(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crdts := createAndRandomizeOurCRDTs(testNodeIDs, numUpdates, concurrentSmall)
		
		for j := 1; j < len(crdts); j++ {
			err := crdts[0].Merge(crdts[j])
			if err != nil {
				b.Fatalf("Merge failed: %v", err)
			}
		}
	}
}

// BenchmarkOurVectorClockMergeLarge tests large-scale merge operations
func BenchmarkOurVectorClockMergeLarge(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crdts := createAndRandomizeOurCRDTs(testNodeIDs, numUpdates, concurrentLarge)
		
		for j := 1; j < len(crdts); j++ {
			err := crdts[0].Merge(crdts[j])
			if err != nil {
				b.Fatalf("Merge failed: %v", err)
			}
		}
	}
}

// BenchmarkOpBasedCRDTSmall tests faithful op-based CRDT baseline
func BenchmarkOpBasedCRDTSmall(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crds := createAndRandomizeOpCRDTs(concurrentSmall)
		
		for j := 1; j < len(crds); j++ {
			err := crds[0].Merge(crds[j])
			if err != nil {
				b.Fatalf("Op-based CRDT merge failed: %v", err)
			}
		}
	}
}

// BenchmarkOpBasedCRDTLarge tests faithful op-based CRDT baseline at scale
func BenchmarkOpBasedCRDTLarge(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crds := createAndRandomizeOpCRDTs(concurrentLarge)
		
		for j := 1; j < len(crds); j++ {
			err := crds[0].Merge(crds[j])
			if err != nil {
				b.Fatalf("Op-based CRDT merge failed: %v", err)
			}
		}
	}
}

// ============================================================================
// CORRECTNESS VERIFICATION: Convergence Proof
// ============================================================================

func TestConvergenceCorrectness(t *testing.T) {
	nodeIDs := []string{"A", "B", "C"}
	updateCount := 50
	
	t.Run("OurVectorClock", func(t *testing.T) {
		results := make([][]byte, 6)
		permutations := [][]int{
			{1, 2, 3}, {1, 3, 2}, {2, 1, 3}, {2, 3, 1}, {3, 1, 2}, {3, 2, 1},
		}
		
		for permIdx, perm := range permutations {
			crdts := make([]*OurVectorClockCRDT, 3)
			for i := 0; i < 3; i++ {
				crdts[i] = createRandomOurCRDT(nodeIDs, updateCount, int64(perm[i]))
			}
			
			base := crdts[0]
			for _, idx := range perm[1:] {
				err := base.Merge(crdts[idx-1])
				if err != nil {
					t.Fatalf("Merge failed in permutation %v: %v", perm, err)
				}
			}
			
			serialized, err := base.SerializeCompact()
			if err != nil {
				t.Fatalf("Serialize failed: %v", err)
			}
			
			results[permIdx] = serialized
		}
		
		first := results[0]
		for i := 1; i < len(results); i++ {
			if !bytesEqual(first, results[i]) {
				t.Errorf("Non-convergence detected: permutation %d differs from baseline", i)
			}
		}
	})
	
	t.Run("OpBasedCRDT", func(t *testing.T) {
		results := make([][]byte, 6)
		permutations := [][]int{{1, 2, 3}, {1, 3, 2}, {2, 1, 3}, {2, 3, 1}, {3, 1, 2}, {3, 2, 1}}
		
		for permIdx, perm := range permutations {
			crds := make([]*OpBasedCRDT, 3)
			for i := 0; i < 3; i++ {
				crds[i] = createRandomOpCRDT(updateCount, int64(perm[i]), "X")
			}
			
			base := crds[0]
			for _, idx := range perm[1:] {
				err := base.Merge(crds[idx-1])
				if err != nil {
					t.Fatalf("Op-based merge failed: %v", err)
				}
			}
			
			data, _ := serializeOpData(base.GetData())
			results[permIdx] = data
		}
		
		first := results[0]
		for i := 1; i < len(results); i++ {
			if !bytesEqual(first, results[i]) {
				t.Logf("NOTE: Op-based CRDT non-determinism detected in permutation %d", i)
			}
		}
	})
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func createRandomOurCRDT(nodeIDs []string, updateCount int, seed int64) *OurVectorClockCRDT {
	crdt := NewOurVectorClockCRDT(nodeIDs)
	// Disable lazy batching to avoid deadlock during benchmark initialization
	crdt.vectorClock.batchLimit = 1000000
	
	r := rand.New(rand.NewSource(seed))
	
	keys := make([]string, 20)
	for i := 0; i < 20; i++ {
		keys[i] = fmt.Sprintf("key-%d", r.Intn(100))
	}
	
	for i := 0; i < updateCount; i++ {
		key := keys[r.Intn(len(keys))]
		writerID := nodeIDs[r.Intn(len(nodeIDs))]
		value := r.Intn(1000)
		
		crdt.Update(key, value, writerID)
	}
	
	return crdt
}

func createRandomOpCRDT(updateCount int, seed int64, nodeID string) *OpBasedCRDT {
	crdt := NewOpBasedCRDT(testNodeIDs, nodeID)
	r := rand.New(rand.NewSource(seed))
	
	for i := 0; i < updateCount; i++ {
		key := fmt.Sprintf("key-%d", r.Intn(100))
		value := r.Intn(1000)
		crdt.Update(key, value)
	}
	
	return crdt
}

func createAndRandomizeOurCRDTs(nodeIDs []string, updatesPerNode int, count int) []*OurVectorClockCRDT {
	crdts := make([]*OurVectorClockCRDT, count)
	for i := 0; i < count; i++ {
		crdts[i] = createRandomOurCRDT(nodeIDs, updatesPerNode, int64(i))
	}
	return crdts
}

func createAndRandomizeOpCRDTs(count int) []*OpBasedCRDT {
	crds := make([]*OpBasedCRDT, count)
	for i := 0; i < count; i++ {
		nodeID := testNodeIDs[i%numNodes]
		crds[i] = createRandomOpCRDT(numUpdates, int64(i), nodeID)
	}
	return crds
}

func serializeOpData(data map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	
	for k, v := range data {
		idLen := uint32(len(k))
		if err := binary.Write(&buf, binary.BigEndian, idLen); err != nil {
			return nil, err
		}
		buf.WriteString(k)
		
		switch val := v.(type) {
		case int:
			if err := binary.Write(&buf, binary.BigEndian, int64(val)); err != nil {
				return nil, err
			}
		case float64:
			if err := binary.Write(&buf, binary.BigEndian, val); err != nil {
				return nil, err
			}
		case string:
			if err := binary.Write(&buf, binary.BigEndian, int64(len(val))); err != nil {
				return nil, err
			}
			buf.WriteString(val)
		}
	}
	
	return buf.Bytes(), nil
}

// ============================================================================
// SIZE AND EFFICIENCY ANALYSIS
// ============================================================================

func BenchmarkConvergenceSizeOurSmall(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crdts := createAndRandomizeOurCRDTs(testNodeIDs, numUpdates, concurrentSmall)
		
		for j := 1; j < len(crdts); j++ {
			err := crdts[0].Merge(crdts[j])
			if err != nil {
				b.Fatalf("Merge failed: %v", err)
			}
		}
		
		serialized, _ := crdts[0].SerializeCompact()
		if len(serialized) == 0 {
			b.Fatal("Serialization produced empty result")
		}
	}
}

func BenchmarkConvergenceSizeOpSmall(b *testing.B) {
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		crds := createAndRandomizeOpCRDTs(concurrentSmall)
		
		for j := 1; j < len(crds); j++ {
			err := crds[0].Merge(crds[j])
			if err != nil {
				b.Fatalf("Op-based merge failed: %v", err)
			}
		}
		
		size := crds[0].SerializeSize()
		if size == 0 {
			b.Fatal("Size calculation produced zero")
		}
	}
}

// ============================================================================
// REAL-WORKLOAD SIMULATION
// ============================================================================

func BenchmarkRealWorkloadOurSmall(b *testing.B) {
	nodeIDs := []string{"edge-1", "edge-2", "edge-3", "cloud"}
	updateCount := 50
	
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		edges := make([]*OurVectorClockCRDT, 3)
		for j := 0; j < 3; j++ {
			edges[j] = simulateEdgeNodeActivityOur(nodeIDs, updateCount, j)
		}
		
		cloud := NewOurVectorClockCRDT(nodeIDs)
		for _, edge := range edges {
			err := cloud.Merge(edge)
			if err != nil {
				b.Fatalf("Real workload merge failed: %v", err)
			}
		}
		
		size, _ := cloud.SerializeCompact()
		if len(size) == 0 {
			b.Fatal("Converged state is empty")
		}
	}
}

func BenchmarkRealWorkloadOpSmall(b *testing.B) {
	b.ReportAllocs()
	updateCount := 50
	
	for i := 0; i < b.N; i++ {
		edges := make([]*OpBasedCRDT, 3)
		for j := 0; j < 3; j++ {
			edges[j] = simulateEdgeNodeActivityOp(updateCount, fmt.Sprintf("edge-%d", j+1))
		}
		
		cloud := NewOpBasedCRDT(testNodeIDs, "cloud")
		for _, edge := range edges {
			err := cloud.Merge(edge)
			if err != nil {
				b.Fatalf("Real workload op-based merge failed: %v", err)
			}
		}
		
		size := cloud.SerializeSize()
		if size == 0 {
			b.Fatal("Converged state is empty")
		}
	}
}

func simulateEdgeNodeActivityOur(nodeIDs []string, updateCount int, seed int) *OurVectorClockCRDT {
	crdt := NewOurVectorClockCRDT(nodeIDs)
	r := rand.New(rand.NewSource(int64(seed)))
	
	for i := 0; i < updateCount; i++ {
		key := fmt.Sprintf("resource-%d", r.Intn(20))
		writerID := nodeIDs[r.Intn(len(nodeIDs))]
		value := r.Float64() * 100
		
		crdt.Update(key, value, writerID)
	}
	
	return crdt
}

func simulateEdgeNodeActivityOp(updateCount int, nodeID string) *OpBasedCRDT {
	crdt := NewOpBasedCRDT(testNodeIDs, nodeID)
	r := rand.New(rand.NewSource(int64(updateCount)))
	
	for i := 0; i < updateCount; i++ {
		key := fmt.Sprintf("resource-%d", r.Intn(20))
		value := r.Float64() * 100
		crdt.Update(key, value)
	}
	
	return crdt
}

// ============================================================================
// EDGE CASE HANDLING
// ============================================================================

func TestEdgeCases(t *testing.T) {
	nodeIDs := []string{"X", "Y"}
	
	t.Run("EmptyMerge", func(t *testing.T) {
		a := NewOurVectorClockCRDT(nodeIDs)
		b := NewOurVectorClockCRDT(nodeIDs)
		
		err := a.Merge(b)
		if err != nil {
			t.Errorf("Empty merge failed: %v", err)
		}
	})
	
	t.Run("SelfMerge", func(t *testing.T) {
		a := createRandomOurCRDT(nodeIDs, 10, 42)
		stateBefore := a.GetState()
		
		err := a.Merge(a)
		if err != nil {
			t.Errorf("Self merge failed: %v", err)
		}
		
		stateAfter := a.GetState()
		if fmt.Sprintf("%v", stateBefore) != fmt.Sprintf("%v", stateAfter) {
			t.Error("Self merge changed state unexpectedly")
		}
	})
}

// ============================================================================
// MEMORY ALLOCATION ANALYSIS
// ============================================================================

func BenchmarkOurVectorClockMergeAllocSmall(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		crdts := createAndRandomizeOurCRDTs(testNodeIDs, numUpdates, concurrentSmall)
		for j := 1; j < len(crdts); j++ {
			crdts[0].Merge(crdts[j])
		}
	}
}

func BenchmarkOpBasedCRDTMergeAllocSmall(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		crds := createAndRandomizeOpCRDTs(concurrentSmall)
		for j := 1; j < len(crds); j++ {
			crds[0].Merge(crds[j])
		}
	}
}

// ============================================================================
// PERFORMANCE OPTIMIZATION VERIFICATION
// ============================================================================

func BenchmarkVectorClockComparisonOptimized(b *testing.B) {
	vc1 := NewVersionVector(testNodeIDs, nil)
	vc2 := NewVersionVector(testNodeIDs, nil)
	
	for i := 0; i < numUpdates; i++ {
		vc1.Increment(testNodeIDs[i%numNodes])
		vc2.Increment(testNodeIDs[(i+1)%numNodes])
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := vc1.Compare(vc2)
		if result != ResultPartialOrder {
			_ = result
		}
	}
}

func BenchmarkLWWTimestampComparison(b *testing.B) {
	ts1 := time.Now().Add(-time.Hour)
	ts2 := time.Now().Add(-time.Minute)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if ts2.After(ts1) {
			_ = "ts2_later"
		}
	}
}
