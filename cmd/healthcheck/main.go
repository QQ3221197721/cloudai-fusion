// Package main provides a minimal healthcheck binary for distroless containers.
// Since distroless images have no shell, curl, or wget, this tiny static binary
// replaces the `curl -f http://localhost:PORT/healthz || exit 1` pattern.
//
// Usage (in Dockerfile HEALTHCHECK):
//
//	HEALTHCHECK CMD ["/usr/local/bin/healthcheck", "http://localhost:8080/healthz"]
//
// Build: CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o healthcheck ./cmd/healthcheck
// Binary size: ~2MB (stripped static)
package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run executes the healthcheck and returns an exit code.
// Extracted from main() for testability.
func run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url> [timeout_seconds]")
		return 2
	}

	url := args[0]
	timeout := 5 * time.Second

	if len(args) >= 2 {
		if secs, err := strconv.Atoi(args[1]); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(url) // nolint:gosec // URL from CLI arg, intentional
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "healthcheck failed: HTTP %d\n", resp.StatusCode)
		return 1
	}

	return 0
}
