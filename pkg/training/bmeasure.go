// +build ignore
//go:build ignore
// This file measures benchmark results without relying on go test -bench output capturing issues on Windows PowerShell.

package training

import (
	"fmt"
	"time"
)

func Main() {
	spec := validSpecForBenchmark("measure")
	s := bigScheduler(nil) // nil T works because we use frozen clock and no logging

	// JobSubmissionLatency
	var start time.Time
	for i := 0; i < 100; i++ {
		start = time.Now()
		s.Submit(spec)
		_ = time.Since(start).Nanoseconds()
	}

	fmt.Println("Module 14 Gang Scheduler Benchmarks")
	fmt.Println("=" + "====================================================")
	fmt.Println("")

	// We'll just run each operation once with timing to get representative values
	batchSize := 1000
	
	// Submit latency
	s2, _ := NewGangScheduler(ClusterCapacity{GPUs: 64, CPUCores: 512, MemoryGB: 2048}, testSigner(&testingT{}))
	s2.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	
	start = time.Now()
	for i := 0; i < batchSize; i++ {
		s2.Submit(spec)
	}
	submitDur := time.Since(start)
	fmt.Printf("Job Submission (%d ops): %s/ops (~%.0f ns/op)\n", batchSize, submitDur, float64(submitDur.Nanoseconds())/float64(batchSize))
	
	// Admission decisions
	s3, _ := NewGangScheduler(ClusterCapacity{GPUs: 7, CPUCores: 512, MemoryGB: 1024}, testSigner(&testingT{}))
	s3.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	
	start = time.Now()
	for i := 0; i < batchSize; i++ {
		_ = s3.TryAdmit(spec)
	}
	admitDur := time.Since(start)
	rate := float64(batchSize) / admitDur.Seconds()
	fmt.Printf("Admission Decisions (%d ops): %s → %.0f ops/sec\n", batchSize, admitDur, rate)
	
	// Happy path end-to-end
	s4, _ := NewGangScheduler(ClusterCapacity{GPUs: 64, CPUCores: 512, MemoryGB: 2048}, testSigner(&testingT{}))
	s4.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	
	start = time.Now()
	for i := 0; i < batchSize; i++ {
		job, _ := s4.Submit(spec)
		s4.Admit(job.ID)
		s4.Start(job.ID)
		s4.Succeed(job.ID)
	}
	e2eDur := time.Since(start)
	fmt.Printf("End-to-End Happy Path (%d ops): %s → %.0f ops/sec\n", batchSize, e2eDur, float64(batchSize)/e2eDur.Seconds())
}

type testingT struct{}

func (t *testingT) Helper()           {}
func (t *testingT) Log(args ...interface{}) {}
func (t *testingT) Errorf(format string, args ...interface{}) {}
