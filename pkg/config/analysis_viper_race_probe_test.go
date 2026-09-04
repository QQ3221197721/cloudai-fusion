//go:build viperrace

package config

// analysis_viper_race_probe_test.go is an OPT-IN probe that demonstrates, at
// runtime, that spf13/viper v1.18.2 is NOT safe for concurrent read+write.
//
// It is guarded by the `viperrace` build tag so it is EXCLUDED from the normal
// `go test ./...` suite (it intentionally provokes a data race / concurrent map
// access that would otherwise destabilise the package test run). Run it on
// demand to reproduce the finding:
//
//	go test ./pkg/config -tags viperrace -race -run TestViperConcurrentReadWriteIsRacy -v
//
// Expected result: the Go race detector reports a data race between
// viper.(*Viper).Set (writes v.override map) and viper.(*Viper).Get, and/or the
// runtime aborts with "fatal error: concurrent map read and map write". This is
// the runtime confirmation of the static finding that viper.go declares NO
// sync.RWMutex and guards NONE of Get/Set/MergeConfigMap.
//
// The CRDT ConfigState, by contrast, guards Set/Merge/Get with sync.RWMutex and
// publishes to a lock-free atomic.Pointer HotStore, so the equivalent workload
// is race-clean (proven by the same run against ConfigState below).

import (
	"fmt"
	"sync"
	"testing"

	"github.com/spf13/viper"
)

// TestViperConcurrentReadWriteIsRacy provokes the viper data race. Under -race
// this test is EXPECTED to fail / report a race; that failure IS the evidence.
func TestViperConcurrentReadWriteIsRacy(t *testing.T) {
	v := viper.New()
	v.Set("k", "seed")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writers mutate the unguarded override map.
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					v.Set("k", fmt.Sprintf("w%d-%d", id, i))
					i++
				}
			}
		}(w)
	}

	// Readers touch the same map concurrently.
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = v.GetString("k")
				}
			}
		}()
	}

	// Let the race window run briefly then stop.
	for i := 0; i < 100000; i++ {
		_ = v.GetString("k")
	}
	close(stop)
	wg.Wait()
	t.Log("if this line is reached under -race without a report, re-run with more iterations")
}

// TestConfigStateConcurrentReadWriteIsSafe is the control: the same workload on
// the CRDT ConfigState is race-clean because Set/Get take the RWMutex.
func TestConfigStateConcurrentReadWriteIsSafe(t *testing.T) {
	cs := NewConfigState("race-control")
	cs.Set("k", "seed")

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			i := 0
			for {
				select {
				case <-stop:
					return
				default:
					cs.Set("k", fmt.Sprintf("w%d-%d", id, i))
					i++
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = cs.Get("k")
				}
			}
		}()
	}

	for i := 0; i < 100000; i++ {
		_, _ = cs.Get("k")
	}
	close(stop)
	wg.Wait()
	t.Log("ConfigState concurrent read+write completed race-clean")
}
