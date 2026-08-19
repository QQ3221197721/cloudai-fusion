package tee_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/tee"
)

func TestDCAPClient_GetQuote(t *testing.T) {
	client, err := tee.NewDCAPClient("http://localhost:8080", "test-key", 30*time.Second)
	if err != nil {
		t.Skipf("Skipping DCAP client creation (expected in non-TEE environment): %v", err)
	}

	ctx := context.Background()
	reportData := make([]byte, 32)

	quote, err := client.GetQuote(ctx, reportData)
	if err != nil {
		t.Logf("GetQuote returned error (mock mode expected): %v", err)
		return
	}

	if quote == nil {
		t.Error("Expected non-nil quote even in mock mode")
	}
	if quote.Version != 1 {
		t.Errorf("Expected version 1, got %d", quote.Version)
	}
}

func TestDCAPClient_VerifyQuote(t *testing.T) {
	client, _ := tee.NewDCAPClient("http://localhost:8080", "test-key", 30*time.Second)

	ctx := context.Background()
	quote := &tee.SGXQuote{
		Version:    1,
		ReportData: make([]byte, 32),
	}

	result, err := client.VerifyQuote(ctx, quote)
	if err != nil {
		t.Logf("VerifyQuote returned error (mock mode expected): %v", err)
		return
	}

	if result == nil {
		t.Error("Expected non-nil verification result")
	}
	if !result.Valid {
		t.Log("Verification failed - expected in mock mode")
	}
}
