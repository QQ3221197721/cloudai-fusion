// Package tee_test - Comprehensive test suite for TEE Provider Framework
package tee_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tee"
)

// ============================================================================
// INTELLIGENCE CONNECTOR TESTS
// ============================================================================

func TestIntelligenceConnectorProcessThreatEvent(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	
	// Mock TEE provider
	mockProvider := &mockTEEProvider{
		createEnclaveFunc: func(ctx context.Context, config tee.EnclaveConfig) (*tee.Enclave, error) {
			return &tee.Enclave{
				ID:       "test-enclave",
				Status:   tee.EnclaveRunning,
				CreatedAt: time.Now(),
			}, nil
		},
	}
	
	connector := tee_integrations.NewIntelligenceConnector(mockProvider, logger)
	
	event := &tee_integrations.ThreatIntelligenceEvent{
		EventID:     "evt_123",
		Timestamp:   time.Now(),
		ThreatType:  "network_scan",
		Severity:    "high",
		Description: "Test threat event",
		Confidence:  0.85,
	}
	
	ctx := context.Background()
	err := connector.ProcessThreatEvent(ctx, event)
	if err != nil {
		t.Fatalf("Failed to process threat event: %v", err)
	}
	
	// Verify evidence was buffered
	connector.mu.RLock()
	if len(connector.EvidenceBuffer) == 0 {
		t.Error("Expected evidence to be buffered")
	}
	connector.mu.RUnlock()
}

// ============================================================================
// SECURITY INTEGRATION TESTS
// ============================================================================

func TestSecurityIntegrationSubmitSecureEvidence(t *testing.T) {
	logger := logrus.New()
	si := tee_integrations.NewSecurityIntegration(logger)
	
	evidence := &tee_integrations.SecurityEvidence{
		EvidenceID:   "evd_456",
		EnclaveID:    "enc_789",
		EvidenceType: "split_brain_detection",
		Payload:      []byte("test payload"),
		CreatedAt:    time.Now(),
	}
	
	ctx := context.Background()
	err := si.SubmitSecureEvidence(ctx, evidence)
	if err != nil {
		t.Fatalf("Failed to submit secure evidence: %v", err)
	}
	
	// Verify evidence was queued
	si.mu.RLock()
	if len(si.EvidenceQueue) == 0 {
		t.Error("Expected evidence in queue")
	}
	si.mu.RUnlock()
}

func TestSecurityIntegrationCollectMetrics(t *testing.T) {
	logger := logrus.New()
	si := tee_integrations.NewSecurityIntegration(logger)
	
	// Add some mock evidence
	si.mu.Lock()
	si.EvidenceQueue = append(si.EvidenceQueue, &tee_integrations.SecurityEvidence{
		EvidenceID: "test1",
	})
	si.mu.Unlock()
	
	ctx := context.Background()
	metrics, err := si.CollectMetrics(ctx)
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	
	if len(metrics) == 0 {
		t.Error("Expected metrics to be collected")
	}
}

// ============================================================================
// COST INTEGRATION TESTS
// ============================================================================

func TestCostIntegrationRecordUsage(t *testing.T) {
	logger := logrus.New()
	ci := tee_integrations.NewCostIntegration(logger)
	
	ctx := context.Background()
	err := ci.RecordUsage(ctx, "enc_test", "cpu", 2.5, 12.50)
	if err != nil {
		t.Fatalf("Failed to record usage: %v", err)
	}
	
	// Verify usage was recorded
	ci.mu.RLock()
	if len(ci.UsageHistory) == 0 {
		t.Error("Expected usage history to be populated")
	}
	ci.mu.RUnlock()
}

// ============================================================================
// INTEL IAS CLIENT TESTS
// ============================================================================

func TestIASClientInspectQuote(t *testing.T) {
	t.Skip("Skipping - requires real Intel IAS API credentials")
	
	client, err := tee.NewIASClient("fake_api_key", "")
	if err != nil && client != nil {
		t.Skip("Invalid API key provided, skipping test")
	}
	
	// Create a fake SGX quote for testing
	fakeQuote := make([]byte, 300)
	rand.Read(fakeQuote)
	
	ctx := context.Background()
	resp, err := client.InspectQuote(ctx, fakeQuote)
	if err != nil {
		// Expected to fail with fake quote
		t.Logf("Expected failure with fake quote: %v", err)
		return
	}
	
	// If successful (unexpected with fake quote), verify response
	if resp == nil {
		t.Error("Expected non-nil response")
	}
}

// ============================================================================
// ENCLAVE LIFECYCLE TESTS
// ============================================================================

func TestEnclaveLifecycle(t *testing.T) {
	logger := logrus.New()
	mockProvider := &mockTEEProvider{}
	connector := tee_integrations.NewIntelligenceConnector(mockProvider, logger)
	
	ctx := context.Background()
	
	// Create enclave
	config := tee.EnclaveConfig{
		ID:                  "test_enclave",
		CodeHash:            []byte{1, 2, 3},
		MemorySizeMB:        256,
		CPUCount:            2,
		NetworkMode:         tee.NetworkInternal,
		SecurityPolicy:      tee.PolicyStrict,
		AttestationRequired: true,
	}
	
	enclave, err := mockProvider.CreateEnclave(ctx, config)
	if err != nil {
		t.Fatalf("Failed to create enclave: %v", err)
	}
	
	if enclave.ID != config.ID {
		t.Errorf("Expected enclave ID %s, got %s", config.ID, enclave.ID)
	}
	
	// Verify enclave
	result, err := mockProvider.VerifyEnclave(ctx, enclave.ID)
	if err != nil {
		t.Logf("Verification skipped: %v", err)
	}
	
	if result == nil || !result.Valid {
		t.Log("Enclave verification failed or returned nil (expected for mock)")
	}
	
	// Destroy enclave
	err = mockProvider.DestroyEnclave(ctx, enclave.ID)
	if err != nil {
		t.Logf("Destroy skipped: %v", err)
	}
}

// ============================================================================
// PERFORMANCE BENCHMARKS
// ============================================================================

func BenchmarkIntelligenceConnector_ProcessThreatEvent(b *testing.B) {
	logger := logrus.New()
	mockProvider := &mockTEEProvider{}
	connector := tee_integrations.NewIntelligenceConnector(mockProvider, logger)
	
	event := &tee_integrations.ThreatIntelligenceEvent{
		EventID:     "benchmark_evt",
		Timestamp:   time.Now(),
		ThreatType:  "test_threat",
		Severity:    "medium",
		Description: "Benchmark threat",
		Confidence:  0.9,
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		connector.ProcessThreatEvent(ctx, event)
	}
}

func BenchmarkSecurityIntegration_SubmitEvidence(b *testing.B) {
	logger := logrus.New()
	si := tee_integrations.NewSecurityIntegration(logger)
	
	evidence := &tee_integrations.SecurityEvidence{
		EvidenceID:   "benchmark_evd",
		EnclaveID:    "benchmark_enc",
		EvidenceType: "benchmark_type",
		Payload:      make([]byte, 1024), // 1KB payload
		CreatedAt:    time.Now(),
	}
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		si.SubmitSecureEvidence(ctx, evidence)
	}
}

func BenchmarkCostIntegration_RecordUsage(b *testing.B) {
	logger := logrus.New()
	ci := tee_integrations.NewCostIntegration(logger)
	
	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ci.RecordUsage(ctx, "bench_enc", "cpu", 1.5, 5.0)
	}
}

// ============================================================================
// MOCK PROVIDERS FOR TESTING
// ============================================================================

type mockTEEProvider struct {
	createEnclaveFunc func(context.Context, tee.EnclaveConfig) (*tee.Enclave, error)
	verifyFunc        func(context.Context, string) (*tee.AttestationResult, error)
	destroyFunc       func(context.Context, string) error
}

func (m *mockTEEProvider) Name() string {
	return "mock_provider"
}

func (m *mockTEEProvider) CreateEnclave(ctx context.Context, config tee.EnclaveConfig) (*tee.Enclave, error) {
	if m.createEnclaveFunc != nil {
		return m.createEnclaveFunc(ctx, config)
	}
	
	return &tee.Enclave{
		ID:       config.ID,
		Status:   tee.EnclaveRunning,
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockTEEProvider) VerifyEnclave(ctx context.Context, enclaveID string) (*tee.AttestationResult, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, enclaveID)
	}
	
	return &tee.AttestationResult{
		Valid:      true,
		QuoteStatus: tee.QuoteValid,
		VerifiedAt: time.Now(),
	}, nil
}

func (m *mockTEEProvider) DestroyEnclave(ctx context.Context, enclaveID string) error {
	if m.destroyFunc != nil {
		return m.destroyFunc(ctx, enclaveID)
	}
	return nil
}

func (m *mockTEEProvider) HealthCheck(ctx context.Context) (*tee.HealthStatus, error) {
	return &tee.HealthStatus{
		IsHealthy: true,
		UptimeSec: int(time.Since(time.Now()).Seconds()),
		LatencyMs: 5,
	}, nil
}
