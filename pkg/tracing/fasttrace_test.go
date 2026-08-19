package tracing

import (
	"context"
	"errors"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestFastSpanBasics(t *testing.T) {
	tr := NewFastTracer("unit")
	ctx, s := tr.Start(context.Background(), "op", trace.SpanKindInternal)

	if !s.SpanContext().TraceID().IsValid() {
		t.Fatal("trace ID must be valid")
	}
	if !s.SpanContext().SpanID().IsValid() {
		t.Fatal("span ID must be valid")
	}
	if s.Name() != "op" {
		t.Errorf("name = %q, want op", s.Name())
	}
	if got := FastSpanFromContext(ctx); got != s {
		t.Error("span must be retrievable from context")
	}

	s.SetString("k", "v").SetInt("n", 7).SetBool("b", true).SetFloat("f", 1.5)
	var count int
	s.Attributes(func(a FastAttr) {
		switch a.Key {
		case "k":
			if v, ok := a.IsString(); !ok || v != "v" {
				t.Errorf("string attr = %q", v)
			}
		case "n":
			if a.AsInt() != 7 {
				t.Errorf("int attr = %d", a.AsInt())
			}
		case "b":
			if !a.AsBool() {
				t.Error("bool attr must be true")
			}
		case "f":
			if a.AsFloat() != 1.5 {
				t.Errorf("float attr = %v", a.AsFloat())
			}
		}
		count++
	})
	if count != 4 {
		t.Errorf("attr count = %d, want 4", count)
	}

	s.SetError(errors.New("boom"))
	if !s.IsError() {
		t.Error("span should be errored")
	}
	s.End()
}

func TestFastSpanParentLinking(t *testing.T) {
	tr := NewFastTracer("unit")
	ctx, parent := tr.Start(context.Background(), "parent", trace.SpanKindServer)
	_, child := tr.Start(ctx, "child", trace.SpanKindInternal)

	if parent.SpanContext().TraceID() != child.SpanContext().TraceID() {
		t.Error("child must inherit parent's trace ID")
	}
	if parent.SpanContext().SpanID() == child.SpanContext().SpanID() {
		t.Error("child must have a distinct span ID")
	}
	child.End()
	parent.End()
}

func TestFastSpanUniqueIDs(t *testing.T) {
	tr := NewFastTracer("unit")
	seen := make(map[[16]byte]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		_, s := tr.Start(context.Background(), "op", trace.SpanKindInternal)
		id := s.SpanContext().TraceID()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate trace ID at iteration %d", i)
		}
		seen[id] = struct{}{}
		s.End()
	}
}

func TestFastTracerOnEnd(t *testing.T) {
	var ended int
	tr := NewFastTracer("unit", WithOnEnd(func(*FastSpan) { ended++ }))
	_, s := tr.Start(context.Background(), "op", trace.SpanKindInternal)
	s.End()
	if ended != 1 {
		t.Errorf("OnEnd called %d times, want 1", ended)
	}

	// Unsampled spans must not trigger OnEnd.
	var ended2 int
	tr2 := NewFastTracer("unit", WithSampled(false), WithOnEnd(func(*FastSpan) { ended2++ }))
	_, s2 := tr2.Start(context.Background(), "op", trace.SpanKindInternal)
	s2.End()
	if ended2 != 0 {
		t.Errorf("OnEnd on unsampled tracer called %d times, want 0", ended2)
	}
}

func TestFastSpanDroppedAttributes(t *testing.T) {
	tr := NewFastTracer("unit")
	_, s := tr.Start(context.Background(), "op", trace.SpanKindInternal)
	for i := 0; i < maxInlineAttrs+5; i++ {
		s.SetInt("k", int64(i))
	}
	if s.DroppedAttributes() != 5 {
		t.Errorf("dropped = %d, want 5", s.DroppedAttributes())
	}
	s.End()
}

func TestFastSpanPoolResetNoLeak(t *testing.T) {
	tr := NewFastTracer("unit")
	_, s1 := tr.Start(context.Background(), "first", trace.SpanKindInternal)
	s1.SetString("secret", "value")
	s1.End() // returns to pool

	// The next span very likely reuses the same struct; it must be clean.
	_, s2 := tr.Start(context.Background(), "second", trace.SpanKindInternal)
	var leaked bool
	s2.Attributes(func(FastAttr) { leaked = true })
	if leaked {
		t.Error("recycled span leaked attributes from previous use")
	}
	if s2.Name() != "second" {
		t.Errorf("name = %q, want second", s2.Name())
	}
	s2.End()
}

func TestFastSpanConcurrent(t *testing.T) {
	tr := NewFastTracer("unit")
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_, s := tr.Start(context.Background(), "op", trace.SpanKindInternal)
				s.SetInt("i", int64(i))
				if !s.SpanContext().TraceID().IsValid() {
					t.Error("invalid trace ID under concurrency")
				}
				s.End()
			}
		}()
	}
	wg.Wait()
}
