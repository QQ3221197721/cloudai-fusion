// +build ignore

package edge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// Simplified logger for standalone test - duplicate of newTestLogger from modules_21_23_stubs_complete_test.go
func newStandaloneTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

func BenchmarkNodeRegistration_Simple(b *testing.B) {
	cfg := DefaultNodeManagerConfig()
	logger := newStandaloneTestLogger()
	mgr := NewNodeManager(cfg, logger)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := mgr.Provision(ctx, fmt.Sprintf("simple-node-%d", i), "test-region", HardwareSpec{
			CPUCores: 8, MemoryGB: 32, GPUType: "nvidia-jetson-orin", GPUCount: 1, GPUMemoryGB: 64,
		})
		if err != nil {
			b.Fatalf("Provision failed: %v", err)
		}
	}
}
