package election

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// BenchmarkMemoryElector_Creation measures overhead of creating memory elector
func BenchmarkMemoryElector_Creation(b *testing.B) {
	config := Config{
		Backend: "memory",
		Identity: "bench-1",
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod: 2 * time.Second,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, _ := New(config)
		_ = e.Identity()
	}
}

// BenchmarkMemoryElector_Leadership_Becoming measures time to become leader
func BenchmarkMemoryElector_Leadership_Becoming(b *testing.B) {
	config := Config{
		Backend: "memory",
		Identity: "bench-2",
		LeaseDuration: 1 * time.Second,
		RenewDeadline: 500 * time.Millisecond,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, _ := New(config)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		go e.Run(ctx)
		start := time.Now()
		for !e.IsLeader() && time.Since(start) < 1*time.Second {
			time.Sleep(1 * time.Millisecond)
		}
		cancel()
	}
}

// BenchmarkMemoryElector_Renewal measures renewal rate
func BenchmarkMemoryElector_Renewal(b *testing.B) {
	config := Config{
		Backend: "memory",
		Identity: "bench-3",
		LeaseDuration: 1 * time.Second,
		RenewDeadline: 500 * time.Millisecond,
	}
	e, _ := New(config)
	ctx, cancel := context.WithCancel(context.Background())
	go e.Run(ctx)
	defer cancel()

	b.ResetTimer()
	var count int64
	for i := 0; i < b.N; i++ {
		stats := e.Stats()
		if stats.RenewalsSuccess > count {
			count = stats.RenewalsSuccess
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// BenchmarkSplitBrainDetector_MultipleLeaders measures detection speed with multiple leaders
func BenchmarkSplitBrainDetector_MultipleLeaders(b *testing.B) {
	d := NewSplitBrainDetector(SplitBrainConfig{
		QuorumSize:    1,
		LeaseDuration: 15 * time.Second,
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.ReportPeer(PeerStatus{Identity: "peer-1", IsReachable: true, ClaimsLeader: true, LeaseTime: time.Now()})
		d.ReportPeer(PeerStatus{Identity: "peer-2", IsReachable: true, ClaimsLeader: true, LeaseTime: time.Now()})
		d.Check("self", "peer-1")
		d.mu.Lock()
		delete(d.peerStatus, "peer-1")
		delete(d.peerStatus, "peer-2")
		d.mu.Unlock()
	}
}

// BenchmarkKubernetesElector_Fallback measures k8s elector fallback time to memory
func BenchmarkKubernetesElector_Fallback(b *testing.B) {
	config := Config{
		Backend: "kubernetes",
		Identity: "bench-4",
		Namespace: "test",
		LockName: "test-lock",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := New(config)
		if err != nil {
			b.Fatalf("New failed: %v", err)
		}
		_ = e.Identity()
	}
}

// BenchmarkEtcdElector_Fallback measures etcd elector initialization time
func BenchmarkEtcdElector_Fallback(b *testing.B) {
	config := Config{
		Backend: "etcd",
		Identity: "bench-5",
		EtcdEndpoints: []string{"localhost:2379"},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e, err := New(config)
		if err != nil {
			b.Fatalf("New failed: %v", err)
		}
		_ = e.Identity()
	}
}

// BenchmarkConcurrentLeadershipSwitches measures leadership switch throughput
func BenchmarkConcurrentLeadershipSwitches(b *testing.B) {
	const numInstances = 10
	instances := make([]LeaderElector, numInstances)
	var identities []string
	
	for i := 0; i < numInstances; i++ {
		cfg := Config{
			Backend: "memory",
			Identity: fmt.Sprintf("instance-%d", i),
			LeaseDuration: 100 * time.Millisecond,
			RenewDeadline: 50 * time.Millisecond,
		}
		e, _ := New(cfg)
		identities = append(identities, cfg.Identity)
		instances[i] = e
	}

	ctx, cancel := context.WithCancel(context.Background())
	for _, inst := range instances {
		go inst.Run(ctx)
	}
	
	time.Sleep(50 * time.Millisecond)
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		for _, inst := range instances {
			if inst.IsLeader() {
				inst.Resign()
			}
		}
		time.Sleep(20 * time.Millisecond)
		for _, inst := range instances {
			if !inst.IsLeader() {
				// Will automatically become leader on next tick
				_ = inst.IsLeader()
			}
		}
	}
	
	cancel()
}
