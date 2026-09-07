//go:build ignore

package training

import (
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
)

func main() {
	spec := training.GangJobSpec{
		Name:       "measure",
		Image:      "pytorch:2.3",
		Replicas:   4,
		Priority:   10,
		MinMembers: 4,
		Resources:  training.ResourceRequest{GPUs: 2, CPUCores: 8, MemoryGB: 32},
		Command:    "torchrun --nproc_per_node=2 train.py",
		Queue:      "research",
	}

	// Build signer deterministically for speed
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	signer, err := training.NewReceiptSignerFromSeed(seed)
	if err != nil {
		panic(err)
	}
	s2, _ := training.NewGangScheduler(training.ClusterCapacity{GPUs: 64, CPUCores: 512, MemoryGB: 2048}, signer)
	s2.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })

	batchSize := 10000

	start := time.Now()
	for i := 0; i < batchSize; i++ {
		s2.Submit(spec)
	}
	submitDur := time.Since(start)
	fmt.Printf("Batch Job Submission (%d ops): %s total �?%.0f ns/op\n", batchSize, submitDur, float64(submitDur.Nanoseconds())/float64(batchSize))
	
	// Admission decisions
	seed2 := make([]byte, 32)
	for i := range seed2 {
		seed2[i] = byte(i + 1)
	}
	signer2, _ := training.NewReceiptSignerFromSeed(seed2)
	s3, _ := training.NewGangScheduler(training.ClusterCapacity{GPUs: 7, CPUCores: 512, MemoryGB: 1024}, signer2)
	s3.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	
	start = time.Now()
	for i := 0; i < batchSize; i++ {
		_ = s3.TryAdmit(spec)
	}
	admitDur := time.Since(start)
	rate := float64(batchSize) / admitDur.Seconds()
	fmt.Printf("Batch Admission Decisions (%d ops): %s total �?%.0f ops/sec\n", batchSize, admitDur, rate)
	
	// Happy path end-to-end
	seed3 := make([]byte, 32)
	for i := range seed3 {
		seed3[i] = byte(i + 1)
	}
	signer3, _ := training.NewReceiptSignerFromSeed(seed3)
	s4, _ := training.NewGangScheduler(training.ClusterCapacity{GPUs: 64, CPUCores: 512, MemoryGB: 2048}, signer3)
	s4.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	
	start = time.Now()
	for i := 0; i < batchSize; i++ {
		job, _ := s4.Submit(spec)
		s4.Admit(job.ID)
		s4.Start(job.ID)
		s4.Succeed(job.ID)
	}
	e2eDur := time.Since(start)
	fmt.Printf("Batch End-to-End Happy Path (%d ops): %s total �?%.0f ops/sec\n", batchSize, e2eDur, float64(batchSize)/e2eDur.Seconds())
}
