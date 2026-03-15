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
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url> [timeout_seconds]")
		os.Exit(2)
	}

	url := os.Args[1]
	timeout := 5 * time.Second

	if len(os.Args) >= 3 {
		if secs, err := strconv.Atoi(os.Args[2]); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}

	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(url) // nolint:gosec // URL from CLI arg, intentional
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		fmt.Fprintf(os.Stderr, "healthcheck failed: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}

	os.Exit(0)
}
