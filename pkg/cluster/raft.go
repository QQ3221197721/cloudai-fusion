package cluster

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Raft Consensus Algorithm Implementation
// ============================================================================
//
// This package implements the Raft consensus algorithm for CloudAI Fusion's
// cluster state replication. Raft ensures that multiple instances of the
// control plane maintain a consistent view of the cluster state.
//
// Key Raft concepts:
//   - Leader Election: at most one leader per term; followers become
//     candidates after election timeout
//   - Log Replication: the leader replicates log entries to followers;
//     a majority acknowledgment makes the entry "committed"
//   - Safety: committed entries are never lost; only nodes with
//     up-to-date logs can become leader
//
// Reference: "In Search of an Understandable Consensus Algorithm"
// (Diego Ongaro, John Ousterhout — USENIX ATC 2014)

// ============================================================================
// Raft Node State
// ============================================================================

// RaftRole represents the role of a Raft node.
type RaftRole int

const (
	RaftFollower RaftRole = iota
	RaftCandidate
	RaftLeader
)

func (r RaftRole) String() string {
	switch r {
	case RaftFollower:
		return "Follower"
	case RaftCandidate:
		return "Candidate"
	case RaftLeader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// ============================================================================
// Log Entry
// ============================================================================

// LogEntry represents a single entry in the Raft log.
type LogEntry struct {
	// Index is the position in the log (1-based).
	Index uint64 `json:"index"`

	// Term is the leader's term when the entry was created.
	Term uint64 `json:"term"`

	// Command is the replicated state machine command.
	Command []byte `json:"command"`

	// CommandType identifies the type of command for deserialization.
	CommandType string `json:"command_type"`

	// Timestamp when the entry was appended.
	Timestamp time.Time `json:"timestamp"`
}

// ============================================================================
// Raft Configuration
// ============================================================================

// RaftConfig configures the Raft consensus module.
type RaftConfig struct {
	// NodeID is the unique identifier for this node.
	NodeID string

	// Peers is the list of peer node addresses (excluding self).
	Peers []RaftPeer

	// ElectionTimeoutMin is the minimum election timeout. Default: 150ms
	ElectionTimeoutMin time.Duration

	// ElectionTimeoutMax is the maximum election timeout. Default: 300ms
	ElectionTimeoutMax time.Duration

	// HeartbeatInterval is how often the leader sends heartbeats. Default: 50ms
	HeartbeatInterval time.Duration

	// MaxLogEntries is the maximum number of log entries to keep in memory.
	// Older entries are snapshotted. Default: 10000
	MaxLogEntries int

	// SnapshotThreshold triggers a snapshot after this many committed entries.
	// Default: 5000
	SnapshotThreshold int

	// Logger for structured logging.
	Logger *logrus.Logger

	// Apply is the callback to apply a committed log entry to the state machine.
	Apply func(entry *LogEntry) error
}

// RaftPeer represents a peer node in the Raft cluster.
type RaftPeer struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// ============================================================================
// Raft Node
// ============================================================================

// RaftNode implements the Raft consensus algorithm.
type RaftNode struct {
	config RaftConfig
	logger *logrus.Logger

	// Persistent state (on all servers)
	currentTerm atomic.Uint64
	votedFor    string // candidateID that received vote in current term
	log         []LogEntry

	// Volatile state (on all servers)
	commitIndex uint64 // highest log entry known to be committed
	lastApplied uint64 // highest log entry applied to state machine
	role        RaftRole

	// Volatile state (on leaders)
	nextIndex  map[string]uint64 // for each peer: index of next log entry to send
	matchIndex map[string]uint64 // for each peer: index of highest log entry known to be replicated

	// Peer communication (in production, use gRPC)
	peers map[string]*raftPeerState

	// Timers
	electionTimer  *time.Timer
	heartbeatTimer *time.Timer

	// Apply channel
	applyCh chan *LogEntry

	// Stats
	stats RaftStats

	cancel context.CancelFunc
	mu     sync.RWMutex
}

type raftPeerState struct {
	peer      RaftPeer
	nextIndex uint64
	matchIdx  uint64
	lastSeen  time.Time
}

// NewRaftNode creates a new Raft consensus node.
func NewRaftNode(cfg RaftConfig) *RaftNode {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.NodeID == "" {
		cfg.NodeID = generateRaftID()
	}
	if cfg.ElectionTimeoutMin <= 0 {
		cfg.ElectionTimeoutMin = 150 * time.Millisecond
	}
	if cfg.ElectionTimeoutMax <= 0 {
		cfg.ElectionTimeoutMax = 300 * time.Millisecond
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 50 * time.Millisecond
	}
	if cfg.MaxLogEntries <= 0 {
		cfg.MaxLogEntries = 10000
	}
	if cfg.SnapshotThreshold <= 0 {
		cfg.SnapshotThreshold = 5000
	}

	node := &RaftNode{
		config:     cfg,
		logger:     cfg.Logger,
		role:       RaftFollower,
		log:        make([]LogEntry, 0, 256),
		nextIndex:  make(map[string]uint64),
		matchIndex: make(map[string]uint64),
		peers:      make(map[string]*raftPeerState),
		applyCh:    make(chan *LogEntry, 256),
	}

	// Initialize peer state
	for _, peer := range cfg.Peers {
		node.peers[peer.ID] = &raftPeerState{
			peer:      peer,
			nextIndex: 1,
		}
	}

	return node
}

// Start begins the Raft consensus loop.
func (n *RaftNode) Start(ctx context.Context) error {
	ctx, n.cancel = context.WithCancel(ctx)

	n.logger.WithFields(logrus.Fields{
		"node_id":  n.config.NodeID,
		"peers":    len(n.config.Peers),
		"role":     n.role.String(),
	}).Info("Starting Raft consensus node")

	// Start as follower with randomized election timeout
	n.resetElectionTimer()

	// Start the main loop
	go n.run(ctx)

	// Start the apply loop
	go n.applyLoop(ctx)

	return nil
}

// Stop gracefully stops the Raft node.
func (n *RaftNode) Stop() {
	if n.cancel != nil {
		n.cancel()
	}
}

// ============================================================================
// Main Raft Loop
// ============================================================================

func (n *RaftNode) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			n.logger.Info("Raft node shutting down")
			return
		default:
		}

		n.mu.RLock()
		role := n.role
		n.mu.RUnlock()

		switch role {
		case RaftFollower:
			n.runFollower(ctx)
		case RaftCandidate:
			n.runCandidate(ctx)
		case RaftLeader:
			n.runLeader(ctx)
		}
	}
}

func (n *RaftNode) runFollower(ctx context.Context) {
	n.logger.Debug("Running as Follower")

	if n.electionTimer == nil {
		n.resetElectionTimer()
	}

	select {
	case <-ctx.Done():
		return
	case <-n.electionTimer.C:
		// Election timeout expired — become candidate
		n.mu.Lock()
		n.logger.Info("Election timeout — transitioning to Candidate")
		n.role = RaftCandidate
		n.stats.ElectionTimeouts++
		n.mu.Unlock()
	}
}

func (n *RaftNode) runCandidate(ctx context.Context) {
	n.mu.Lock()
	// Increment term and vote for self
	n.currentTerm.Add(1)
	n.votedFor = n.config.NodeID
	currentTerm := n.currentTerm.Load()
	n.stats.ElectionsStarted++
	n.mu.Unlock()

	n.logger.WithFields(logrus.Fields{
		"term": currentTerm,
	}).Info("Starting election")

	// Request votes from all peers (simulated)
	votesReceived := 1 // self-vote
	totalVoters := len(n.config.Peers) + 1
	majority := totalVoters/2 + 1

	// In production, send RequestVote RPCs to all peers
	// For simulation, assume we win if we have majority of peers configured
	for _, peer := range n.config.Peers {
		// Simulate vote response (in production: gRPC RequestVote RPC)
		n.logger.WithFields(logrus.Fields{
			"peer": peer.ID,
			"term": currentTerm,
		}).Debug("Requesting vote")

		// For single-node or development: auto-grant votes
		if len(n.config.Peers) == 0 {
			votesReceived++
		}
	}

	if votesReceived >= majority {
		n.mu.Lock()
		n.role = RaftLeader
		n.stats.ElectionsWon++
		n.stats.LastLeaderElection = time.Now()

		// Initialize leader state
		lastLogIndex := n.lastLogIndex()
		for peerID := range n.peers {
			n.nextIndex[peerID] = lastLogIndex + 1
			n.matchIndex[peerID] = 0
		}
		n.mu.Unlock()

		n.logger.WithField("term", currentTerm).Info("Won election — becoming Leader")
	} else {
		// Lost election — back to follower
		n.mu.Lock()
		n.role = RaftFollower
		n.mu.Unlock()
		n.resetElectionTimer()
	}
}

func (n *RaftNode) runLeader(ctx context.Context) {
	// Send heartbeats
	n.sendHeartbeats()

	// Advance commit index
	n.advanceCommitIndex()

	select {
	case <-ctx.Done():
		return
	case <-time.After(n.config.HeartbeatInterval):
	}
}

// ============================================================================
// Log Operations
// ============================================================================

// Propose proposes a new command to be replicated via Raft consensus.
// Only the leader can accept proposals. Returns the log index if accepted.
func (n *RaftNode) Propose(ctx context.Context, commandType string, command interface{}) (uint64, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.role != RaftLeader {
		return 0, fmt.Errorf("not the leader (current role: %s)", n.role.String())
	}

	data, err := json.Marshal(command)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal command: %w", err)
	}

	entry := LogEntry{
		Index:       n.lastLogIndex() + 1,
		Term:        n.currentTerm.Load(),
		Command:     data,
		CommandType: commandType,
		Timestamp:   time.Now(),
	}

	n.log = append(n.log, entry)
	n.stats.LogEntriesAppended++

	n.logger.WithFields(logrus.Fields{
		"index":        entry.Index,
		"term":         entry.Term,
		"command_type": commandType,
	}).Debug("Log entry appended")

	// In production: replicate to followers via AppendEntries RPC
	// For single-node: immediately commit
	if len(n.config.Peers) == 0 {
		n.commitIndex = entry.Index
		n.applyCh <- &entry
	}

	return entry.Index, nil
}

func (n *RaftNode) lastLogIndex() uint64 {
	if len(n.log) == 0 {
		return 0
	}
	return n.log[len(n.log)-1].Index
}

func (n *RaftNode) lastLogTerm() uint64 {
	if len(n.log) == 0 {
		return 0
	}
	return n.log[len(n.log)-1].Term
}

// ============================================================================
// Heartbeats & Replication
// ============================================================================

func (n *RaftNode) sendHeartbeats() {
	n.mu.RLock()
	defer n.mu.RUnlock()

	for peerID, peerState := range n.peers {
		// In production, send AppendEntries RPC:
		// - If leader's log is ahead of peer's nextIndex, include entries
		// - Otherwise, send empty heartbeat
		n.logger.WithFields(logrus.Fields{
			"peer":       peerID,
			"next_index": peerState.nextIndex,
		}).Trace("Sending heartbeat/AppendEntries")
	}

	n.stats.HeartbeatsSent++
}

func (n *RaftNode) advanceCommitIndex() {
	n.mu.Lock()
	defer n.mu.Unlock()

	// For single-node mode: commit all entries
	if len(n.config.Peers) == 0 {
		if n.lastLogIndex() > n.commitIndex {
			oldCommit := n.commitIndex
			n.commitIndex = n.lastLogIndex()
			// Apply all newly committed entries
			for i := oldCommit + 1; i <= n.commitIndex; i++ {
				if int(i) <= len(n.log) {
					entry := n.log[i-1]
					n.applyCh <- &entry
				}
			}
		}
		return
	}

	// For multi-node: find the highest index replicated on a majority
	for idx := n.commitIndex + 1; idx <= n.lastLogIndex(); idx++ {
		if idx == 0 || int(idx) > len(n.log) {
			break
		}
		// Only commit entries from the current term
		if n.log[idx-1].Term != n.currentTerm.Load() {
			continue
		}

		replicatedCount := 1 // self
		for _, peerState := range n.peers {
			if peerState.matchIdx >= idx {
				replicatedCount++
			}
		}

		majority := (len(n.config.Peers)+1)/2 + 1
		if replicatedCount >= majority {
			n.commitIndex = idx
			n.applyCh <- &n.log[idx-1]
			n.stats.EntriesCommitted++
		}
	}
}

// ============================================================================
// Apply Loop — applies committed entries to state machine
// ============================================================================

func (n *RaftNode) applyLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case entry := <-n.applyCh:
			if entry == nil {
				continue
			}

			n.mu.Lock()
			if entry.Index <= n.lastApplied {
				n.mu.Unlock()
				continue
			}
			n.lastApplied = entry.Index
			n.mu.Unlock()

			if n.config.Apply != nil {
				if err := n.config.Apply(entry); err != nil {
					n.logger.WithError(err).WithField("index", entry.Index).Error("Failed to apply log entry")
				} else {
					n.logger.WithFields(logrus.Fields{
						"index":        entry.Index,
						"command_type": entry.CommandType,
					}).Debug("Log entry applied to state machine")
				}
			}
		}
	}
}

// ============================================================================
// RequestVote RPC (simulated)
// ============================================================================

// RequestVoteArgs contains the arguments for a RequestVote RPC.
type RequestVoteArgs struct {
	Term         uint64 `json:"term"`
	CandidateID  string `json:"candidate_id"`
	LastLogIndex uint64 `json:"last_log_index"`
	LastLogTerm  uint64 `json:"last_log_term"`
}

// RequestVoteReply contains the response for a RequestVote RPC.
type RequestVoteReply struct {
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
}

// HandleRequestVote processes a RequestVote RPC from a candidate.
func (n *RaftNode) HandleRequestVote(args RequestVoteArgs) RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	currentTerm := n.currentTerm.Load()

	reply := RequestVoteReply{
		Term:        currentTerm,
		VoteGranted: false,
	}

	// Rule 1: Reply false if term < currentTerm
	if args.Term < currentTerm {
		return reply
	}

	// If RPC term > currentTerm, update term and become follower
	if args.Term > currentTerm {
		n.currentTerm.Store(args.Term)
		n.role = RaftFollower
		n.votedFor = ""
		reply.Term = args.Term
	}

	// Rule 2: Grant vote if votedFor is null or candidateID,
	// and candidate's log is at least as up-to-date as receiver's log
	if (n.votedFor == "" || n.votedFor == args.CandidateID) &&
		n.isLogUpToDate(args.LastLogIndex, args.LastLogTerm) {
		n.votedFor = args.CandidateID
		reply.VoteGranted = true
		n.resetElectionTimer()
	}

	return reply
}

func (n *RaftNode) isLogUpToDate(lastLogIndex, lastLogTerm uint64) bool {
	myLastTerm := n.lastLogTerm()
	myLastIndex := n.lastLogIndex()

	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}
	return lastLogIndex >= myLastIndex
}

// ============================================================================
// AppendEntries RPC (simulated)
// ============================================================================

// AppendEntriesArgs contains the arguments for an AppendEntries RPC.
type AppendEntriesArgs struct {
	Term         uint64     `json:"term"`
	LeaderID     string     `json:"leader_id"`
	PrevLogIndex uint64     `json:"prev_log_index"`
	PrevLogTerm  uint64     `json:"prev_log_term"`
	Entries      []LogEntry `json:"entries"`
	LeaderCommit uint64     `json:"leader_commit"`
}

// AppendEntriesReply contains the response for an AppendEntries RPC.
type AppendEntriesReply struct {
	Term    uint64 `json:"term"`
	Success bool   `json:"success"`
}

// HandleAppendEntries processes an AppendEntries RPC from the leader.
func (n *RaftNode) HandleAppendEntries(args AppendEntriesArgs) AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	currentTerm := n.currentTerm.Load()

	reply := AppendEntriesReply{
		Term:    currentTerm,
		Success: false,
	}

	// Rule 1: Reply false if term < currentTerm
	if args.Term < currentTerm {
		return reply
	}

	// Reset election timer (leader is alive)
	n.resetElectionTimer()

	// If RPC term > currentTerm, update term
	if args.Term > currentTerm {
		n.currentTerm.Store(args.Term)
		n.role = RaftFollower
		n.votedFor = ""
	}

	// If already a candidate, step down to follower
	if n.role == RaftCandidate {
		n.role = RaftFollower
	}

	// Rule 2: Reply false if log doesn't contain an entry at prevLogIndex
	// whose term matches prevLogTerm
	if args.PrevLogIndex > 0 {
		if args.PrevLogIndex > uint64(len(n.log)) {
			return reply
		}
		if n.log[args.PrevLogIndex-1].Term != args.PrevLogTerm {
			return reply
		}
	}

	// Rule 3: If an existing entry conflicts with a new one, delete existing
	// entry and all that follow it
	// Rule 4: Append any new entries not already in the log
	if len(args.Entries) > 0 {
		insertIdx := args.PrevLogIndex + 1
		for i, entry := range args.Entries {
			logIdx := insertIdx + uint64(i)
			if logIdx <= uint64(len(n.log)) {
				if n.log[logIdx-1].Term != entry.Term {
					n.log = n.log[:logIdx-1]
					n.log = append(n.log, args.Entries[i:]...)
					break
				}
			} else {
				n.log = append(n.log, args.Entries[i:]...)
				break
			}
		}
	}

	// Rule 5: If leaderCommit > commitIndex, update commitIndex
	if args.LeaderCommit > n.commitIndex {
		lastLogIdx := n.lastLogIndex()
		if args.LeaderCommit < lastLogIdx {
			n.commitIndex = args.LeaderCommit
		} else {
			n.commitIndex = lastLogIdx
		}
	}

	reply.Success = true
	n.stats.AppendEntriesReceived++
	return reply
}

// ============================================================================
// Timer Management
// ============================================================================

func (n *RaftNode) resetElectionTimer() {
	timeout := n.randomElectionTimeout()
	if n.electionTimer != nil {
		n.electionTimer.Reset(timeout)
	} else {
		n.electionTimer = time.NewTimer(timeout)
	}
}

func (n *RaftNode) randomElectionTimeout() time.Duration {
	min := n.config.ElectionTimeoutMin.Milliseconds()
	max := n.config.ElectionTimeoutMax.Milliseconds()
	diff := max - min
	if diff <= 0 {
		return n.config.ElectionTimeoutMin
	}
	nBig, err := rand.Int(rand.Reader, big.NewInt(diff))
	if err != nil {
		return n.config.ElectionTimeoutMin
	}
	return time.Duration(min+nBig.Int64()) * time.Millisecond
}

// ============================================================================
// Status & Stats
// ============================================================================

// RaftStats holds runtime statistics for the Raft node.
type RaftStats struct {
	ElectionTimeouts       int64     `json:"election_timeouts"`
	ElectionsStarted       int64     `json:"elections_started"`
	ElectionsWon           int64     `json:"elections_won"`
	HeartbeatsSent         int64     `json:"heartbeats_sent"`
	AppendEntriesReceived  int64     `json:"append_entries_received"`
	LogEntriesAppended     int64     `json:"log_entries_appended"`
	EntriesCommitted       int64     `json:"entries_committed"`
	LastLeaderElection     time.Time `json:"last_leader_election,omitempty"`
}

// RaftStatus describes the current state of the Raft node.
type RaftStatus struct {
	NodeID      string    `json:"node_id"`
	Role        string    `json:"role"`
	CurrentTerm uint64    `json:"current_term"`
	CommitIndex uint64    `json:"commit_index"`
	LastApplied uint64    `json:"last_applied"`
	LogLength   int       `json:"log_length"`
	PeerCount   int       `json:"peer_count"`
	VotedFor    string    `json:"voted_for"`
	Stats       RaftStats `json:"stats"`
}

// Status returns the current status of the Raft node.
func (n *RaftNode) Status() RaftStatus {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return RaftStatus{
		NodeID:      n.config.NodeID,
		Role:        n.role.String(),
		CurrentTerm: n.currentTerm.Load(),
		CommitIndex: n.commitIndex,
		LastApplied: n.lastApplied,
		LogLength:   len(n.log),
		PeerCount:   len(n.config.Peers),
		VotedFor:    n.votedFor,
		Stats:       n.stats,
	}
}

// IsLeader returns true if this node is the current Raft leader.
func (n *RaftNode) IsLeader() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.role == RaftLeader
}

func generateRaftID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("raft-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("raft-%s", hex.EncodeToString(b))
}
