package sdkrouter

import (
	"context"
	"testing"
)

func BenchmarkSimpleProxy_N100(b *testing.B) {
	proxy := NewSimplePromptProxy("https://api.sdkrouter.com")
	req := &PromptRequest{ModelID: "claude-v2", UserPrompt: "test query"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = proxy.Complete(context.Background(), req)
	}
}

// BenchmarkDirectAPI_N100 (legacy - now uses mockProvider)
func BenchmarkMockProvider_N100(b *testing.B) {
	proxy := NewSimplePromptProxy("https://api.mockprovider.com")
	req := &PromptRequest{ModelID: "claude-v2", UserPrompt: "test query"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = proxy.Complete(context.Background(), req)
	}
}
