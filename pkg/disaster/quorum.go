package disaster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrQuorumNotAchieved = errors.New("quorum not achieved - possible minority partition")

// NodeInfo describes a node in the distributed system
type NodeInfo struct {
	ID           string
	Address      string
	IsPrimary    bool
	LastSeen     time.Time
}

// SplitBrainResult contains split-brain detection outcome
type SplitBrainResult struct {
	InMinority   bool
	Reachable    []NodeInfo
	TotalNodes   int
	Threshold    int
	DetectionAt  time.Time
	Message      string
}

// QuorumDetector detects split-brain scenarios using quorum voting
type QuorumDetector struct {
	nodes       []NodeInfo
	mu          sync.RWMutex
	quorumSize  int
	pingTimeout time.Duration
	logger      func(format string, args ...interface{})
}

// NewQuorumDetector creates a new quorum detector
func NewQuorumDetector(nodes []NodeInfo) *QuorumDetector {
	n := len(nodes)
	if n < 1 {
		n = 1
	}
	
	quorumSize := (n / 2) + 1
	
	return &QuorumDetector{
		nodes:       nodes,
		quorumSize:  quorumSize,
		pingTimeout: 2 * time.Second,
		logger:      func(format string, args ...interface{}) {},
	}
}

// SetLogger sets custom logging function
func (qd *QuorumDetector) SetLogger(log func(format string, args ...interface{})) {
	qd.logger = log
}

// DetectSplitBrain pings all nodes and determines if we're in a minority partition
func (qd *QuorumDetector) DetectSplitBrain(ctx context.Context) (*SplitBrainResult, error) {
	qd.mu.RLock()
	allNodes := make([]NodeInfo, len(qd.nodes))
	copy(allNodes, qd.nodes)
	qd.mu.RUnlock()

	var reachable []NodeInfo
	ctx, cancel := context.WithTimeout(ctx, time.Duration(len(allNodes))*qd.pingTimeout)
	defer cancel()

	for _, node := range allNodes {
		select {
		case <-ctx.Done():
			// Timeout reached
			goto checkQuorum
		default:
			// Ping simulation - in real implementation, would send RPC
			if qd.pingNode(ctx, node) {
				reachable = append(reachable, node)
			}
		}
	}

checkQuorum:
	// Check if we have quorum
	if len(reachable) < qd.quorumSize {
		qd.logger("SPLIT-BRAIN DETECTED: Only %d/%d nodes reachable (need %d)", 
			len(reachable), len(allNodes), qd.quorumSize)
		
		return &SplitBrainResult{
			InMinority: true,
			Reachable:  reachable,
			TotalNodes: len(allNodes),
			Threshold:  qd.quorumSize,
			DetectionAt: time.Now().UTC(),
			Message: fmt.Sprintf("Possible network partition detected. Reachable nodes (%d) < quorum (%d). Consider stepping down.", 
				len(reachable), qd.quorumSize),
		}, ErrQuorumNotAchieved
	}

	return &SplitBrainResult{
		InMinority: false,
		Reachable:  reachable,
		TotalNodes: len(allNodes),
		Threshold:  qd.quorumSize,
		DetectionAt: time.Now().UTC(),
		Message: fmt.Sprintf("Quorum confirmed: %d/%d nodes reachable", len(reachable), len(allNodes)),
	}, nil
}

// pingNode simulates a node ping (in production, this would use gRPC/HTTP health check)
func (qd *QuorumDetector) pingNode(ctx context.Context, node NodeInfo) bool {
	// Simulate network latency between 5-200ms
	time.Sleep(time.Millisecond * time.Duration(5+(int(node.LastSeen.UnixNano())%195)))
	
	// Simulate success rate of ~95%
	success := (node.LastSeen.UnixNano()%20) != 0
	
	return success
}

// Vote performs a voting round to determine if we should remain leader
func (qd *QuorumDetector) Vote(ctx context.Context) (isMajority bool, err error) {
	result, err := qd.DetectSplitBrain(ctx)
	if err != nil {
		return false, err
	}
	
	isMajority = !result.InMinority
	
	if isMajority {
		qd.logger("VOTE PASSED: We hold majority with %d votes", len(result.Reachable))
	} else {
		qd.logger("VOTE FAILED: Minority partition at %d votes, need %d", 
			len(result.Reachable), qd.quorumSize)
	}
	
	return isMajority, nil
}

// AddNode adds a node to the cluster membership
func (qd *QuorumDetector) AddNode(node NodeInfo) {
	qd.mu.Lock()
	defer qd.mu.Unlock()
	
	for i, existing := range qd.nodes {
		if existing.ID == node.ID {
			qd.nodes[i] = node
			return
		}
	}
	
	qd.nodes = append(qd.nodes, node)
	
	// Recalculate quorum size
	if len(qd.nodes) > 0 {
		qd.quorumSize = (len(qd.nodes)/2) + 1
	}
}

// RemoveNode removes a node from the cluster
func (qd *QuorumDetector) RemoveNode(nodeID string) {
	qd.mu.Lock()
	defer qd.mu.Unlock()
	
	newNodes := make([]NodeInfo, 0, len(qd.nodes)-1)
	for _, node := range qd.nodes {
		if node.ID != nodeID {
			newNodes = append(newNodes, node)
		}
	}
	
	qd.nodes = newNodes
	if len(qd.nodes) > 0 {
		qd.quorumSize = (len(qd.nodes)/2) + 1
	}
}

// GetClusterStatus returns current cluster status
func (qd *QuorumDetector) GetClusterStatus() (total, quorumSize int, nodes []NodeInfo) {
	qd.mu.RLock()
	defer qd.mu.RUnlock()
	
	return len(qd.nodes), qd.quorumSize, qd.nodes
}
