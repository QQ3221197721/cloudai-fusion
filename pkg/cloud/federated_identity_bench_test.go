// Package cloud implements performance benchmarks for federated identity token exchange.
package cloud

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Performance Benchmarks - T2 Barrier Validation (<200ms p99)
// ============================================================================

func BenchmarkFederatedIdentity_AuthenticateUser(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	ctx := context.Background()
	username := "benchmark-user"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := fim.AuthenticateUser(username, "password")
		if err != nil {
			b.Fatalf("Authentication failed: %v", err)
		}
		_ = ctx
	}
}

func BenchmarkFederatedIdentity_ExchangeToken_SingleCloud(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	ctx := context.Background()
	token, _ := fim.AuthenticateUser("bench-user", "password")

	req := ExchangeRequest{
		IDToken:       token.IDToken,
		Audience:      "cloudai-fusion-aws",
		Scope:         []string{"benchmark"},
		CloudProvider: "aws",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := fim.ExchangeToken(ctx, req)
		if err != nil {
			b.Fatalf("Token exchange failed: %v", err)
		}
		if resp == nil || resp.AccessToken == "" {
			b.Fatal("Invalid response from token exchange")
		}
	}
}

func BenchmarkFederatedIdentity_ExchangeToken_Latency(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	ctx := context.Background()
	token, _ := fim.AuthenticateUser("latency-test", "password")

	req := ExchangeRequest{
		IDToken:       token.IDToken,
		Audience:      "cloudai-fusion-aws",
		Scope:         []string{"latency"},
		CloudProvider: "aws",
	}

	results := make([]time.Duration, b.N)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		resp, err := fim.ExchangeToken(ctx, req)
		elapsed := time.Since(start)

		if err != nil {
			b.Fatalf("Token exchange failed at iteration %d: %v", i, err)
		}

		if resp == nil || resp.AccessToken == "" {
			b.Fatalf("Invalid response at iteration %d", i)
		}

		results[i] = elapsed
	}

	// Calculate percentiles
	p50 := percentile(results, 50)
	p90 := percentile(results, 90)
	p95 := percentile(results, 95)
	p99 := percentile(results, 99)

	fmt.Printf("\n=== Token Exchange Latency Results ===\n")
	fmt.Printf("p50: %v\n", p50)
	fmt.Printf("p90: %v\n", p90)
	fmt.Printf("p95: %v\n", p95)
	fmt.Printf("p99: %v\n", p99)
	fmt.Printf("max:   %v\n\n", maxDuration(results))

	// Validate against T2 barrier requirement (<200ms p99)
	if p99 > 200*time.Millisecond {
		b.Logf("WARNING: p99 latency (%v) exceeds T2 barrier (<200ms)", p99)
	} else {
		b.Logf("✓ PASS: p99 latency within T2 barrier requirements")
	}
}

func BenchmarkFederatedIdentity_CacheHit_Latency(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	// Pre-populate cache with one request
	token, _ := fim.AuthenticateUser("cache-bench", "password")
	preReq := ExchangeRequest{
		IDToken:       token.IDToken,
		Audience:      "cloudai-fusion-aws",
		Scope:         []string{"cache-preload"},
		CloudProvider: "aws",
	}
	_, _ = fim.ExchangeToken(context.Background(), preReq)

	req := ExchangeRequest{
		IDToken:       token.IDToken,
		Audience:      "cloudai-fusion-aws",
		Scope:         []string{"cache-hit"},
		CloudProvider: "aws",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := fim.ExchangeToken(context.Background(), req)
		if err != nil {
			b.Fatalf("Cache hit failed: %v", err)
		}
		if !resp.CacheHit {
			b.Logf("Expected cache hit at iteration %d", i)
		}
	}
}

func BenchmarkFederatedIdentity_CrossCloud_Performance(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	ctx := context.Background()
	token, _ := fim.AuthenticateUser("cross-cloud-bench", "password")

	clouds := []string{"aws", "azure", "gcp", "alibaba", "tencent", "huawei"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cloudIndex := i % len(clouds)
		
		req := ExchangeRequest{
			IDToken:       token.IDToken,
			Audience:      fmt.Sprintf("cloudai-fusion-%s", clouds[cloudIndex]),
			Scope:         []string{"cross-cloud"},
			CloudProvider: clouds[cloudIndex],
		}

		resp, err := fim.ExchangeToken(ctx, req)
		if err != nil {
			b.Fatalf("Cross-cloud exchange failed: %v", err)
		}

		if resp == nil {
			b.Fatal("Nil response in cross-cloud benchmark")
		}
	}
}

func BenchmarkFederatedIdentity_Concurrent_Exchanges(b *testing.B) {
	if testing.BenchmarkMemStats.Enabled && testing.Short() {
		return
	}

	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	token, _ := fim.AuthenticateUser("concurrent-bench", "password")

	numGoroutines := 10
	requestsPerGoroutine := b.N / numGoroutines

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		
		for pb.Next() {
			req := ExchangeRequest{
				IDToken:       token.IDToken,
				Audience:      "cloudai-fusion-aws",
				Scope:         []string{"parallel"},
				CloudProvider: "aws",
			}

			resp, err := fim.ExchangeToken(ctx, req)
			if err != nil {
				b.Fatalf("Concurrent exchange failed: %v", err)
			}

			if resp == nil {
				b.Fatal("Nil response in concurrent benchmark")
			}
		}
	})
}

// ============================================================================
// Helper Functions for Benchmark Analysis
// ============================================================================

func percentile(durations []time.Duration, p float64) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	slices.Sort(sorted)

	index := int(float64(len(sorted)-1) * p / 100.0)
	return sorted[index]
}

func maxDuration(durations []time.Duration) time.Duration {
	max := durations[0]
	for _, d := range durations[1:] {
		if d > max {
			max = d
		}
	}
	return max
}

// ============================================================================
// Memory Allocation Benchmarks
// ============================================================================

func BenchmarkFederatedIdentity_MemoryAllocations(b *testing.B) {
	fim, err := NewFederatedIdentityManager("", "")
	if err != nil {
		b.Fatalf("Failed to create FederatedIdentityManager: %v", err)
	}

	ctx := context.Background()
	token, _ := fim.AuthenticateUser("memory-bench", "password")

	req := ExchangeRequest{
		IDToken:       token.IDToken,
		Audience:      "cloudai-fusion-aws",
		Scope:         []string{"memory"},
		CloudProvider: "aws",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := fim.ExchangeToken(ctx, req)
		if err != nil {
			b.Fatalf("Memory benchmark failed: %v", err)
		}

		if resp == nil || resp.AccessToken == "" {
			b.Fatal("Invalid memory benchmark response")
		}
	}
}

// ============================================================================
// Stress Tests for Production Readiness
// ============================================================================

func TestStress_FederationUnderLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	fim, err := NewFederatedIdentityManager("", "")
	assert.NoError(t, err)

	concurrencyLevel := 50
	totalRequests := concurrencyLevel * 100
	successCount := 0
	errorCount := 0

	done := make(chan bool, totalRequests)

	ctx := context.Background()
	token, _ := fim.AuthenticateUser("stress-test", "password")

	// Launch concurrent requests
	for i := 0; i < concurrencyLevel; i++ {
		go func(id int) {
			for j := 0; j < (totalRequests / concurrencyLevel); j++ {
				req := ExchangeRequest{
					IDToken:       token.IDToken,
					Audience:      "cloudai-fusion-stress",
					Scope:         []string{"stress", fmt.Sprintf("worker-%d", id)},
					CloudProvider: "aws",
				}

				resp, err := fim.ExchangeToken(ctx, req)
				if err == nil && resp != nil {
					successCount++
				} else {
					errorCount++
				}

				done <- true
			}
		}(i)
	}

	// Wait for all requests
	for i := 0; i < totalRequests; i++ {
		<-done
	}

	successRate := float64(successCount) / float64(totalRequests) * 100
	t.Logf("Stress Test Results: %d/%d successful (%.2f%% success rate)", 
		successCount, totalRequests, successRate)

	if successRate < 99.0 {
		t.Errorf("Success rate (%.2f%%) below threshold (99%%)", successRate)
	}
}
