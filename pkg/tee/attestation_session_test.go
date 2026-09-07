package tee

import (
	"context"
	"testing"
	"time"
)

// ============================================================================
// Attestation Session Cache — 性能真实 benchmark
// ============================================================================

func setupSimCache(t testing.TB) (*SessionCache, func()) {
	t.Helper()
	if tb, ok := t.(*testing.T); ok {
		setupFakeDevRoot(tb)
	}
	cache := NewSessionCache(nil, 1*time.Minute)
	ba, _ := NewBoundAttestor(WithSimulation())
	cache.attestor = ba
	return cache, func() {}
}

func BenchmarkSessionCache_Attest_Hit(b *testing.B) {
	cache, _ := setupSimCache(b)
	enclaveID := "bench-enclave"
	nonce := []byte("benchmark-nonce")

	ctx := context.Background()
	_, _, _ = cache.Attest(ctx, enclaveID, nonce)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		tok, reattested, err := cache.Attest(ctx, enclaveID, nonce)
		if tok == nil || err != nil {
			b.Fatalf("unexpected: %v", err)
		}
		if reattested {
			b.Fatal("should be hit path")
		}
		_ = tok.IssuedAt
		_ = tok.Measurement
		_ = tok.SessionID
	}
}

// BenchmarkSessionCache_Establish 测量昂贵路径（每次强制重新建立会话/完整证明）
func BenchmarkSessionCache_Establish(b *testing.B) {
	cache, _ := setupSimCache(b)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sess, _ := cache.establish(ctx, "establish-enclave")
		if sess == nil {
			b.Fatal("establish returned nil session")
		}
		_ = sess.ID
	}
}

func BenchmarkSessionCache_Verify(b *testing.B) {
	cache, _ := setupSimCache(b)
	ctx := context.Background()
	nonce := []byte("verify-nonce")

	tok, _, _ := cache.Attest(ctx, "bench-sess", nonce)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := cache.VerifyToken(tok, nonce, false); err != nil {
			b.Fatalf("unexpected: %v", err)
		}
	}
}

func BenchmarkSessionCache_IssueToken(b *testing.B) {
	cache, _ := setupSimCache(b)
	sess := &AttestationSession{
		ID:            "bench-id",
		Measurement:   "MRENCLAVE-bench",
		hmacKey:       make([]byte, 32),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		token := cache.issueToken(sess, []byte("token-nonce"))
		_ = token.MAC
	}
}
