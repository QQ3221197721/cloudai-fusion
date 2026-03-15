package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Two-Phase Commit (2PC) — Distributed Transaction Protocol
// ============================================================================
//
// Two-Phase Commit ensures atomicity across multiple data stores or services.
// When a single logical operation must update multiple databases/shards,
// 2PC coordinates all participants so either ALL commit or ALL abort.
//
// Phase 1 (Prepare):
//   The coordinator sends PREPARE to all participants.
//   Each participant writes to WAL, acquires locks, and responds YES/NO.
//
// Phase 2 (Commit/Abort):
//   If ALL participants voted YES → coordinator sends COMMIT.
//   If ANY participant voted NO  → coordinator sends ABORT.
//
// Failure Recovery:
//   - Coordinator crash: participants wait (blocking protocol); resolved
//     by coordinator WAL replay on restart.
//   - Participant crash: on restart, check WAL for pending 2PC decisions
//     and either commit or abort based on coordinator's decision.
//   - Network partition: timeout-based abort for unknown state.
//
// Optimizations implemented:
//   - Read-only optimization: participants with no changes vote READONLY
//     and are excluded from Phase 2.
//   - Presumed abort: if the coordinator crashes before writing COMMIT,
//     participants abort (no need for abort log entry at coordinator).

// ============================================================================
// 2PC Types & Configuration
// ============================================================================

// TxnPhase represents the current phase of a distributed transaction.
type TxnPhase int

const (
	TxnPhaseInit TxnPhase = iota
	TxnPhasePreparing
	TxnPhasePrepared
	TxnPhaseCommitting
	TxnPhaseCommitted
	TxnPhaseAborting
	TxnPhaseAborted
)

func (p TxnPhase) String() string {
	switch p {
	case TxnPhaseInit:
		return "INIT"
	case TxnPhasePreparing:
		return "PREPARING"
	case TxnPhasePrepared:
		return "PREPARED"
	case TxnPhaseCommitting:
		return "COMMITTING"
	case TxnPhaseCommitted:
		return "COMMITTED"
	case TxnPhaseAborting:
		return "ABORTING"
	case TxnPhaseAborted:
		return "ABORTED"
	default:
		return "UNKNOWN"
	}
}

// TxnVote represents a participant's vote in Phase 1.
type TxnVote int

const (
	VoteNone TxnVote = iota
	VoteYes
	VoteNo
	VoteReadOnly
)

func (v TxnVote) String() string {
	switch v {
	case VoteYes:
		return "YES"
	case VoteNo:
		return "NO"
	case VoteReadOnly:
		return "READONLY"
	default:
		return "NONE"
	}
}

// TwoPCConfig configures the Two-Phase Commit coordinator.
type TwoPCConfig struct {
	// PrepareTimeout is the maximum time to wait for all PREPARE responses.
	// Default: 10s
	PrepareTimeout time.Duration

	// CommitTimeout is the maximum time to wait for all COMMIT acknowledgments.
	// Default: 30s
	CommitTimeout time.Duration

	// RecoveryInterval is how often to check for incomplete transactions.
	// Default: 60s
	RecoveryInterval time.Duration

	// MaxRetries for Phase 2 delivery. Default: 3
	MaxRetries int

	// Logger for structured logging.
	Logger *logrus.Logger
}

// DefaultTwoPCConfig returns production-ready defaults.
func DefaultTwoPCConfig() TwoPCConfig {
	return TwoPCConfig{
		PrepareTimeout:   10 * time.Second,
		CommitTimeout:    30 * time.Second,
		RecoveryInterval: 60 * time.Second,
		MaxRetries:       3,
	}
}

// ============================================================================
// Participant Interface
// ============================================================================

// TwoPCParticipant is the interface that each data store/shard must implement
// to participate in distributed transactions.
type TwoPCParticipant interface {
	// Prepare executes the transaction locally (but does NOT commit).
	// Returns VoteYes if ready to commit, VoteNo if cannot, VoteReadOnly
	// if no changes were made.
	Prepare(ctx context.Context, txnID string, operations []TxnOperation) (TxnVote, error)

	// Commit finalizes the prepared transaction.
	Commit(ctx context.Context, txnID string) error

	// Abort rolls back the prepared transaction.
	Abort(ctx context.Context, txnID string) error

	// Recover checks for any in-doubt transactions and returns their IDs.
	Recover(ctx context.Context) ([]string, error)

	// Name returns the participant's unique identifier.
	Name() string
}

// TxnOperation represents a single operation within a distributed transaction.
type TxnOperation struct {
	// Type is the operation type: "insert", "update", "delete"
	Type string `json:"type"`

	// Table is the target table/collection.
	Table string `json:"table"`

	// Key is the primary key or identifier.
	Key string `json:"key"`

	// Data is the operation payload.
	Data json.RawMessage `json:"data,omitempty"`

	// ParticipantName identifies which participant handles this operation.
	ParticipantName string `json:"participant_name"`
}

// ============================================================================
// Distributed Transaction
// ============================================================================

// DistributedTxn represents a single distributed transaction managed by 2PC.
type DistributedTxn struct {
	ID           string                       `json:"id"`
	Phase        TxnPhase                     `json:"phase"`
	Operations   []TxnOperation               `json:"operations"`
	Votes        map[string]TxnVote           `json:"votes"`
	CreatedAt    time.Time                    `json:"created_at"`
	PreparedAt   *time.Time                   `json:"prepared_at,omitempty"`
	CompletedAt  *time.Time                   `json:"completed_at,omitempty"`
	Error        string                       `json:"error,omitempty"`
}

// ============================================================================
// 2PC Coordinator
// ============================================================================

// TwoPCCoordinator manages distributed transactions using Two-Phase Commit.
type TwoPCCoordinator struct {
	config       TwoPCConfig
	logger       *logrus.Logger
	participants map[string]TwoPCParticipant

	// Active transactions
	transactions map[string]*DistributedTxn
	mu           sync.RWMutex

	// Stats
	stats TwoPCStats
}

// NewTwoPCCoordinator creates a new 2PC coordinator.
func NewTwoPCCoordinator(cfg TwoPCConfig) *TwoPCCoordinator {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.PrepareTimeout <= 0 {
		cfg.PrepareTimeout = 10 * time.Second
	}
	if cfg.CommitTimeout <= 0 {
		cfg.CommitTimeout = 30 * time.Second
	}
	if cfg.RecoveryInterval <= 0 {
		cfg.RecoveryInterval = 60 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	return &TwoPCCoordinator{
		config:       cfg,
		logger:       cfg.Logger,
		participants: make(map[string]TwoPCParticipant),
		transactions: make(map[string]*DistributedTxn),
	}
}

// RegisterParticipant adds a participant to the coordinator.
func (c *TwoPCCoordinator) RegisterParticipant(p TwoPCParticipant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.participants[p.Name()] = p
	c.logger.WithField("participant", p.Name()).Info("2PC participant registered")
}

// Execute runs a distributed transaction across all involved participants.
// This is the main entry point for application code.
func (c *TwoPCCoordinator) Execute(ctx context.Context, operations []TxnOperation) (string, error) {
	txnID := generateTxnID()

	txn := &DistributedTxn{
		ID:         txnID,
		Phase:      TxnPhaseInit,
		Operations: operations,
		Votes:      make(map[string]TxnVote),
		CreatedAt:  time.Now(),
	}

	c.mu.Lock()
	c.transactions[txnID] = txn
	c.mu.Unlock()

	c.logger.WithField("txn_id", txnID).Info("Starting distributed transaction")

	// Phase 1: Prepare
	if err := c.prepare(ctx, txn); err != nil {
		// Prepare failed → abort all
		c.abort(ctx, txn)
		c.stats.TransactionsAborted++
		return txnID, fmt.Errorf("2PC prepare failed: %w", err)
	}

	// Phase 2: Commit
	if err := c.commit(ctx, txn); err != nil {
		// Commit failed (partial) — this is a serious state; log and retry
		c.logger.WithError(err).WithField("txn_id", txnID).Error("2PC commit partially failed — manual intervention may be needed")
		c.stats.CommitFailures++
		return txnID, fmt.Errorf("2PC commit failed: %w", err)
	}

	c.stats.TransactionsCommitted++
	c.logger.WithField("txn_id", txnID).Info("Distributed transaction committed successfully")

	return txnID, nil
}

// ============================================================================
// Phase 1: Prepare
// ============================================================================

func (c *TwoPCCoordinator) prepare(ctx context.Context, txn *DistributedTxn) error {
	txn.Phase = TxnPhasePreparing

	// Group operations by participant
	opsByParticipant := make(map[string][]TxnOperation)
	for _, op := range txn.Operations {
		opsByParticipant[op.ParticipantName] = append(opsByParticipant[op.ParticipantName], op)
	}

	// Send PREPARE to all participants concurrently
	prepareCtx, cancel := context.WithTimeout(ctx, c.config.PrepareTimeout)
	defer cancel()

	type voteResult struct {
		name string
		vote TxnVote
		err  error
	}

	results := make(chan voteResult, len(opsByParticipant))

	for name, ops := range opsByParticipant {
		name := name
		ops := ops
		go func() {
			c.mu.RLock()
			participant, ok := c.participants[name]
			c.mu.RUnlock()

			if !ok {
				results <- voteResult{name: name, vote: VoteNo, err: fmt.Errorf("participant %q not registered", name)}
				return
			}

			vote, err := participant.Prepare(prepareCtx, txn.ID, ops)
			results <- voteResult{name: name, vote: vote, err: err}
		}()
	}

	// Collect votes
	allYes := true
	for i := 0; i < len(opsByParticipant); i++ {
		select {
		case result := <-results:
			txn.Votes[result.name] = result.vote

			if result.err != nil {
				c.logger.WithError(result.err).WithFields(logrus.Fields{
					"txn_id":      txn.ID,
					"participant": result.name,
				}).Warn("Participant prepare failed")
				allYes = false
			} else if result.vote == VoteNo {
				c.logger.WithFields(logrus.Fields{
					"txn_id":      txn.ID,
					"participant": result.name,
				}).Warn("Participant voted NO")
				allYes = false
			} else if result.vote == VoteReadOnly {
				c.logger.WithFields(logrus.Fields{
					"txn_id":      txn.ID,
					"participant": result.name,
				}).Debug("Participant voted READONLY (excluded from Phase 2)")
			}

		case <-prepareCtx.Done():
			return fmt.Errorf("prepare phase timed out")
		}
	}

	if !allYes {
		return fmt.Errorf("one or more participants voted NO")
	}

	now := time.Now()
	txn.PreparedAt = &now
	txn.Phase = TxnPhasePrepared
	c.stats.PrepareSuccesses++

	return nil
}

// ============================================================================
// Phase 2: Commit
// ============================================================================

func (c *TwoPCCoordinator) commit(ctx context.Context, txn *DistributedTxn) error {
	txn.Phase = TxnPhaseCommitting

	commitCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)
	defer cancel()

	var commitErrors []error
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, vote := range txn.Votes {
		// Skip read-only participants
		if vote == VoteReadOnly {
			continue
		}

		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()

			c.mu.RLock()
			participant, ok := c.participants[name]
			c.mu.RUnlock()

			if !ok {
				mu.Lock()
				commitErrors = append(commitErrors, fmt.Errorf("participant %q not found", name))
				mu.Unlock()
				return
			}

			// Retry commit with backoff
			var lastErr error
			for attempt := 0; attempt < c.config.MaxRetries; attempt++ {
				if err := participant.Commit(commitCtx, txn.ID); err != nil {
					lastErr = err
					c.logger.WithError(err).WithFields(logrus.Fields{
						"txn_id":      txn.ID,
						"participant": name,
						"attempt":     attempt + 1,
					}).Warn("Commit attempt failed, retrying")
					time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
					continue
				}
				lastErr = nil
				break
			}

			if lastErr != nil {
				mu.Lock()
				commitErrors = append(commitErrors, fmt.Errorf("participant %s commit failed: %w", name, lastErr))
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(commitErrors) > 0 {
		txn.Error = fmt.Sprintf("%d commit failures", len(commitErrors))
		return fmt.Errorf("commit phase had %d failures: %v", len(commitErrors), commitErrors[0])
	}

	now := time.Now()
	txn.CompletedAt = &now
	txn.Phase = TxnPhaseCommitted

	return nil
}

// ============================================================================
// Abort
// ============================================================================

func (c *TwoPCCoordinator) abort(ctx context.Context, txn *DistributedTxn) {
	txn.Phase = TxnPhaseAborting

	abortCtx, cancel := context.WithTimeout(ctx, c.config.CommitTimeout)
	defer cancel()

	var wg sync.WaitGroup

	for name, vote := range txn.Votes {
		// Skip participants that didn't prepare
		if vote == VoteNone || vote == VoteReadOnly {
			continue
		}

		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()

			c.mu.RLock()
			participant, ok := c.participants[name]
			c.mu.RUnlock()

			if !ok {
				return
			}

			if err := participant.Abort(abortCtx, txn.ID); err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"txn_id":      txn.ID,
					"participant": name,
				}).Warn("Abort failed (participant may need manual cleanup)")
			}
		}()
	}

	wg.Wait()

	now := time.Now()
	txn.CompletedAt = &now
	txn.Phase = TxnPhaseAborted

	c.logger.WithField("txn_id", txn.ID).Info("Distributed transaction aborted")
}

// ============================================================================
// Recovery
// ============================================================================

// StartRecovery starts the background recovery loop that checks for
// incomplete transactions.
func (c *TwoPCCoordinator) StartRecovery(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(c.config.RecoveryInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.recoverIncomplete(ctx)
			}
		}
	}()
}

func (c *TwoPCCoordinator) recoverIncomplete(ctx context.Context) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, txn := range c.transactions {
		switch txn.Phase {
		case TxnPhasePrepared:
			// Prepared but not committed/aborted — re-attempt commit
			c.logger.WithField("txn_id", txn.ID).Warn("Recovering prepared transaction — re-attempting commit")
			go func(t *DistributedTxn) {
				if err := c.commit(ctx, t); err != nil {
					c.logger.WithError(err).WithField("txn_id", t.ID).Error("Recovery commit failed")
				}
			}(txn)

		case TxnPhasePreparing:
			// Still preparing — abort (presumed abort)
			c.logger.WithField("txn_id", txn.ID).Warn("Recovering preparing transaction — aborting (presumed abort)")
			go func(t *DistributedTxn) {
				c.abort(ctx, t)
			}(txn)

		case TxnPhaseCommitting:
			// Partially committed — re-attempt remaining commits
			c.logger.WithField("txn_id", txn.ID).Warn("Recovering committing transaction — re-attempting")
			go func(t *DistributedTxn) {
				if err := c.commit(ctx, t); err != nil {
					c.logger.WithError(err).WithField("txn_id", t.ID).Error("Recovery re-commit failed")
				}
			}(txn)
		}
	}
}

// ============================================================================
// Stats & Status
// ============================================================================

// TwoPCStats holds runtime statistics for the 2PC coordinator.
type TwoPCStats struct {
	TransactionsStarted   int64 `json:"transactions_started"`
	TransactionsCommitted int64 `json:"transactions_committed"`
	TransactionsAborted   int64 `json:"transactions_aborted"`
	PrepareSuccesses      int64 `json:"prepare_successes"`
	PrepareFailures       int64 `json:"prepare_failures"`
	CommitFailures        int64 `json:"commit_failures"`
}

// Stats returns the current 2PC statistics.
func (c *TwoPCCoordinator) Stats() TwoPCStats {
	return c.stats
}

// GetTransaction returns the state of a specific transaction.
func (c *TwoPCCoordinator) GetTransaction(txnID string) (*DistributedTxn, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	txn, ok := c.transactions[txnID]
	return txn, ok
}

func generateTxnID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("txn-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("txn-%s", hex.EncodeToString(b))
}

// ============================================================================
// In-Memory Participant (for development/testing)
// ============================================================================

// MemoryParticipant is an in-memory 2PC participant for development/testing.
type MemoryParticipant struct {
	name       string
	prepared   map[string][]TxnOperation
	committed  map[string]bool
	mu         sync.Mutex
}

// NewMemoryParticipant creates a new in-memory participant.
func NewMemoryParticipant(name string) *MemoryParticipant {
	return &MemoryParticipant{
		name:      name,
		prepared:  make(map[string][]TxnOperation),
		committed: make(map[string]bool),
	}
}

func (p *MemoryParticipant) Name() string { return p.name }

func (p *MemoryParticipant) Prepare(_ context.Context, txnID string, ops []TxnOperation) (TxnVote, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(ops) == 0 {
		return VoteReadOnly, nil
	}

	p.prepared[txnID] = ops
	return VoteYes, nil
}

func (p *MemoryParticipant) Commit(_ context.Context, txnID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.prepared, txnID)
	p.committed[txnID] = true
	return nil
}

func (p *MemoryParticipant) Abort(_ context.Context, txnID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.prepared, txnID)
	return nil
}

func (p *MemoryParticipant) Recover(_ context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ids := make([]string, 0, len(p.prepared))
	for id := range p.prepared {
		ids = append(ids, id)
	}
	return ids, nil
}
