package tracing

import (
	"context"
	"testing"
)

func TestInit_Disabled(t *testing.T) {
	p, err := Init(context.Background(), Config{
		ServiceName: "test",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("Init disabled: %v", err)
	}
	if p == nil {
		t.Fatal("provider should not be nil even when disabled")
	}
	if p.Tracer() == nil {
		t.Error("tracer should not be nil")
	}
	// Shutdown should work
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestInit_NoEndpoint(t *testing.T) {
	p, err := Init(context.Background(), Config{
		ServiceName: "test",
		Enabled:     true,
		Endpoint:    "", // empty endpoint = disabled
	})
	if err != nil {
		t.Fatalf("Init with empty endpoint: %v", err)
	}
	if p == nil {
		t.Fatal("provider should not be nil")
	}
}

func TestStartSpan(t *testing.T) {
	// Init a no-op provider first
	_, err := Init(context.Background(), Config{
		ServiceName: "test",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx, span := StartSpan(context.Background(), "test-op")
	if ctx == nil {
		t.Error("context should not be nil")
	}
	if span == nil {
		t.Error("span should not be nil")
	}
	span.End()
}

func TestConfig_SampleRate(t *testing.T) {
	// Verify sample rate clamping by checking Init doesn't error
	configs := []Config{
		{ServiceName: "test", Enabled: false, SampleRate: -1},
		{ServiceName: "test", Enabled: false, SampleRate: 0},
		{ServiceName: "test", Enabled: false, SampleRate: 0.5},
		{ServiceName: "test", Enabled: false, SampleRate: 1.0},
		{ServiceName: "test", Enabled: false, SampleRate: 2.0},
	}
	for _, cfg := range configs {
		p, err := Init(context.Background(), cfg)
		if err != nil {
			t.Errorf("Init with SampleRate=%f: %v", cfg.SampleRate, err)
		}
		p.Shutdown(context.Background())
	}
}
