package observability

import (
	"context"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// W3C Trace Context Tests
// ============================================================================

func TestExtract_ValidTraceParent(t *testing.T) {
	carrier := map[string]string{
		"traceparent": "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01",
	}

	sc, err := Extract(carrier)
	require.NoError(t, err)
	assert.Equal(t, "00", sc.Version)
	assert.Equal(t, "0af7651916cd43dd8448eb211c803119", sc.TraceID)
	assert.Equal(t, "b7ad6b7169203331", sc.SpanID)
	assert.Equal(t, "01", sc.Flags)
	assert.True(t, sc.Valid())
}

func TestExtract_EmptyHeader(t *testing.T) {
	carrier := map[string]string{}

	sc, err := Extract(carrier)
	require.NoError(t, err)
	assert.False(t, sc.Valid())
	assert.Empty(t, sc.String())
}

func TestParseTraceParent_InvalidFormat(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantErr string
	}{
		{"wrong_parts", "00-abc-def", ErrInvalidTraceContext.Error()},
		{"wrong_version", "01-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01", ErrInvalidTraceContext.Error()},
		{"short_traceid", "00-abcdef-b7ad6b7169203331-01", ErrInvalidTraceContext.Error()},
		{"long_traceid", "00-0af7651916cd43dd8448eb211c803119ff-b7ad6b7169203331-01", ErrInvalidTraceContext.Error()},
		{"short_spanid", "00-0af7651916cd43dd8448eb211c803119-abc-01", ErrInvalidTraceContext.Error()},
		{"long_spanid", "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331abcd-01", ErrInvalidTraceContext.Error()},
		{"invalid_hex_trace", "00-xyz7651916cd43dd8448eb211c803119-b7ad6b7169203331-01", ErrInvalidTraceContext.Error()},
		{"short_flags", "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-0", ErrInvalidTraceContext.Error()},
		{"flags_with_letter_g", "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-g1", ErrInvalidTraceContext.Error()},
		{"too_many_parts", "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01-extra", ErrInvalidTraceContext.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTraceParent(tt.header)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestInject_WithContext(t *testing.T) {
	ctx := context.Background()

	sc := SpanContext{
		Version: "00",
		TraceID: "0af7651916cd43dd8448eb211c803119",
		SpanID:  "b7ad6b7169203331",
		Flags:   "01",
	}
	ctx = sc.WithValue(ctx)

	carrier := make(map[string]string)
	Inject(ctx, carrier)

	require.Contains(t, carrier, "traceparent")
	expected := "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01"
	assert.Equal(t, expected, carrier["traceparent"])
}

func TestInject_NoContext(t *testing.T) {
	ctx := context.Background()
	carrier := make(map[string]string)

	Inject(ctx, carrier)
	assert.Empty(t, carrier["traceparent"])
}

func TestChildOf(t *testing.T) {
	parent := SpanContext{
		Version: "00",
		TraceID: "0af7651916cd43dd8448eb211c803119",
		SpanID:  "b7ad6b7169203331",
		Flags:   "01",
	}

	child := parent.ChildOf()

	assert.Equal(t, parent.Version, child.Version)
	assert.Equal(t, parent.TraceID, child.TraceID)
	assert.NotEqual(t, parent.SpanID, child.SpanID)
	assert.Equal(t, parent.SpanID, child.ParentID)
	assert.True(t, child.Valid())
}

func TestFromContext(t *testing.T) {
	ctx := context.Background()
	sc := SpanContext{
		Version: "00",
		TraceID: "0af7651916cd43dd8448eb211c803119",
		SpanID:  "b7ad6b7169203331",
		Flags:   "01",
	}
	ctx = sc.WithValue(ctx)

	extracted, ok := FromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, sc, extracted)

	emptyCtx := context.Background()
	_, ok = FromContext(emptyCtx)
	assert.False(t, ok)
}

// ============================================================================
// Span Tests
// ============================================================================

func TestSpan_StartAndEnd(t *testing.T) {
	span := &Span{
		Name: "test-operation",
	}

	span.Start()
	time.Sleep(10 * time.Millisecond)
	span.End()

	assert.Greater(t, span.Duration, 10*time.Millisecond)
	assert.False(t, span.StartTime.IsZero())
	assert.False(t, span.EndTime.IsZero())
}

func TestSpan_SetAttribute(t *testing.T) {
	span := &Span{Name: "test"}

	span.SetAttribute("key1", "value1")
	span.SetAttribute("key2", 42)
	span.SetAttribute("key3", true)

	assert.Equal(t, "value1", span.Attributes["key1"])
	assert.Equal(t, 42, span.Attributes["key2"])
	assert.Equal(t, true, span.Attributes["key3"])
}

func TestSpan_SetError(t *testing.T) {
	span := &Span{
		Name:     "test",
		Status:   StatusOk,
		ErrorMsg: "",
	}

	span.SetError("something went wrong")

	assert.Equal(t, StatusError, span.Status)
	assert.Equal(t, "something went wrong", span.ErrorMsg)
}

func TestSpan_Clone(t *testing.T) {
	original := &Span{
		SpanID:   "span123",
		TraceID:  "trace123",
		Name:     "test",
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
		Duration:  time.Hour,
		Status:    StatusError,
		ErrorMsg:  "error",
	}
	original.SetAttribute("k", "v")
	original.AddChild("child1")

	cloned := original.Clone()

	assert.Equal(t, original.SpanID, cloned.SpanID)
	assert.Equal(t, original.TraceID, cloned.TraceID)
	assert.Equal(t, original.Name, cloned.Name)
	assert.Equal(t, original.Status, cloned.Status)
	assert.Equal(t, original.ErrorMsg, cloned.ErrorMsg)
	assert.Equal(t, "v", cloned.Attributes["k"])
	assert.Contains(t, cloned.ChildSpans, "child1")

	// Clone should be independent
	cloned.SetAttribute("new_key", "new_value")
	assert.Nil(t, original.Attributes["new_key"])
}

// ============================================================================
// Sampler Tests
// ============================================================================

func TestHeadBasedSampler_AlwaysSamplesAtHighRate(t *testing.T) {
	sampler := NewHeadBasedSampler(1.0) // 100% rate

	for i := 0; i < 100; i++ {
		result := sampler.ShouldSample("trace00000000000000000000000000000001", "span", true)
		assert.True(t, result.ShouldSample)
		assert.Equal(t, "head_prob", result.SampledBy)
	}
}

func TestHeadBasedSampler_DeterministicSampling(t *testing.T) {
	// With a 50%% rate and varied trace IDs, we should see roughly half sampled
	sampler := NewHeadBasedSampler(0.5)

	var sampledCount int
	for i := 0; i < 1000; i++ {
		result := sampler.ShouldSample(fmt.Sprintf("traceid-%016x0000000000000000", i), "test", true)
		if result.ShouldSample {
			sampledCount++
		}
	}

	// Should sample roughly half (within statistical margin of error)
	lowerBound := 400 // Allow for variance
	upperBound := 600
	assert.GreaterOrEqual(t, sampledCount, lowerBound, "should sample at least ~50%%")
	assert.LessOrEqual(t, sampledCount, upperBound, "should sample no more than ~50%%")
}

func TestHeadBasedSampler_InheritedNonRoot(t *testing.T) {
	sampler := NewHeadBasedSampler(0.01)

	// Non-root spans always return ShouldSample=true unless root rejected them
	result := sampler.ShouldSample("trace123", "child-span", false)
	assert.True(t, result.ShouldSample)
	assert.Equal(t, "inherited from root", result.Reason)
}

func TestHeadBasedSampler_Stats(t *testing.T) {
	sampler := NewHeadBasedSampler(1.0) // 100% for easier testing

	for i := 0; i < 100; i++ {
		sampler.ShouldSample(hex.EncodeToString(make([]byte, 16)), "span", true)
	}

	count, sampled := sampler.Stats()
	assert.Equal(t, 100, count)
	assert.Equal(t, 100, sampled)
}

func TestCompositeSampler_ORLogic(t *testing.T) {
	sampler1 := NewHeadBasedSampler(0.0)  // Never samples
	sampler2 := NewHeadBasedSampler(1.0)  // Always samples
	composite := NewCompositeSampler(sampler1, sampler2)

	result := composite.ShouldSample("trace123", "test", true)
	assert.True(t, result.ShouldSample)
}

func TestForcedSampler_AlwaysSamples(t *testing.T) {
	sampler := &ForcedSampler{}

	result := sampler.ShouldSample("trace123", "test", true)
	assert.True(t, result.ShouldSample)
	assert.Equal(t, "forced", result.SampledBy)
}

// ============================================================================
// SpanStorage Tests
// ============================================================================

func TestSpanStorage_RecordAndRetrieve(t *testing.T) {
	storage := NewSpanStorage(100)

	span := &Span{
		SpanID:   "span123",
		TraceID:  "trace123",
		Name:     "test",
		StartTime: time.Now().Add(-time.Hour),
		EndTime:   time.Now(),
	}

	storage.Record(span)

	retrieved, ok := storage.GetSpan("span123")
	assert.True(t, ok)
	assert.Equal(t, "test", retrieved.Name)
}

func TestSpanStorage_GetTraces(t *testing.T) {
	storage := NewSpanStorage(100)

	spans := []*Span{
		{SpanID: "span1", TraceID: "trace1"},
		{SpanID: "span2", TraceID: "trace1"},
		{SpanID: "span3", TraceID: "trace2"},
	}

	for _, s := range spans {
		storage.Record(s)
	}

	trace1 := storage.GetTraces("trace1")
	assert.Len(t, trace1, 2)

	trace2 := storage.GetTraces("trace2")
	assert.Len(t, trace2, 1)
}

func TestSpanStorage_Eviction(t *testing.T) {
	storage := NewSpanStorage(3) // Max 3 spans

	storage.Record(&Span{SpanID: "s1"})
	storage.Record(&Span{SpanID: "s2"})
	storage.Record(&Span{SpanID: "s3"})
	storage.Record(&Span{SpanID: "s4"}) // This should evict one

	assert.Equal(t, 3, storage.CountSpans())
}

func TestSpanStorage_ListAllTraces(t *testing.T) {
	storage := NewSpanStorage(100)

	storage.Record(&Span{SpanID: "s1", TraceID: "t1"})
	storage.Record(&Span{SpanID: "s2", TraceID: "t2"})
	storage.Record(&Span{SpanID: "s3", TraceID: "t1"}) // Same trace as s1

	traces := storage.ListAllTraces()
	assert.Contains(t, traces, "t1")
	assert.Contains(t, traces, "t2")
	assert.Len(t, traces, 2)
}

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestSplitByHyphen(t *testing.T) {
	tests := []struct {
		input  string
		expect []string
	}{
		{"00-abc-def-gh", []string{"00", "abc", "def", "gh"}},
		{"single", []string{"single"}},
		{"no-separators", []string{"no", "separators"}},
		{"", []string{""}},
	}

	for _, tt := range tests {
		result := splitByHyphen(tt.input)
		assert.Equal(t, tt.expect, result)
	}
}

func TestIsValidHex(t *testing.T) {
	assert.True(t, isValidHex("0123456789abcdef", 16))
	assert.True(t, isValidHex("ABCDEF0123456789", 16))
	assert.False(t, isValidHex("0123456789abcdefg", 16)) // Invalid char 'g'
	assert.False(t, isValidHex("0123456789abcde", 16))  // Too short
	assert.False(t, isValidHex("0123456789abcdef0", 16)) // Too long
}

// ============================================================================
// Round-trip Consistency Test
// ============================================================================

func TestTraceParent_RoundTrip(t *testing.T) {
	// Create a sample traceparent header
	original := "00-0af7651916cd43dd8448eb211c803119-b7ad6b7169203331-01"

	// Parse it
	parsed, err := ParseTraceParent(original)
	require.NoError(t, err)

	// Convert back to string
	reparsed := parsed.String()

	assert.Equal(t, original, reparsed)
}

// ============================================================================
// Integration Test: End-to-End Trace Flow
// ============================================================================

func TestTraceFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode")
	}

	// Simulate a distributed trace flow
	rootContext := context.Background()

	// Step 1: Generate root span context
	sampler := NewHeadBasedSampler(1.0) // 100% for testing
	sampleResult := sampler.ShouldSample("deadbeefcafebabe1234567890abcdef", "root-operation", true)
	assert.True(t, sampleResult.ShouldSample)

	rootContext = SpanContext{
		Version: "00",
		TraceID: "deadbeefcafebabe1234567890abcdef",
		SpanID:  "1234567890abcdef",
	}.WithValue(rootContext)

	// Step 2: Extract from incoming request
	requestHeaders := map[string]string{
		"traceparent": "00-deadbeefcafebabe1234567890abcdef-1234567890abcdef-01",
	}

	extracted, err := Extract(requestHeaders)
	require.NoError(t, err)
	assert.True(t, extracted.Valid())

	// Step 3: Create child span
	childContext := extracted.ChildOf()
	assert.Equal(t, extracted.TraceID, childContext.TraceID)
	assert.Equal(t, extracted.SpanID, childContext.ParentID)
	assert.NotEqual(t, extracted.SpanID, childContext.SpanID)

	// Step 4: Inject into outgoing request
	responseHeaders := make(map[string]string)
	Inject(childContext.WithValue(context.Background()), responseHeaders)
	assert.Contains(t, responseHeaders, "traceparent")
	assert.Equal(t, childContext.String(), responseHeaders["traceparent"])

	// Step 5: Verify round-trip consistency
	redone, err := ParseTraceParent(responseHeaders["traceparent"])
	require.NoError(t, err)
	assert.Equal(t, childContext.SpanID, redone.SpanID)
}
