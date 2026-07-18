package redteam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// tools.go is the M1 tool plane: a registry of real security tools, each of which
// reports honestly whether it runs a real binary/endpoint or a reported
// simulation (when the binary is absent). Tool selection is by name/technique;
// the ToolExecutor (executor.go) bridges an authorized Action to a Tool.Invoke and
// records a verifiable redteam.recon receipt (input/output hashes + mode).

// ToolInput is the structured input to a tool invocation.
type ToolInput struct {
	Target string         // authorized target (host/URL/IP)
	Args   map[string]any // optional tool-specific args
}

// ToolOutput is the structured result of a tool invocation.
type ToolOutput struct {
	Raw     []byte          // raw tool output (hashed into evidence, not stored raw)
	Summary string          // bounded human summary
	Finding *Finding        // optional finding parsed from the output
	Mode    capability.Mode // real | simulated for THIS invocation
	Driver  string          // e.g. the binary name
}

// Tool is a single capability (a wrapped real security tool). It reports its
// static mode to the capability registry at registration time.
type Tool interface {
	Name() string
	Techniques() []string // MITRE ATT&CK IDs this tool exercises
	Mode() capability.Mode // real when the binary/endpoint is reachable
	Invoke(ctx context.Context, in ToolInput) (ToolOutput, error)
}

// ToolRegistry holds the available tools and reports each to capability.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool and honestly reports its mode (real vs simulated) to the
// capability registry, e.g. redteam.tool.nmap = real|simulated.
func (r *ToolRegistry) Register(t Tool) {
	r.mu.Lock()
	r.tools[t.Name()] = t
	r.mu.Unlock()
	_ = capability.Report("redteam.tool."+t.Name(), t.Name(), t.Mode(),
		"red-team tool ("+strings.Join(t.Techniques(), ",")+")")
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *ToolRegistry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	return out
}

// sha256Hex returns the hex SHA-256 of b (used to fingerprint tool I/O so the
// ledger records WHAT was run/observed without storing bulky or sensitive raw
// output).
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hashToolInput returns a deterministic fingerprint of a tool input.
func hashToolInput(in ToolInput) string {
	var b strings.Builder
	b.WriteString(in.Target)
	b.WriteByte('\n')
	keys := make([]string, 0, len(in.Args))
	for k := range in.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v\n", k, in.Args[k])
	}
	return sha256Hex([]byte(b.String()))
}

// modeStr maps a capability.Mode to the stable string used in receipts.
func modeStr(m capability.Mode) string {
	if m == capability.ModeReal {
		return "real"
	}
	return "simulated"
}

// bounded truncates s to at most n runes for inclusion in evidence payloads.
func bounded(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}
