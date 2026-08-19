package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("go", "test", "-bench=.", "-benchmem", "-count=3", "./pkg/sdk/")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := buf.String()
	fmt.Println("=== M38 SDK BENCHMARK OUTPUT ===")
	fmt.Println(out)
	if err != nil {
		fmt.Printf("RunError: %v\n", err)
	}
	_ = os.WriteFile("docs/benchmark_result.txt", []byte(out), 0644)
}
