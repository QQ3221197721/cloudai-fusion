package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNew_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "debug", Format: "json", Output: &buf, Component: "test"})
	l.Info("hello")

	out := buf.String()
	if !strings.Contains(out, `"message":"hello"`) {
		t.Errorf("JSON log should contain message field, got: %s", out)
	}
	if !strings.Contains(out, `"level":"info"`) {
		t.Errorf("JSON log should contain level field, got: %s", out)
	}
}

func TestNew_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "text", Output: &buf})
	l.Info("text output")

	out := buf.String()
	if !strings.Contains(out, "text output") {
		t.Errorf("Text log should contain message, got: %s", out)
	}
}

func TestNew_DefaultLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "invalid-level", Output: &buf})
	// Should default to info level, so debug should not appear
	l.Debug("should not appear")
	if buf.Len() > 0 {
		t.Error("debug message should not appear with default info level")
	}
}

func TestWithContext_NilContext(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Output: &buf, Component: "api"})
	entry := l.WithContext(nil)
	entry.Info("nil ctx")

	out := buf.String()
	if !strings.Contains(out, "api") {
		t.Errorf("should include component even with nil ctx: %s", out)
	}
}

func TestWithContext_Fields(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "json", Output: &buf, Component: "sched"})

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithSpanID(ctx, "span-456")
	ctx = WithRequestID(ctx, "req-789")
	ctx = WithUserID(ctx, "user-abc")

	l.WithContext(ctx).Info("enriched log")

	out := buf.String()
	for _, expected := range []string{"trace-123", "span-456", "req-789", "user-abc"} {
		if !strings.Contains(out, expected) {
			t.Errorf("log should contain %q, got: %s", expected, out)
		}
	}
}

func TestWithContext_ComponentOverride(t *testing.T) {
	var buf bytes.Buffer
	l := New(Config{Level: "info", Format: "json", Output: &buf, Component: "default"})
	ctx := WithComponent(context.Background(), "override")
	l.WithContext(ctx).Info("test")

	out := buf.String()
	if !strings.Contains(out, "override") {
		t.Errorf("component should be overridden, got: %s", out)
	}
}

func TestLogrus(t *testing.T) {
	l := New(Config{Level: "info"})
	if l.Logrus() == nil {
		t.Error("Logrus() should not return nil")
	}
}

func TestTraceIDFromContext(t *testing.T) {
	ctx := context.Background()
	if TraceIDFromContext(ctx) != "" {
		t.Error("empty context should return empty trace id")
	}
	ctx = WithTraceID(ctx, "abc-123")
	if TraceIDFromContext(ctx) != "abc-123" {
		t.Errorf("TraceIDFromContext = %q, want 'abc-123'", TraceIDFromContext(ctx))
	}
}

func TestRequestIDFromContext(t *testing.T) {
	ctx := context.Background()
	if RequestIDFromContext(ctx) != "" {
		t.Error("empty context should return empty request id")
	}
	ctx = WithRequestID(ctx, "req-xyz")
	if RequestIDFromContext(ctx) != "req-xyz" {
		t.Errorf("RequestIDFromContext = %q, want 'req-xyz'", RequestIDFromContext(ctx))
	}
}
