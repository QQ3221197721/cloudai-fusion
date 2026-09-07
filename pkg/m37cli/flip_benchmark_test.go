package m37cli_test

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/m37cli"
)

// TestCobraDispatchLatency measures baseline cobra subcommand dispatch overhead
func TestCobraDispatchLatency(t *testing.T) {
	start := time.Now()
	for i := 0; i < 1000; i++ {
		cmd := m37cli.NewCommand()
		_ = cmd.ExecuteContext(context.Background())
	}
	cobraOverhead := time.Since(start) / 1000

	t.Logf("Cobra dispatch latency: %v per command", cobraOverhead)
}

// TestPreParsedDispatchLatency measures our pre-parsed registry overhead
func TestPreParsedDispatchLatency(t *testing.T) {
	start := time.Now()
	for i := 0; i < 1000; i++ {
		cmd := m37cli.NewPreParsedCommand()
		_ = cmd.ExecuteContext(context.Background())
	}
	preParsedOverhead := time.Since(start) / 1000

	t.Logf("Pre-parsed dispatch latency: %v per command", preParsedOverhead)
}

// BenchmarkHeadToHead compares cobra vs our implementation
func BenchmarkHeadToHead(b *testing.B) {
	b.Run("Cobra", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cmd := m37cli.NewCommand()
			_ = cmd.ExecuteContext(context.Background())
		}
	})

	b.Run("PreParsed", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cmd := m37cli.NewPreParsedCommand()
			_ = cmd.ExecuteContext(context.Background())
		}
	})
}
