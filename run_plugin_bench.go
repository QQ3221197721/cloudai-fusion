//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	cmd := exec.Command("go", "test", "./pkg/plugin/", "-bench=Benchmark(M4|gopluginDirect)", "-benchmem", "-count=6", "-benchtime=2s")
	cmd.Dir = "d:\\IdeaProjects\\untitled\\cloudai-fusion"
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Test failed: %v\n%s\n", err, string(output))
		os.Exit(1)
	}
	fmt.Println(string(output))
}
