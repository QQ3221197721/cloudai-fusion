//go:build bench_only

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

func main() {
	fmt.Println("CloudAI Fusion M1 vs 2026 Competitors Benchmark")
	fmt.Println("=" + string(make([]byte, 80)))
	
	m1Reg := capability.NewAtomicRegistryV2(runmode.Simulation)
	
	// Populate data
	for i := 0; i < 100; i++ {
		m1Reg.Report("db"+string(rune(i)), "test-driver", capability.ModeReal, "")
	}
	
	start := time.Now()
	calls := 100000
	
	for i := 0; i < calls; i++ {
		_ = m1Reg.GetAllCapabilities()
	}
	
	elapsed := time.Since(start)
	nsPerOp := elapsed.Nanoseconds() / int64(calls)
	
	fmt.Printf("\nCloudAI Fusion M1 Atomic Registry V2\n")
	fmt.Printf("Total operations: %d\n", calls)
	fmt.Printf("Total time: %v\n", elapsed)
	fmt.Printf("Average per op: %d ns/op\n", nsPerOp)
	fmt.Printf("\nEstimated throughput: %.2f ops/sec\n", float64(calls)/elapsed.Seconds())
	
	os.Exit(0)
}
