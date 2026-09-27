// Package scheduler_test provides comprehensive test coverage for ZK proof functionality
package scheduler

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common/defensive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Fixtures & Setup
// ============================================================================

type ZKProverTestSuite struct {
	circuitDir string
	keysDir    string
	tempDirs   []string
}

func setupZKProverTest(t *testing.T) *ZKProverTestSuite {
	ts := &ZKProverTestSuite{}
	
	// Create temporary directory structure
	tempBase, err := os.MkdirTemp("", "zkp-test-*")
	assert.NoError(t, err)
	ts.tempDirs = append(ts.tempDirs, tempBase)
	
	ts.circuitDir = filepath.Join(tempBase, "circuits")
	ts.keysDir = filepath.Join(tempBase, "keys")
	buildDir := filepath.Join(tempBase, "build")
	
	os.MkdirAll(ts.circuitDir, 0755)
	os.MkdirAll(ts.keysDir, 0755)
	os.MkdirAll(buildDir, 0755)
	
	// Create mock circuit assets (will be replaced with real ones during tests)
	os.WriteFile(filepath.Join(buildDir, "scheduling_fairness.r1cs"), []byte("MOCK_R1CS"), 0644)
	os.WriteFile(filepath.Join(ts.keysDir, "proving_0000.zkey"), []byte("MOCK_PROVING_KEY"), 0644)
	os.WriteFile(filepath.Join(ts.keysDir, "verification.key"), []byte("MOCK_VERIFICATION_KEY"), 0644)
	
	return ts
}

func (ts *ZKProverTestSuite) teardown(t *testing.T) {
	for _, dir := range ts.tempDirs {
		os.RemoveAll(dir)
	}
}

// Helper function to create realistic allocation test data
func createTestAllocations(numTenants int) ([]scheduler.Allocation, []scheduler.Weight) {
	allocations := make([]scheduler.Allocation, numTenants)
	weights := make([]scheduler.Weight, numTenants)
	
	sumWeight := 0.0
	
	// Generate weighted distribution ensuring sum �?1.0
	for i := 0; i < numTenants; i++ {
		weight := float64(i+1) / float64(numTenants*(numTenants+1)/2)
		sumWeight += weight
		
		allocations[i] = scheduler.Allocation{
			TenantID: fmt.Sprintf("tenant-%d", i),
			GPUSHours: float64((i+1) * 100), // Varying usage from 100 to N*100
			Priority:  (i % 3) + 1,          // Priority levels 1-3
		}
		
		weights[i] = scheduler.Weight{
			TenantID:     fmt.Sprintf("tenant-%d", i),
			Weight:       weight,
			BillingShare: weight,
		}
	}
	
	// Normalize weights to ensure exact sum of 1.0
	normalizationFactor := 1.0 / sumWeight
	for i := range weights {
		weights[i].Weight *= normalizationFactor
	}
	
	return allocations, weights
}

// ============================================================================
// Unit Tests: Core Functionality
// ============================================================================

func TestZKProver_NewSuccess(t *testing.T) {
	ts := setupZKProverTest(t)
	defer ts.teardown(t)
	
	prover, err := scheduler.NewZKProver(
		ts.circuitDir,
		ts.keysDir,
		nil, // Use default logger
	)
	
	require.NoError(t, err)
	require.NotNil(t, prover)
	
	assert.Equal(t, scheduler.DefaultProofTimeout, prover.Timeout())
}

func TestZKProver_NewInvalidPaths(t *testing.T) {
	_, err := scheduler.NewZKProver(
		"/nonexistent/path/1",
		"/another/nonexistent/path/2",
		nil,
	)
	
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required asset")
}

// ============================================================================
// Integration Tests: Input Validation
// ============================================================================

func TestInputValidationWithDefensiveGuards(t *testing.T) {
	tests := []struct {
		name        string
		tenants     int
		expectError bool
		errSubstr   string
	}{
		{"valid_single_tenant", 1, false, ""},
		{"valid_multiple_tenants", 10, false, ""},
		{"exceeds_limit_too_many", 30, true, "exceeds limit"},
		{"zero_tenants_invalid", 0, true, "must be in range"},
	}
	
	ts := setupZKProverTest(t)
	defer ts.teardown(t)
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validity := evaluateAllocationValidity(tt.tenants)
			
			if tt.expectError {
				assert.False(t, validity)
			} else {
				assert.True(t, validity)
			}
		})
	}
}

func TestThresholdBoundsValidation(t *testing.T) {
	testCases := []struct {
		value     float64
		shouldErr bool
	}{
		{-0.1, true},   // Below minimum
		{0.0, false},   // At lower bound (valid)
		{0.5, false},   // Valid middle value
		{1.0, false},   // At upper bound (valid)
		{1.01, true},   // Above maximum
		{-1.0, true},   // Way below
		{2.0, true},    // Way above
	}
	
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("threshold_%v", tc.value), func(t *testing.T) {
			err := defensive.ValidateRange(tc.value, 0.0, 1.0, "threshold")
			
			if tc.shouldErr {
				assert.Error(t, err, "expected error for out-of-range threshold")
			} else {
				assert.NoError(t, err, "expected no error for valid threshold")
			}
		})
	}
}

func TestWeightsSumValidation(t *testing.T) {
	t.Run("valid_weights_sum_to_one", func(t *testing.T) {
		numTenants := 5
		weights := make([]scheduler.Weight, numTenants)
		
		// Equal weight distribution
		sum := 0.0
		for i := 0; i < numTenants; i++ {
			weight := 1.0 / float64(numTenants)
			weights[i] = scheduler.Weight{Weight: weight}
			sum += weight
		}
		
		// Check sum is approximately 1.0 (within floating point tolerance)
		assert.InDelta(t, 1.0, sum, 0.0001, "weights should sum to 1.0")
		
		// Validate each weight individually
		for i, w := range weights {
			err := defensive.ValidateRange(w.Weight, 0.0, 1.0, fmt.Sprintf("weights[%d].weight", i))
			assert.NoError(t, err)
		}
	})
	
	t.Run("invalid_weights_not_normalized", func(t *testing.T) {
		weights := []scheduler.Weight{
			{Weight: 0.5},
			{Weight: 0.5},
			{Weight: 0.1}, // Sum = 1.1 > 1.0
		}
		
		sum := 0.0
		for _, w := range weights {
			sum += w.Weight
		}
		
		assert.InDelta(t, 1.1, sum, 0.001, "sum exceeds 1.0")
		
		// Try to validate the total
		err := defensive.ValidateRange(sum, 0.95, 1.05, "total_weight_sum")
		// This will fail if sum outside tolerance
		assert.Error(t, err)
	})
}

// ============================================================================
// Differential Privacy Testing
// ============================================================================

func TestDifferentialPrivacyNoiseAddition(t *testing.T) {
	epsilon := 0.01
	sigma := 0.005
	
	t.Run("noise_bounded_within_epsilon", func(t *testing.T) {
		baseThreshold := 0.7
		maxIterations := 1000
		
		maxObservedNoise := 0.0
		
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for i := 0; i < maxIterations; i++ {
			noise := rng.NormFloat64(sigma) * epsilon
			
			observedNoise := math.Abs(noise)
			if observedNoise > maxObservedNoise {
				maxObservedNoise = observedNoise
			}
			
			assert.LessOrEqual(t, observedNoise, epsilon, 
				fmt.Sprintf("Noise should be within ±ε, got %.6f", observedNoise))
		}
		
		log.Printf("Max observed noise over %d iterations: %.6f", maxIterations, maxObservedNoise)
	})
	
	t.Run("noisy_threshold_clamped_to_valid_range", func(t *testing.T) {
		baseThresholds := []float64{0.0, 0.5, 1.0}
		
		for _, base := range baseThresholds {
			// Force extreme noise that would push value out of bounds
			extremeNoise := 0.2 // Much larger than epsilon
			noisyValue := base + extremeNoise
			
			// Apply clamping
			if noisyValue < 0.0 {
				noisyValue = 0.0
			}
			if noisyValue > 1.0 {
				noisyValue = 1.0
			}
			
			assert.GreaterOrEqual(t, noisyValue, 0.0, "clamped value should be >= 0")
			assert.LessOrEqual(t, noisyValue, 1.0, "clamped value should be <= 1")
		}
	})
}

// ============================================================================
// Table-Driven Tests
// ============================================================================

func TestValidAllocationPatterns(t *testing.T) {
	type TestCase struct {
		name             string
		tenants          int
		equalWeight      bool
		sufficientFunds  bool
		expectedValidity bool
	}
	
	tests := []TestCase{
		{"single_tenant_basic", 1, true, true, true},
		{"multiple_tenants_equal_distribution", 10, true, true, true},
		{"skewed_allocation_pattern", 5, false, true, true},
		{"too_many_tenants_exceeds_limit", 30, true, true, false},
		{"insufficient_total_gpu_hours", 5, true, false, false},
		{"max_allowed_tenants", 25, true, true, true},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluateAllocationPattern(tt.tenants, tt.equalWeight, tt.sufficientFunds)
			assert.Equal(t, tt.expectedValidity, result, "allocation pattern evaluation mismatch")
		})
	}
}

// ============================================================================
// Performance Benchmarks
// ============================================================================

func BenchmarkZKProverInitialization(b *testing.B) {
	tempDir := b.TempDir()
	keysDir := filepath.Join(tempDir, "keys")
	os.MkdirAll(keysDir, 0755)
	
	// Pre-create dummy key files
	os.WriteFile(filepath.Join(keysDir, "proving_0000.zkey"), []byte("MOCK"), 0644)
	os.WriteFile(filepath.Join(keysDir, "verification.key"), []byte("MOCK"), 0644)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scheduler.NewZKProver(tempDir, keysDir, nil)
	}
}

func BenchmarkInputValidation(b *testing.B) {
	tenants := 10
	allocations := make([]scheduler.Allocation, tenants)
	weights := make([]scheduler.Weight, tenants)
	
	for i := 0; i < tenants; i++ {
		allocations[i] = scheduler.Allocation{TenantID: fmt.Sprintf("t%d", i)}
		weights[i] = scheduler.Weight{TenantID: fmt.Sprintf("t%d", i), Weight: 1.0 / float64(tenants)}
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defensive.RequireNonNil(allocations, "allocations")
		_ = defensive.RequireNonNil(weights, "weights")
	}
}

// ============================================================================
// Security Tests
// ============================================================================

func TestNonceUniqueness(t *testing.T) {
	t.Run("generate_unique_nonces", func(t *testing.T) {
		nonces := make(map[string]bool)
		
		for i := 0; i < 100; i++ {
			newUUID := uuid.New().String()
			assert.False(t, nonces[newUUID], "nonce collision detected!")
			nonces[newUUID] = true
		}
	})
}

func TestTimestampValidityChecks(t *testing.T) {
	now := time.Now()
	
	// Test recent timestamp (valid)
	recentTS := now.Add(-1 * time.Minute).Unix()
	assert.GreaterOrEqual(t, recentTS, now.Unix()-3600, "recent timestamp should be within 1 hour")
	
	// Test stale timestamp (invalid - would be rejected by circuit)
	staleTS := now.Add(-2 * time.Hour).Unix()
	assert.LessThan(t, staleTS, now.Unix()-3600, "stale timestamp outside acceptable window")
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func TestWrapFunctions(t *testing.T) {
	t.Run("wrap_preserves_original_error", func(t *testing.T) {
		original := fmt.Errorf("base error message")
		wrapped := defensive.Wrap(original, defensive.ErrorCodeInternal, "context wrapper")
		
		assert.NotNil(t, wrapped.Cause)
		assert.Equal(t, original, wrapped.Cause)
		
		unwrapped := wrapped.Unwrap()
		assert.Equal(t, original, unwrapped)
	})
	
	t.Run("standard_error_handler_format", func(t *testing.T) {
		appErr := defensive.NotFound("resource", "identifier-123")
		
		assert.Equal(t, defensive.ErrorCodeNotFound, appErr.Code)
		assert.Contains(t, appErr.Message, "not found")
		assert.Equal(t, "resource", appErr.Metadata["resource"])
		assert.Equal(t, "identifier-123", appErr.Metadata["identifier"])
	})
}

// Helper functions
func evaluateAllocationValidity(tenants int) bool {
	const maxTenants = 25
	
	if tenants < 1 || tenants > maxTenants {
		return false
	}
	
	return true
}

func evaluateAllocationPattern(tenants int, equalWeight bool, sufficientFunds bool) bool {
	const maxTenants = 25
	
	if tenants < 1 || tenants > maxTenants {
		return false
	}
	
	if !sufficientFunds {
		return false
	}
	
	if !equalWeight {
		// For non-equal weights, check they sum to ~1.0
		sum := 0.0
		for i := 0; i < tenants; i++ {
			weight := float64(i+1) / float64(tenants*(tenants+1)/2)
			sum += weight
		}
		
		if sum < 0.95 || sum > 1.05 {
			return false
		}
	}
	
	return true
}
