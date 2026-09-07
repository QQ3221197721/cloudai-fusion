//go:build ignore

package scheduler

import (
	"os"
	"path/filepath"
)

func main() {
	file := "dasp_vs_naive_bench_test.go"
	data, err := os.ReadFile(file)
	if err != nil {
		panic(err)
	}
	
	cleaned := []byte{}
	for _, b := range data {
		if b < 128 {
			cleaned = append(cleaned, b)
		} else if b >= 194 && b <= 223 && len(cleaned) > 0 {
			// Skip invalid UTF-8 continuation bytes
			continue
		} else if b >= 128 {
			// Replace multi-byte characters with ASCII fallback
			continue
		} else {
			cleaned = append(cleaned, b)
		}
	}
	
	os.WriteFile(filepath.Join("cloudai-fusion", "pkg", "scheduler", file), cleaned, 0644)
	println("Fixed encoding")
}
