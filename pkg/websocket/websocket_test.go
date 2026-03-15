package websocket

import (
	"context"
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	h := NewHub(nil)
	if h == nil {
		t.Fatal("NewHub should return non-nil")
	}
	if h.clients == nil {
		t.Error("clients map should be initialized")
	}
	if h.ClientCount() != 0 {
		t.Error("new hub should have 0 clients")
	}
}

func TestHub_Publish(t *testing.T) {
	h := NewHub(nil)
	// Publish without running should not panic (buffer absorbs)
	h.Publish(&Event{
		Type: EventTypeAlert,
		Data: map[string]interface{}{"message": "test"},
	})
}

func TestHub_PublishAlert(t *testing.T) {
	h := NewHub(nil)
	h.PublishAlert("critical", "server down", "cluster-1")
	// Should not panic
}

func TestHub_PublishWorkloadStatus(t *testing.T) {
	h := NewHub(nil)
	h.PublishWorkloadStatus("wl-1", "pending", "running")
	// Should not panic
}

func TestHub_Run_ShutdownClean(t *testing.T) {
	h := NewHub(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Hub.Run did not return after context cancel")
	}
}

func TestEvent_TimestampAutoFill(t *testing.T) {
	h := NewHub(nil)
	e := &Event{
		Type: EventTypeSystem,
		Data: map[string]interface{}{"key": "val"},
	}
	h.Publish(e)
	if e.Timestamp.IsZero() {
		t.Error("Publish should auto-fill Timestamp")
	}
}

func TestEventType_Constants(t *testing.T) {
	types := []EventType{
		EventTypeAlert, EventTypeWorkloadStatus, EventTypeClusterHealth,
		EventTypeGPUMetrics, EventTypeAuditLog, EventTypeScheduler, EventTypeSystem,
	}
	seen := make(map[EventType]bool)
	for _, et := range types {
		if string(et) == "" {
			t.Error("event type should not be empty")
		}
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
	}
}

// ============================================================================
// WebSocket Frame Encoding Tests
// ============================================================================

func TestEncodeTextFrame_Short(t *testing.T) {
	payload := []byte("hello")
	frame := encodeTextFrame(payload)
	if frame[0] != 0x81 {
		t.Errorf("first byte should be 0x81 (FIN+text), got 0x%02x", frame[0])
	}
	if frame[1] != 5 {
		t.Errorf("length byte should be 5, got %d", frame[1])
	}
	if string(frame[2:]) != "hello" {
		t.Errorf("payload mismatch")
	}
}

func TestEncodeTextFrame_Medium(t *testing.T) {
	payload := make([]byte, 200)
	for i := range payload {
		payload[i] = 'x'
	}
	frame := encodeTextFrame(payload)
	if frame[0] != 0x81 {
		t.Errorf("first byte should be 0x81, got 0x%02x", frame[0])
	}
	if frame[1] != 126 {
		t.Errorf("length indicator should be 126 for medium, got %d", frame[1])
	}
	extLen := int(frame[2])<<8 | int(frame[3])
	if extLen != 200 {
		t.Errorf("extended length = %d, want 200", extLen)
	}
}

func TestEncodeTextFrame_Large(t *testing.T) {
	payload := make([]byte, 70000)
	frame := encodeTextFrame(payload)
	if frame[1] != 127 {
		t.Errorf("length indicator should be 127 for large, got %d", frame[1])
	}
	if len(frame) != 10+70000 {
		t.Errorf("frame length = %d, want %d", len(frame), 10+70000)
	}
}

// ============================================================================
// String Helper Tests
// ============================================================================

func TestComputeAcceptKey(t *testing.T) {
	// RFC 6455 example key
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	accept := computeAcceptKey(key)
	expected := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if accept != expected {
		t.Errorf("computeAcceptKey = %q, want %q", accept, expected)
	}
}

func TestSplitTopics(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"alert,workload_status", 2},
		{"single", 1},
		{"", 0},
		{" a , b , c ", 3},
	}
	for _, tt := range tests {
		got := splitTopics(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitTopics(%q) = %v (len=%d), want len=%d", tt.input, got, len(got), tt.want)
		}
	}
}

func TestSplitString(t *testing.T) {
	got := splitString("a,b,c", ",")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("splitString = %v", got)
	}
	got = splitString("single", ",")
	if len(got) != 1 || got[0] != "single" {
		t.Errorf("splitString no sep = %v", got)
	}
}

func TestIndexOf(t *testing.T) {
	if indexOf("hello world", "world") != 6 {
		t.Error("indexOf should find 'world' at 6")
	}
	if indexOf("hello", "xyz") != -1 {
		t.Error("indexOf should return -1 for not found")
	}
}

func TestTrimSpace(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  hello  ", "hello"},
		{"hello", "hello"},
		{"\thello\t", "hello"},
		{"", ""},
		{"  ", ""},
	}
	for _, tt := range tests {
		got := trimSpace(tt.input)
		if got != tt.want {
			t.Errorf("trimSpace(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
