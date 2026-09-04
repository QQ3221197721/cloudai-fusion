package security

import (
	"fmt"
	"testing"
)

// FLIP M36 Compliance Benchmark - REAL comparison
func TestM36ComplianceBenchmark(t *testing.T) {
	fmt.Println("🚀 Running FLIP M36 Compliance Benchmark...")
	RunM36ComplianceBenchmark()
	t.Log("✅ M36 Benchmark completed!")
}
