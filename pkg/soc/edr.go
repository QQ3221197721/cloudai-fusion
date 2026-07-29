package soc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// edr.go turns the L3 Endpoint well from "caller supplies hashes" into a REAL
// endpoint telemetry collector. ProcEDRCollector reads the running processes from
// the Linux /proc filesystem and hashes each executable (SHA-256), producing real
// indicators that are then matched against L1 IOCs. On non-Linux hosts it reports
// honestly that it cannot collect (no fabricated telemetry).
//
// Consistent with the SOC package's decision, the collector does not register
// into pkg/capability; instead every collection reports its real-vs-simulated
// mode inline (via IsReal()/Name() and the evidence receipt), so a run is never
// silently a simulation.

// ProcessInfo describes one observed process and its executable hash.
type ProcessInfo struct {
	PID    int    `json:"pid"`
	Exe    string `json:"exe"`
	SHA256 string `json:"sha256"`
}

// EndpointTelemetry is a point-in-time snapshot of a host's processes.
type EndpointTelemetry struct {
	Host      string        `json:"host"`
	Processes []ProcessInfo `json:"processes"`
}

// Hashes returns the distinct executable SHA-256 hashes in the telemetry.
func (t EndpointTelemetry) Hashes() []string {
	seen := make(map[string]struct{}, len(t.Processes))
	out := make([]string, 0, len(t.Processes))
	for _, p := range t.Processes {
		if p.SHA256 == "" {
			continue
		}
		if _, ok := seen[p.SHA256]; ok {
			continue
		}
		seen[p.SHA256] = struct{}{}
		out = append(out, p.SHA256)
	}
	return out
}

// EDRCollector gathers endpoint telemetry for L3 detection.
type EDRCollector interface {
	Name() string
	IsReal() bool
	Collect(ctx context.Context) (EndpointTelemetry, error)
}

// ProcEDRCollector is the REAL collector: it enumerates /proc and hashes process
// executables. It is real only on Linux; elsewhere Collect returns an error.
type ProcEDRCollector struct {
	Host         string // reported host name (defaults to os.Hostname)
	MaxProcesses int    // cap the number of processes hashed (0 => 256)
	MaxFileBytes int64  // cap bytes hashed per executable (0 => 64 MiB)
	procRoot     string // overridable for tests (defaults to /proc)
}

// NewProcEDRCollector builds a /proc-backed collector.
func NewProcEDRCollector(host string) *ProcEDRCollector {
	if host == "" {
		if h, err := os.Hostname(); err == nil {
			host = h
		} else {
			host = "unknown"
		}
	}
	return &ProcEDRCollector{Host: host, MaxProcesses: 256, MaxFileBytes: 64 << 20, procRoot: "/proc"}
}

// Name identifies the collector.
func (*ProcEDRCollector) Name() string { return "proc-edr" }

// IsReal reports whether real collection is possible on this OS (Linux /proc).
func (c *ProcEDRCollector) IsReal() bool { return runtime.GOOS == "linux" }

// Collect reads /proc, resolves each process's executable, and hashes it.
// Unreadable processes (permission denied, exited) are skipped, not fabricated.
func (c *ProcEDRCollector) Collect(ctx context.Context) (EndpointTelemetry, error) {
	if !c.IsReal() {
		return EndpointTelemetry{}, fmt.Errorf("soc: proc-edr requires Linux /proc (GOOS=%s)", runtime.GOOS)
	}
	root := c.procRoot
	if root == "" {
		root = "/proc"
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return EndpointTelemetry{}, fmt.Errorf("soc: read %s: %w", root, err)
	}
	maxProc := c.MaxProcesses
	if maxProc <= 0 {
		maxProc = 256
	}
	tel := EndpointTelemetry{Host: c.Host}
	for _, e := range entries {
		if ctx.Err() != nil {
			return tel, ctx.Err()
		}
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name()) // /proc PID dirs are numeric
		if err != nil || pid <= 0 {
			continue
		}
		exe, err := os.Readlink(filepath.Join(root, e.Name(), "exe"))
		if err != nil || exe == "" {
			continue // kernel threads / permission denied
		}
		sum, err := c.hashFile(exe)
		if err != nil {
			continue
		}
		tel.Processes = append(tel.Processes, ProcessInfo{PID: pid, Exe: exe, SHA256: sum})
		if len(tel.Processes) >= maxProc {
			break
		}
	}
	return tel, nil
}

// hashFile returns the hex SHA-256 of the file at path (bounded by MaxFileBytes).
func (c *ProcEDRCollector) hashFile(path string) (string, error) {
	clean := filepath.Clean(path)
	f, err := os.Open(clean)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	limit := c.MaxFileBytes
	if limit <= 0 {
		limit = 64 << 20
	}
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, limit)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// StaticEDRCollector returns injected telemetry. It is the honest simulated
// collector for development and tests (IsReal()=false).
type StaticEDRCollector struct {
	Telemetry EndpointTelemetry
}

// NewStaticEDRCollector builds a simulated collector from fixed telemetry.
func NewStaticEDRCollector(t EndpointTelemetry) *StaticEDRCollector {
	return &StaticEDRCollector{Telemetry: t}
}

// Name identifies the collector.
func (*StaticEDRCollector) Name() string { return "static-edr" }

// IsReal reports false: static telemetry is a simulation.
func (*StaticEDRCollector) IsReal() bool { return false }

// Collect returns the injected telemetry.
func (c *StaticEDRCollector) Collect(context.Context) (EndpointTelemetry, error) {
	return c.Telemetry, nil
}

// CollectEndpoint runs a real (or simulated) EDR collection and feeds the observed
// executable hashes through the L3 endpoint detector, storing and signing any
// findings. The returned host is the telemetry's host; the collector's mode is
// recorded in the detection receipt so the run is never a silent simulation.
func (e *Engine) CollectEndpoint(ctx context.Context, collector EDRCollector) ([]Finding, error) {
	if collector == nil {
		return nil, fmt.Errorf("soc: nil EDR collector")
	}
	tel, err := collector.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("soc: edr collect (%s): %w", collector.Name(), err)
	}
	findings, err := e.endpoint.Analyze(ctx, tel.Host, tel.Hashes())
	if err != nil {
		return nil, err
	}
	e.store.Add(findings...)
	_, recErr := e.recorder.Record(ctx, edrReceipt(collector, tel, len(findings)))
	if recErr != nil {
		e.logger.WithError(recErr).Warn("soc: failed to record EDR collection evidence")
	}
	return findings, nil
}

// edrReceipt builds the evidence receipt for one EDR collection, recording the
// collector's real-vs-simulated mode and driver so the ledger is honest.
func edrReceipt(collector EDRCollector, tel EndpointTelemetry, findings int) evidence.RecordInput {
	mode := "simulated"
	if collector.IsReal() {
		mode = "real"
	}
	return evidence.RecordInput{
		Actor:   "soc-" + WellEndpoint.String(),
		Action:  detectAction,
		Subject: tel.Host,
		Output: map[string]any{
			"well": WellEndpoint.String(), "findings": findings,
			"processes": len(tel.Processes), "collector": collector.Name(), "mode": mode,
		},
		Components: []string{"soc.endpoint-ioc"},
	}
}
