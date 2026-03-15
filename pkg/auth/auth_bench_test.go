package auth

import (
	"testing"
	"time"
)

// ============================================================================
// Benchmark: Password Hashing (bcrypt is intentionally slow)
// ============================================================================

func BenchmarkHashPassword(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword("BenchmarkP@ssword123!")
	}
}

func BenchmarkCheckPassword(b *testing.B) {
	hash, _ := HashPassword("BenchmarkP@ssword123!")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		CheckPassword("BenchmarkP@ssword123!", hash)
	}
}

// ============================================================================
// Benchmark: JWT Token Generation & Validation
// ============================================================================

func BenchmarkGenerateToken(b *testing.B) {
	svc, _ := NewService(Config{
		JWTSecret: "benchmark-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &User{ID: "bench-user", Username: "benchuser", Role: RoleAdmin, Status: "active"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = svc.GenerateToken(user)
	}
}

func BenchmarkValidateToken(b *testing.B) {
	svc, _ := NewService(Config{
		JWTSecret: "benchmark-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &User{ID: "bench-user", Username: "benchuser", Role: RoleAdmin, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = svc.ValidateToken(tokenResp.AccessToken)
	}
}

// ============================================================================
// Benchmark: RBAC Permission Check
// ============================================================================

func BenchmarkHasPermission(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		HasPermission(RoleAdmin, PermClusterCreate)
	}
}

func BenchmarkHasPermission_Miss(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		HasPermission(RoleViewer, PermClusterCreate) // denied
	}
}

// ============================================================================
// Fuzz: Password Hashing (handles arbitrary input safely)
// ============================================================================

func FuzzHashPassword(f *testing.F) {
	// Seed corpus
	f.Add("password123")
	f.Add("")
	f.Add("a")
	f.Add("very-long-password-that-exceeds-normal-length-limits-!@#$%^&*()")
	f.Add("密码测试中文")
	f.Add("\x00\x01\x02\xff")

	f.Fuzz(func(t *testing.T, password string) {
		hash, err := HashPassword(password)
		if err != nil {
			// bcrypt has a 72-byte limit; passwords exceeding it may truncate but should not crash
			return
		}
		if hash == "" {
			t.Error("hash should not be empty for valid input")
		}
		// Verify the hash is actually valid bcrypt
		if hash[0] != '$' {
			t.Errorf("hash should start with '$', got: %q", hash[:1])
		}
	})
}

// ============================================================================
// Fuzz: JWT Token Validation (should never panic)
// ============================================================================

func FuzzValidateToken(f *testing.F) {
	svc, _ := NewService(Config{
		JWTSecret: "fuzz-test-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})

	// Seed with valid token
	user := &User{ID: "1", Username: "test", Role: RoleAdmin, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	f.Add(tokenResp.AccessToken)
	f.Add("invalid-token")
	f.Add("")
	f.Add("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.invalid")
	f.Add("not.a.jwt")
	f.Add("\x00\xff\x01")

	f.Fuzz(func(t *testing.T, tokenString string) {
		// ValidateToken should never panic regardless of input
		_, _ = svc.ValidateToken(tokenString)
	})
}
