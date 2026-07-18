package cluster

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func realRaftTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x44}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestRealRaft_ElectsLeaderAppliesAndCommits(t *testing.T) {
	var applied [][]byte
	node, err := NewRealRaftNode(RealRaftConfig{
		NodeID: "n1",
		Apply:  func(cmd []byte) error { applied = append(applied, cmd); return nil },
	})
	if err != nil {
		t.Fatalf("new real raft: %v", err)
	}
	defer func() { _ = node.Stop() }()

	// Real leader election must converge for the bootstrapped single voter.
	if !node.WaitForLeader(3 * time.Second) {
		t.Fatalf("node should become leader, state=%s", node.State())
	}
	if !node.IsLeader() {
		t.Fatal("IsLeader must be true after election")
	}

	// A real Apply goes through the Raft log and is committed + applied by the FSM.
	if err := node.Apply([]byte(`{"op":"set","k":"a","v":1}`), 2*time.Second); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if node.AppliedCount() < 1 {
		t.Fatalf("FSM must have applied the committed command, got %d", node.AppliedCount())
	}
	if len(applied) < 1 {
		t.Fatal("apply callback must have observed the committed command")
	}
}

func TestRealRaft_EmitsVerifiableCommitEvidence(t *testing.T) {
	ledger := realRaftTestLedger(t)
	node, err := NewRealRaftNode(RealRaftConfig{NodeID: "n1", Recorder: ledger})
	if err != nil {
		t.Fatalf("new real raft: %v", err)
	}
	defer func() { _ = node.Stop() }()

	if !node.WaitForLeader(3 * time.Second) {
		t.Fatal("node should become leader")
	}
	if err := node.Apply([]byte("cmd-1"), 2*time.Second); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Poll for the raft.commit receipt (FSM apply happens asynchronously).
	deadline := time.Now().Add(2 * time.Second)
	var sawCommit bool
	for time.Now().Before(deadline) && !sawCommit {
		all, _ := ledger.Store().All(context.Background())
		for _, e := range all {
			if e.Action == "raft.commit" {
				sawCommit = true
			}
		}
		if !sawCommit {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !sawCommit {
		t.Fatal("expected a raft.commit evidence receipt")
	}

	// Every receipt must carry the real backend fact and the chain must verify.
	all, _ := ledger.Store().All(context.Background())
	var sawRealBackend bool
	for _, e := range all {
		for _, b := range e.Backends {
			if b.Component == "consensus.raft" && b.Mode == "real" && b.Driver == "hashicorp-raft" {
				sawRealBackend = true
			}
		}
	}
	if !sawRealBackend {
		t.Fatal("raft evidence must record consensus.raft as real (hashicorp-raft)")
	}
	rep, err := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.Valid {
		t.Fatalf("raft evidence chain must verify, got %+v", rep)
	}
}
