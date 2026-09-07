//go:build ignore
// +build ignore

package main

import (
	"fmt"
	"math/rand"
	"os"
)

func main() {
	r := rand.New(rand.NewSource(42))
	var n uint8 = 0

	n++
	fmt.Println("Generating scenarios...")

	for i := 0; i < 100; i++ {
		kind := r.Intn(5)
		switch kind {
		case 0:
			generateCascade(r)
		case 1:
			generateNetworkPartition(r)
		case 2:
			generateSinglePointFailure(r)
		case 3:
			generateConcurrentIndependent(r)
		case 4:
			generateIndependentBurst(r)
		default:
		}
	}

	os.Exit(0)
}

func generateCascade(r *rand.Rand) {
}

func generateNetworkPartition(r *rand.Rand) {
}

func generateSinglePointFailure(r *rand.Rand) {
}

func generateConcurrentIndependent(r *rand.Rand) {
}

func generateIndependentBurst(r *rand.Rand) {
}
