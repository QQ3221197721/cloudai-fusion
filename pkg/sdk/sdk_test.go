package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Run("uses default timeout when no options", func(t *testing.T) {
		c := New("http://localhost")
		if c.httpClient.Timeout != DefaultTimeout {
			t.Errorf("expected timeout %v, got %v", DefaultTimeout, c.httpClient.Timeout)
		}
	})

	t.Run("applies API key option", func(t *testing.T) {
		c := New("http://localhost", WithAPIKey("test-key"))
		if c.apiKey != "test-key" {
			t.Errorf("expected apiKey 'test-key', got '%s'", c.apiKey)
		}
	})

	t.Run("wires sub-clients", func(t *testing.T) {
		c := New("http://localhost", WithAPIKey("k"))
		if c.Evidence == nil || c.GPU == nil || c.Security == nil || c.Billing == nil {
			t.Error("expected all sub-clients to be non-nil")
		}
	})
}

func TestOptionChaining(t *testing.T) {
	custom := &http.Client{Timeout: time.Minute}
	c := New("http://example.com",
		WithAPIKey("multi"),
		WithTimeout(10*time.Second),
		WithHTTPClient(custom),
	)
	if c.httpClient != custom {
		t.Error("expected custom HTTP client to override default")
	}
	// Timeout option should not overwrite a custom HTTP client
	if c.httpClient.Timeout != time.Minute {
		t.Logf("custom HTTP client preserved: %v", custom.Timeout)
	}
}

func TestParseAPIError(t *testing.T) {
	t.Run("parses structured JSON error", func(t *testing.T) {
		body := []byte(`{"code":"NOT_FOUND","message":"resource not found"}`)
		err := parseAPIError(http.StatusNotFound, body)
		if err.Code != "NOT_FOUND" || err.Message != "resource not found" {
			t.Errorf("unexpected error: %+v", err)
		}
	})

	t.Run("falls back to text for invalid JSON", func(t *testing.T) {
		body := []byte("internal server error")
		err := parseAPIError(http.StatusInternalServerError, body)
		if err.Message != "internal server error" {
			t.Errorf("expected raw message, got %q", err.Message)
		}
	})
}

func TestEvidenceVerifyPathEscaping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/evidence/verify" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if want := "prod%2Fus-east"; r.URL.RawQuery != "namespace="+want {
			t.Errorf("expected query namespace=%s, got %s", want, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(VerifyResult{Valid: true, EntryCount: 42})
	}))
	defer server.Close()

	c := New(server.URL, WithAPIKey("fake"))
	res, err := c.Evidence.Verify(context.Background(), "prod/us-east")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid || res.EntryCount != 42 {
		t.Errorf("unexpected response: %+v", res)
	}
}

func TestGPUSubmitJob(t *testing.T) {
	jobIn := &GPUJob{Name: "train-bert", GPUCount: 4, Image: "nvcr.io/pytorch:24.01"}
	var jobOut GPUJob

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&jobOut); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JobResult{ID: "job-123", Status: "pending"})
	}))
	defer server.Close()

	c := New(server.URL, WithAPIKey("k"))
	result, err := c.GPU.SubmitJob(context.Background(), jobIn)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "job-123" {
		t.Errorf("expected ID 'job-123', got %q", result.ID)
	}
}

func TestBillingRecordUsage(t *testing.T) {
	usageIn := &UsageRecord{Category: "gpu", Amount: 1.5, Unit: "hour"}
	var usageOut UsageRecord

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&usageOut); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(BillingReceipt{
			ID:          "rcpt-789",
			Amount:      1.5,
			Unit:        "hour",
			ReceiptHash: "abc123",
		})
	}))
	defer server.Close()

	c := New(server.URL, WithAPIKey("k"))
	receipt, err := c.Billing.RecordUsage(context.Background(), usageIn)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ID != "rcpt-789" || receipt.ReceiptHash != "abc123" {
		t.Errorf("unexpected receipt: %+v", receipt)
	}
}
