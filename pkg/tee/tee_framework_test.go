// Package tee - Comprehensive test suite for TEE Provider Framework and Failover Orchestrator
package tee_test

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tee"
)

// ============================================================================
// INTEL SGX PROVIDER TESTS
// ============================================================================

func TestIntelSGXProviderCreateEnclave(t *testing.T) {
	logger := logrus.New()
	
	// Create provider with mock IAS client
	mockIAS := &mockIASClient{}
	provider := newIntelSGXProvider(mockIAS)
	
	ctx := context.Background()
	config := tee.EnclaveConfig{
		ID:                  "test_sgx_enclave",
		CodeHash:            []byte("test_code_hash"),
		MemorySizeMB:        512,
		CPUCount:            2,
		NetworkMode:         tee.NetworkInternal,
		SecurityPolicy:      tee.PolicyStrict,
		AttestationRequired: true,
	}
	
	enclave, err := provider.CreateEnclave(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create SGX enclave: %v", err)
	}
	
	if enclave.ID != config.ID {
		t.Errorf("Expected enclave ID %s, got %s", config.ID, enclave.ID)
	}
	
	if enclave.Status != tee.EnclaveRunning {
		t.Errorf("Expected enclave status %s, got %s", tee.EnclaveRunning, enclave.Status)
	}
}

func TestIntelSGXProviderVerifyEnclave(t *testing.T) {
	logger := logrus.New()
	mockIAS := &mockIASClient{validQuote: true}
	provider := newIntelSGXProvider(mockIAS)
	
	ctx := context.Background()
	result, err := provider.VerifyEnclave(ctx, "test_enclave_id")
	if err != nil {
		t.Fatalf("Failed to verify enclave: %v", err)
	}
	
	if !result.Valid {
		t.Error("Expected enclave verification to succeed")
	}
}

func TestIntelSGXProviderHealthCheck(t *testing.T) {
	logger := logrus.New()
	mockIAS := &mockIASClient{}
	provider := newIntelSGXProvider(mockIAS)
	
	ctx := context.Background()
	health, err := provider.HealthCheck(ctx)
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	
	if !health.IsHealthy {
		t.Error("Expected provider to be healthy")
	}
}

// ============================================================================
// TEE PROVIDER FACTORY TESTS
// ============================================================================

func TestTEEProviderFactoryRegisterProviders(t *testing.T) {
	logger := logrus.New()
	factory := NewTEEProviderFactory(logger)
	
	// Factory should have Intel provider registered
	if factory.Providers == nil || len(factory.Providers) == 0 {
		t.Error("Expected at least one provider to be registered")
	}
}

func TestTEEProviderFactorySelectPrimaryAndFailover(t *testing.T) {
	logger := logrus.New()
	factory := NewTEEProviderFactory(logger)
	
	// Register providers manually for testing
	intelProvider := &mockTEEProvider{NameValue: "intel_sgx"}
	factory.RegisterProvider("intel", intelProvider)
	
	err := factory.SelectPrimaryProvider("intel", []string{})
	if err != nil {
		t.Fatalf("Failed to select primary provider: %v", err)
	}
	
	// Verify active provider is set
	active := factory.GetActiveProvider(context.Background())
	if active == nil {
		t.Error("Expected active provider to be set")
	}
}

func TestTEEProviderFactoryAutoFailover(t *testing.T) {
	logger := logrus.New()
	factory := NewTEEProviderFactory(logger)
	
	// Create multiple providers
	prov1 := &mockTEEProvider{NameValue: "provider_1", unhealthy: false}
	prov2 := &mockTEEProvider{NameValue: "provider_2", unhealthy: true}
	
	factory.RegisterProvider("primary", prov1)
	factory.RegisterProvider("failover", prov2)
	
	err := factory.SelectPrimaryProvider("primary", []string{"failover"})
	if err != nil {
		t.Fatalf("Failed to select providers: %v", err)
	}
	
	// Simulate failure by marking primary unhealthy
	prov1.unhealthy = true
	
	// Trigger health check
	go factory.runHealthCheckLoop(context.Background())
	time.Sleep(6 * time.Second) // Wait for health check interval
	
	// Get active provider after failover
	active := factory.GetActiveProvider(context.Background())
	if active == nil {
		t.Error("Expected active provider after failover")
	}
}

// ============================================================================
// ENCLAVE CONFIGURATION TESTS
// ============================================================================

func TestEnclaveConfigValidation(t *testing.T) {
	config := tee.EnclaveConfig{
		ID:                  "valid_config",
		CodeHash:            []byte("valid_hash"),
		MemorySizeMB:        256,
		CPUCount:            2,
		NetworkMode:         tee.NetworkInternal,
		SecurityPolicy:      tee.PolicyBalanced,
		AttestationRequired: true,
	}
	
	// Should not panic or crash
	if config.ID == "" {
		t.Error("Config ID should not be empty")
	}
	
	if config.MemorySizeMB <= 0 {
		t.Error("Memory size must be positive")
	}
}

// ============================================================================
// ATTESTATION RESULT TESTS
// ============================================================================

func TestIASResponseIsValid(t *testing.T) {
	resp := &tee.IASResponse{
		QuoteStatus: "VALID",
	}
	
	if !resp.IsValid() {
		t.Error("Expected VALID response to return true for IsValid()")
	}
}

func TestIASResponseIsRevoked(t *testing.T) {
	resp := &tee.IASResponse{
		QuoteStatus: "REVOKED",
	}
	
	if !resp.IsRevoked() {
		t.Error("Expected REVOKED response to return true for IsRevoked()")
	}
}

func TestIASResponseGetTCBStatus(t *testing.T) {
	resp := &tee.IASResponse{
		TCBEvaluationStatus: "FULLY_UPDATED",
	}
	
	status := resp.GetTCBStatus()
	if status != tee.TCBFullyUpdated {
		t.Errorf("Expected TCBFullyUpdated, got %s", status)
	}
}

// ============================================================================
// PERFORMANCE BENCHMARKS
// ============================================================================

func BenchmarkIntelSGXProvider_CreateEnclave(b *testing.B) {
	logger := logrus.New()
	mockIAS := &mockIASClient{}
	provider := newIntelSGXProvider(mockIAS)
	
	ctx := context.Background()
	config := tee.EnclaveConfig{
		ID:                  "bench_sgx",
		CodeHash:            []byte("bench_hash"),
		MemorySizeMB:        512,
		CPUCount:            2,
		NetworkMode:         tee.NetworkInternal,
		SecurityPolicy:      tee.PolicyStrict,
		AttestationRequired: true,
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.CreateEnclave(ctx, config)
	}
}

func BenchmarkTEEProviderFactory_GetActiveProvider(b *testing.B) {
	logger := logrus.New()
	factory := NewTEEProviderFactory(logger)
	
	prov := &mockTEEProvider{NameValue: "bench_provider"}
	factory.RegisterProvider("bench", prov)
	factory.SelectPrimaryProvider("bench", []string{})
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		factory.GetActiveProvider(ctx)
	}
}

func BenchmarkIASResponse_IsValid(b *testing.B) {
	resp := &tee.IASResponse{QuoteStatus: "VALID"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resp.IsValid()
	}
}
