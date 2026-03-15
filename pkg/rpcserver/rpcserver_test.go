package rpcserver

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.MaxRecvMsgSize != 16*1024*1024 {
		t.Errorf("MaxRecvMsgSize = %d, want 16MB", cfg.MaxRecvMsgSize)
	}
	if cfg.MaxSendMsgSize != 16*1024*1024 {
		t.Errorf("MaxSendMsgSize = %d, want 16MB", cfg.MaxSendMsgSize)
	}
	if cfg.KeepaliveInterval != 30*time.Second {
		t.Errorf("KeepaliveInterval = %v, want 30s", cfg.KeepaliveInterval)
	}
	if cfg.KeepaliveTimeout != 10*time.Second {
		t.Errorf("KeepaliveTimeout = %v, want 10s", cfg.KeepaliveTimeout)
	}
	if cfg.MaxConcurrentStreams != 1000 {
		t.Errorf("MaxConcurrentStreams = %d, want 1000", cfg.MaxConcurrentStreams)
	}
}

func TestNew(t *testing.T) {
	s := New(DefaultConfig(), nil)
	if s == nil {
		t.Fatal("New should return non-nil")
	}
	if s.grpcServer == nil {
		t.Error("grpcServer should be initialized")
	}
	if s.healthServer == nil {
		t.Error("healthServer should be initialized")
	}
}

func TestNew_DefaultsApplied(t *testing.T) {
	// Empty config — all zeros should get defaults
	s := New(Config{Port: 8080}, nil)
	if s.config.MaxRecvMsgSize != 16*1024*1024 {
		t.Errorf("MaxRecvMsgSize should default to 16MB, got %d", s.config.MaxRecvMsgSize)
	}
	if s.config.KeepaliveInterval != 30*time.Second {
		t.Errorf("KeepaliveInterval should default to 30s")
	}
}

func TestGRPCServer(t *testing.T) {
	s := New(DefaultConfig(), nil)
	gs := s.GRPCServer()
	if gs == nil {
		t.Error("GRPCServer() should return non-nil")
	}
}

func TestStop_NotRunning(t *testing.T) {
	s := New(DefaultConfig(), nil)
	// Should not panic when stopping a server that was never started
	s.Stop()
}

func TestStart_AlreadyRunning(t *testing.T) {
	s := New(Config{Port: 0}, nil)
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	err := s.Start()
	if err == nil {
		t.Error("Start() should return error if already running")
	}
}
