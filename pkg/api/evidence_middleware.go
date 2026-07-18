// Package api - evidence_middleware.go emits a signed receipt for every
// STATE-CHANGING control-plane request. This is the general form of the platform
// promise "did this action really run, or was it degraded/simulated?": each
// mutating call (POST/PUT/DELETE/PATCH) produces a hash-chained receipt recording
// who did what, the outcome, and — via the capability snapshot embedded by the
// ledger — which backends were real vs simulated at that moment.
package api

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceMiddlewareConfig configures which mutating requests are receipted.
type EvidenceMiddlewareConfig struct {
	Ledger *evidence.Ledger
	// SkipPathPrefixes are never receipted. Auth endpoints are skipped by default
	// so credentials are never captured, and evidence-read paths to avoid noise.
	SkipPathPrefixes []string
}

// defaultEvidenceSkips are paths excluded from action receipts.
var defaultEvidenceSkips = []string{
	"/api/v1/auth/", // never receipt credential-bearing requests
	"/api/v1/evidence",
	"/healthz", "/readyz", "/metrics",
}

// EvidenceMiddleware returns a Gin middleware that records a signed receipt after
// each mutating request. It runs the handler first, then emits the receipt with
// the resulting status, so the receipt reflects the true outcome. Emission never
// blocks or fails the request — a ledger error is logged by the ledger, not
// surfaced to the client.
func EvidenceMiddleware(cfg EvidenceMiddlewareConfig) gin.HandlerFunc {
	skips := append(defaultEvidenceSkips, cfg.SkipPathPrefixes...)
	return func(c *gin.Context) {
		if cfg.Ledger == nil || !isMutating(c.Request.Method) || hasAnyPrefix(c.Request.URL.Path, skips) {
			c.Next()
			return
		}

		c.Next() // execute the handler, then receipt the outcome

		actor := actorFromContext(c)
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		// Emit best-effort; the request has already completed. The ledger embeds
		// the per-action capability snapshot (real-vs-simulated) automatically.
		_, _ = cfg.Ledger.Record(c.Request.Context(), evidence.RecordInput{
			Actor:   actor,
			Action:  "api." + c.Request.Method + " " + route,
			Subject: subjectFromContext(c),
			Input: map[string]any{
				"method": c.Request.Method,
				"path":   c.Request.URL.Path,
				"query":  c.Request.URL.RawQuery,
			},
			Output: map[string]any{
				"status": c.Writer.Status(),
			},
		})
	}
}

// isMutating reports whether the method changes server state.
func isMutating(method string) bool {
	switch method {
	case "POST", "PUT", "DELETE", "PATCH":
		return true
	default:
		return false
	}
}

// hasAnyPrefix reports whether path starts with any of the given prefixes.
func hasAnyPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// actorFromContext extracts the authenticated identity set by auth.AuthMiddleware,
// falling back to "anonymous" for unauthenticated (or pre-auth) requests.
func actorFromContext(c *gin.Context) string {
	if u := c.GetString("username"); u != "" {
		return u
	}
	if id := c.GetString("user_id"); id != "" {
		return id
	}
	return "anonymous"
}

// subjectFromContext picks the most specific resource identifier available: a
// path parameter (:id, :workloadID, ...) if present, else the route path.
func subjectFromContext(c *gin.Context) string {
	for _, key := range []string{"id", "workloadID", "name", "key"} {
		if v := c.Param(key); v != "" {
			return v
		}
	}
	if route := c.FullPath(); route != "" {
		return route
	}
	return c.Request.URL.Path
}
