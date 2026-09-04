// +build ignore

// Temporary helper to remove the orphaned _fixed.go file
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	orphan := filepath.Join("pkg", "scheduler", "deep_rl_optimizer_fixed.go")
	if _, err := os.Stat(orphan); err == nil {
		fmt.Printf("Deleting orphan: %s\n", orphan)
		if err := os.Remove(orphan); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to delete: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("SUCCESS: Orphan removed")
	} else if os.IsNotExist(err) {
		fmt.Println("Orphan already absent - good!")
	} else {
		fmt.Fprintf(os.Stderr, "Unexpected error: %v\n", err)
		os.Exit(1)
	}
}
