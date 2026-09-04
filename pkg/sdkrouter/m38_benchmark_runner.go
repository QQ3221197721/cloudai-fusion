package sdkrouter

import (
	"context"
	"testing"
	"time"
)

// This file contains M38 FLIP benchmarks vs Documented Industry Overhead
//
// CLAIMS TO VERIFY:
//   - Zero-allocation template rendering: <10ns/op (vs LangChain-JS ~50B/op)
//   - O(1) provider routing: <5ns (vs Semantic-Kernel ~80μs reflection)
//   - Complete request path: <100ns (vs AWS Bedrock ~21μs marshal+auth)

func Benchmark_Template_ZeroAlloc(b *testing.B) {
	engine := NewTemplateEngine()
	template := "Hello @name! You have @count messages."
	values := map[string]string{
		"name":  "John Doe",
		"count": "150",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := engine.Render(template, values)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func Benchmark_Router_O1(b *testing.B) {
	router := NewProviderRouter(&mockProvider{name: "fallback"})
	provider := &mockProvider{name: "claude"}
	router.Register("anthropic.claude-v2", provider)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = router.Select("anthropic.claude-v2")
	}
}

func Benchmark_Proxy_Complete(b *testing.B) {
	proxy := NewSimplePromptProxy("https://api.sdkrouter.com")
	req := &PromptRequest{
		ModelID:      "anthropic.claude-v2",
		UserPrompt:   "Explain quantum computing",
		MaxTokens:    500,
		Temperature:  0.7,
		Variables: map[string]string{"complexity": "beginner"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := proxy.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		_ = resp.Content
	}
}

func Benchmark_Mock_Latency(b *testing.B) {
	proxy := NewSimplePromptProxy("https://api.mock.com")
	req := &PromptRequest{
		ModelID: "claude-v2",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := proxy.Complete(context.Background(), req)
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

// Manual timing verification
func Test_TemplatePerformance(t *testing.T) {
	engine := NewTemplateEngine()
	template := "Test @variable"
	values := map[string]string{"variable": "value"}

	start := getNanoTime()
	for i := 0; i < 10000; i++ {
		result, _ := engine.Render(template, values)
		_ = result
	}
	elapsed := getNanoTime() - start
	
	t.Logf("Template rendering (10K ops): %d ns/op (%.2f ns/op)", 
		elapsed/10000, float64(elapsed)/10000)
}

// Helper functions
func getNanoTime() int64 {
	return int64(time.Now().UnixNano())
}
