package main

import (
	"fmt"
	"os"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/aiops"
)

func main() {
	jsonData, err := aiops.M49ArtifactFreeBenchmark()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== RAW OUTPUT ===")
	fmt.Println(string(jsonData))
}
