package cluster

import (
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
)

// buildRaftCluster wires n RealRaftNodes over connected in-memory transports and
// bootstraps a single real Raft cluster. This exercises OUR RealRaftNode in a
// genuine multi-node configuration (real election + replication over transports).
func buildRaftCluster(t *testing.T, n int) []*RealRaftNode {
	t.Helper()
	ids := make([]hraft.ServerID, n)
	addrs := make([]hraft.ServerAddress, n)
	transports := make([]*hraft.InmemTransport, n)
	for i := 0; i < n; i++ {
		ids[i] = hraft.ServerID([]byte{byte('a' + i)})
		a, tr := hraft.NewInmemTransport("")
		addrs[i], transports[i] = a, tr
	}
	// Fully connect the transports so peers can reach each other.
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if i != j {
				transports[i].Connect(addrs[j], transports[j])
			}
		}
	}
	servers := make([]hraft.Server, n)
	for i := 0; i < n; i++ {
		servers[i] = hraft.Server{Suffrage: hraft.Voter, ID: ids[i], Address: addrs[i]}
	}

	nodes := make([]*RealRaftNode, n)
	for i := 0; i < n; i++ {
		cfg := RealRaftConfig{
			NodeID:    string(ids[i]),
			Transport: transports[i],
			Address:   addrs[i],
		}
		if i == 0 {
			cfg.BootstrapServers = servers // exactly one node bootstraps
		}
		node, err := NewRealRaftNode(cfg)
		if err != nil {
			t.Fatalf("node %d: %v", i, err)
		}
		nodes[i] = node
	}
	return nodes
}

func waitForClusterLeader(nodes []*RealRaftNode, timeout time.Duration) *RealRaftNode {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, n := range nodes {
			if n != nil && n.IsLeader() {
				return n
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

func TestRealRaft_MultiNodeElectReplicateAndReelect(t *testing.T) {
	nodes := buildRaftCluster(t, 3)
	defer func() {
		for _, n := range nodes {
			if n != nil {
				_ = n.Stop()
			}
		}
	}()

	// A real leader must be elected among the 3 connected nodes.
	leader := waitForClusterLeader(nodes, 5*time.Second)
	if leader == nil {
		t.Fatal("no leader elected in a 3-node real raft cluster")
	}

	// A command applied on the leader must replicate + commit to ALL FSMs.
	if err := leader.Apply([]byte(`{"op":"set","k":"a"}`), 3*time.Second); err != nil {
		t.Fatalf("apply on leader: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allApplied := true
		for _, n := range nodes {
			if n.AppliedCount() < 1 {
				allApplied = false
			}
		}
		if allApplied {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for i, n := range nodes {
		if n.AppliedCount() < 1 {
			t.Fatalf("node %d FSM did not receive the replicated command", i)
		}
	}

	// Kill the leader -> the remaining nodes must elect a NEW leader (real re-election).
	var survivors []*RealRaftNode
	for _, n := range nodes {
		if n == leader {
			_ = n.Stop()
			continue
		}
		survivors = append(survivors, n)
	}
	newLeader := waitForClusterLeader(survivors, 6*time.Second)
	if newLeader == nil {
		t.Fatal("no new leader elected after killing the original leader")
	}
	if newLeader == leader {
		t.Fatal("the dead node must not remain leader")
	}
}
