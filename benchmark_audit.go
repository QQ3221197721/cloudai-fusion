package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func main() {
	pkgs := []string{
		"auth", "billing", "cache", "capability", "cluster", "common", "controller",
		"controlplane", "cost", "delivery", "deploy", "detect", "devsecops", "disaster",
		"edge", "edgeautonomy", "elasticpool", "election", "enterprise", "evidence",
		"experiment", "exploit", "fabric", "feature", "fed", "finops", "gitops", "ha",
		"hotswap", "hunt", "inference", "intel", "k8s", "logging", "manifest", "mesh",
		"messaging", "metrics", "middleware", "migrate", "modelmonitor", "modelregistry",
		"monitor", "multicluster", "observability", "pipeline", "plugin", "provenance",
		"redteam", "resilience", "resources", "rpcserver", "runmode", "sandbox", "scanners",
		"sdk", "security", "store", "support", "tee", "tenant", "tenants", "testutil",
		"tracing", "tsdb", "validation", "version", "wasm", "websocket", "wellreadiness",
		"wellrouter", "workload", "zkp",
	}

	fmt.Println("=== Benchmark Audit Report ===")
	hasBench := []string{}
	noBench := []string{}
	noTestFiles := []string{}

	for _, pkg := range pkgs {
		cmd := exec.Command("go", "test", fmt.Sprintf("./pkg/%s/", pkg), "-bench=.", "-benchmem", "-count=1", "-run=", "^$")
		output, err := cmd.CombinedOutput()
		outStr := string(output)

		if strings.Contains(outStr, "Benchmark") {
			hasBench = append(hasBench, pkg)
			fmt.Printf("[✓] %s - HAS BENCHMARK\n", pkg)
		} else if strings.Contains(outStr, "[no test files]") {
			noTestFiles = append(noTestFiles, pkg)
			fmt.Printf("[?] %s - NO TEST FILES\n", pkg)
		} else if err != nil || !strings.Contains(outStr, "PASS") {
			// Check if it's truly no benchmark or an error
			if !strings.Contains(outStr, "ok") {
				fmt.Printf("[✗] %s - ERROR: %s\n", pkg, outStr)
			} else {
				noBench = append(noBench, pkg)
				fmt.Printf("[ ] %s - NO BENCHMARK\n", pkg)
			}
		} else {
			noBench = append(noBench, pkg)
			fmt.Printf("[ ] %s - NO BENCHMARK\n", pkg)
		}
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Has Benchmark: %d (%v)\n", len(hasBench), hasBench)
	fmt.Printf("No Benchmark: %d (%v)\n", len(noBench), noBench)
	fmt.Printf("No Test Files: %d (%v)\n", len(noTestFiles), noTestFiles)
}
