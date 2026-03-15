// Package api provides the RESTful API layer for CloudAI Fusion.
// This file adds /debug/* endpoints for cross-language debugging,
// runtime inspection, and distributed trace correlation.
//
// SECURITY: All debug endpoints are protected by a multi-layer defense:
//   1. Production auto-disable — disabled unless CLOUDAI_DEBUG_ENABLED=true (default: off)
//   2. JWT authentication — requires valid Bearer token (same as /api/v1/*)
//   3. Admin role enforcement — only admin users can access debug endpoints
//   4. IP allowlist — optional CLOUDAI_DEBUG_ALLOWED_IPS for network-level restriction
//   5. Rate limiting — pprof endpoints are rate-limited to prevent DoS
//   6. Audit logging — all debug access is logged with user identity

package api

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/logging"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/middleware"
)

// DebugConfig holds dependencies for the debug API.
type DebugConfig struct {
	Logger      *logging.Logger
	Version     string
	StartedAt   time.Time
	AuthService auth.AuthService // JWT validator for debug auth
}

// RegisterDebugRoutes attaches /debug/* endpoints to the Gin engine.
// Security model (defense in depth):
//   - CLOUDAI_DEBUG_ENABLED must be explicitly set to "true" (safe default: disabled)
//   - All requests require valid JWT + admin role
//   - Optional IP allowlist via CLOUDAI_DEBUG_ALLOWED_IPS
//   - pprof endpoints have aggressive rate limiting (anti-DoS)
func RegisterDebugRoutes(router *gin.Engine, cfg DebugConfig) {
	// Layer 1: Safe default — debug is OFF unless explicitly enabled.
	// This is the inverse of the old behavior (was ON unless set to "false").
	if os.Getenv("CLOUDAI_DEBUG_ENABLED") != "true" {
		return
	}

	debug := router.Group("/debug")

	// Layer 2: JWT authentication — reuse the same auth middleware as /api/v1/*
	if cfg.AuthService != nil {
		debug.Use(cfg.AuthService.AuthMiddleware())
	}

	// Layer 3: Admin role enforcement — only admin can access debug
	debug.Use(debugAdminOnly())

	// Layer 4: IP allowlist (optional, configured via CLOUDAI_DEBUG_ALLOWED_IPS)
	allowedIPs := parseAllowedIPs()
	if len(allowedIPs) > 0 {
		debug.Use(debugIPAllowlist(allowedIPs))
	}

	// Layer 5: Audit logging for all debug access
	debug.Use(debugAuditLog())

	{
		// ---- Runtime Information ----
		debug.GET("/info", handleDebugInfo(cfg))

		// ---- Log Level (dynamic) ----
		debug.GET("/log-level", handleGetLogLevel(cfg))
		debug.PUT("/log-level", handleSetLogLevel(cfg))

		// ---- Cross-Service Health Probe ----
		debug.GET("/services", handleDebugServices())

		// ---- Trace Correlation Test ----
		debug.GET("/tracing", handleDebugTracing())

		// ---- Go pprof (with aggressive rate limiting — Layer 6: anti-DoS) ----
		// pprof/profile can run for 30s consuming CPU; limit to 2 req/min burst 3
		pprofRL := middleware.EndpointRateLimit(0.033, 3) // ~2 req/min
		debug.GET("/pprof/", gin.WrapF(pprof.Index))
		debug.GET("/pprof/cmdline", gin.WrapF(pprof.Cmdline))
		debug.GET("/pprof/profile", pprofRL, gin.WrapF(pprof.Profile))
		debug.GET("/pprof/symbol", gin.WrapF(pprof.Symbol))
		debug.GET("/pprof/trace", pprofRL, gin.WrapF(pprof.Trace))
		debug.GET("/pprof/heap", gin.WrapH(pprof.Handler("heap")))
		debug.GET("/pprof/goroutine", gin.WrapH(pprof.Handler("goroutine")))
		debug.GET("/pprof/allocs", gin.WrapH(pprof.Handler("allocs")))
		debug.GET("/pprof/block", gin.WrapH(pprof.Handler("block")))
		debug.GET("/pprof/mutex", gin.WrapH(pprof.Handler("mutex")))
		debug.GET("/pprof/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))

		// ---- Goroutine Dump (text) ----
		debug.GET("/goroutines", handleGoroutineDump())

		// ---- Memory Stats ----
		debug.GET("/memory", handleMemoryStats())

		// ---- Environment (sanitized) ----
		debug.GET("/env", handleDebugEnv())
	}
}

// ============================================================================
// Debug Security Middleware
// ============================================================================

// debugAdminOnly enforces that only users with the "admin" role can access
// debug endpoints. This runs AFTER JWT authentication, so c.Get("role") is set.
func debugAdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		roleVal, exists := c.Get("role")
		if !exists {
			// No role in context means auth middleware didn't run or token was invalid.
			// Fail closed — deny access.
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "debug endpoints require admin role",
			})
			return
		}
		if auth.Role(roleVal.(string)) != auth.RoleAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "debug endpoints require admin role",
				"role":    roleVal,
			})
			return
		}
		c.Next()
	}
}

// parseAllowedIPs reads CLOUDAI_DEBUG_ALLOWED_IPS (comma-separated IPs/CIDRs)
// and returns the parsed list. Example: "10.0.0.0/8,192.168.1.0/24,127.0.0.1"
func parseAllowedIPs() []string {
	raw := os.Getenv("CLOUDAI_DEBUG_ALLOWED_IPS")
	if raw == "" {
		return nil
	}
	var ips []string
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			ips = append(ips, s)
		}
	}
	return ips
}

// debugIPAllowlist restricts debug access to a set of trusted IPs/CIDRs.
func debugIPAllowlist(allowed []string) gin.HandlerFunc {
	// Pre-parse CIDRs at init time for performance
	nets := make([]*net.IPNet, 0, len(allowed))
	plain := make([]net.IP, 0, len(allowed))
	for _, s := range allowed {
		if strings.Contains(s, "/") {
			_, cidr, err := net.ParseCIDR(s)
			if err == nil {
				nets = append(nets, cidr)
			}
		} else {
			if ip := net.ParseIP(s); ip != nil {
				plain = append(plain, ip)
			}
		}
	}

	return func(c *gin.Context) {
		clientIP := net.ParseIP(c.ClientIP())
		if clientIP == nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "cannot determine client IP",
			})
			return
		}

		// Check plain IPs
		for _, ip := range plain {
			if ip.Equal(clientIP) {
				c.Next()
				return
			}
		}
		// Check CIDRs
		for _, cidr := range nets {
			if cidr.Contains(clientIP) {
				c.Next()
				return
			}
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"code":    403,
			"message": "client IP not in debug allowlist",
			"ip":      c.ClientIP(),
		})
	}
}

// debugAuditLog logs every debug endpoint access with user identity and endpoint.
func debugAuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		username, _ := c.Get("username")
		userID, _ := c.Get("user_id")

		// Use fmt to stderr for audit (always visible, not filtered by log level)
		fmt.Fprintf(os.Stderr,
			"[DEBUG-AUDIT] user=%v uid=%v ip=%s method=%s path=%s status=%d duration=%s\n",
			username, userID, c.ClientIP(), c.Request.Method,
			c.Request.URL.Path, c.Writer.Status(), time.Since(start),
		)
	}
}

// handleDebugInfo returns runtime and build information.
func handleDebugInfo(cfg DebugConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		c.JSON(http.StatusOK, gin.H{
			"service":      "cloudai-apiserver",
			"language":     "go",
			"go_version":   runtime.Version(),
			"version":      cfg.Version,
			"uptime":       time.Since(cfg.StartedAt).String(),
			"started_at":   cfg.StartedAt.Format(time.RFC3339),
			"goroutines":   runtime.NumGoroutine(),
			"num_cpu":      runtime.NumCPU(),
			"gomaxprocs":   runtime.GOMAXPROCS(0),
			"os":           runtime.GOOS,
			"arch":         runtime.GOARCH,
			"memory": gin.H{
				"alloc_mb":       memStats.Alloc / 1024 / 1024,
				"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
				"sys_mb":         memStats.Sys / 1024 / 1024,
				"gc_cycles":      memStats.NumGC,
				"gc_pause_total": fmt.Sprintf("%.2fms", float64(memStats.PauseTotalNs)/1e6),
			},
			"debug_endpoints": []string{
				"/debug/info",
				"/debug/log-level",
				"/debug/services",
				"/debug/tracing",
				"/debug/pprof/",
				"/debug/goroutines",
				"/debug/memory",
				"/debug/env",
			},
		})
	}
}

// handleGetLogLevel returns the current log level.
func handleGetLogLevel(cfg DebugConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Logger == nil {
			c.JSON(http.StatusOK, gin.H{"level": "unknown", "error": "no logger configured"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"level":     cfg.Logger.GetLevel(),
			"component": "apiserver",
			"hint":      "PUT /debug/log-level?level=debug to change dynamically",
		})
	}
}

// handleSetLogLevel dynamically changes the log level at runtime.
func handleSetLogLevel(cfg DebugConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.Logger == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no logger configured"})
			return
		}
		newLevel := c.Query("level")
		if newLevel == "" {
			var body struct {
				Level string `json:"level"`
			}
			if c.ShouldBindJSON(&body) == nil {
				newLevel = body.Level
			}
		}
		if newLevel == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "missing level parameter",
				"valid": []string{"debug", "info", "warn", "error"},
			})
			return
		}
		if err := cfg.Logger.SetLevel(newLevel); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"level":   cfg.Logger.GetLevel(),
			"message": fmt.Sprintf("Log level changed to %s", newLevel),
		})
	}
}

// handleDebugServices probes all known CloudAI services and reports their status.
func handleDebugServices() gin.HandlerFunc {
	type serviceCheck struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Status   string `json:"status"`
		Latency  string `json:"latency"`
		Language string `json:"language"`
		Error    string `json:"error,omitempty"`
	}

	services := []struct {
		Name     string
		URL      string
		Language string
	}{
		{"apiserver", "http://localhost:8080/healthz", "go"},
		{"scheduler", "http://scheduler:8081/healthz", "go"},
		{"agent", "http://agent:8082/healthz", "go"},
		{"ai-engine", "http://ai-engine:8090/healthz", "python"},
		{"ai-engine-tracing", "http://ai-engine:8090/debug/tracing", "python"},
	}

	return func(c *gin.Context) {
		results := make([]serviceCheck, 0, len(services))
		client := &http.Client{Timeout: 3 * time.Second}

		for _, svc := range services {
			start := time.Now()
			result := serviceCheck{
				Name:     svc.Name,
				URL:      svc.URL,
				Language: svc.Language,
			}

			resp, err := client.Get(svc.URL)
			result.Latency = fmt.Sprintf("%.0fms", float64(time.Since(start).Microseconds())/1000)

			if err != nil {
				result.Status = "unreachable"
				result.Error = err.Error()
			} else {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					result.Status = "healthy"
				} else {
					result.Status = fmt.Sprintf("unhealthy (HTTP %d)", resp.StatusCode)
				}
			}
			results = append(results, result)
		}

		healthy := 0
		for _, r := range results {
			if r.Status == "healthy" {
				healthy++
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"summary":  fmt.Sprintf("%d/%d services healthy", healthy, len(results)),
			"services": results,
			"hint":     "Run with --profile monitoring for Jaeger/Prometheus/Grafana",
		})
	}
}

// handleDebugTracing returns trace context information for verifying cross-language propagation.
func handleDebugTracing() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-ID")
		if traceID == "" {
			traceID = logging.TraceIDFromContext(c.Request.Context())
		}

		c.JSON(http.StatusOK, gin.H{
			"service":     "apiserver",
			"language":    "go",
			"trace_id":    traceID,
			"request_id":  logging.RequestIDFromContext(c.Request.Context()),
			"propagation": "W3C TraceContext (traceparent/tracestate)",
			"otlp_endpoint": os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
			"how_to_verify": gin.H{
				"step1": "Call any Go API endpoint → note X-Trace-ID in response header",
				"step2": "Go service calls ai-engine → same trace_id propagated via traceparent header",
				"step3": "Open Jaeger UI (http://localhost:16686) → search by trace_id",
				"step4": "Verify spans from both Go (apiserver) and Python (ai-engine) appear in same trace",
			},
			"ai_engine_debug_url": "http://ai-engine:8090/debug/tracing",
		})
	}
}

// handleGoroutineDump returns a text dump of all goroutines.
func handleGoroutineDump() gin.HandlerFunc {
	return func(c *gin.Context) {
		buf := make([]byte, 1<<20) // 1MB
		n := runtime.Stack(buf, true)

		format := c.DefaultQuery("format", "text")
		if format == "json" {
			// Parse goroutine stacks into structured format
			stacks := strings.Split(string(buf[:n]), "\n\n")
			c.JSON(http.StatusOK, gin.H{
				"goroutine_count": runtime.NumGoroutine(),
				"stacks":          len(stacks),
				"dump":            stacks,
			})
			return
		}

		c.Data(http.StatusOK, "text/plain; charset=utf-8", buf[:n])
	}
}

// handleMemoryStats returns detailed memory allocation statistics.
func handleMemoryStats() gin.HandlerFunc {
	return func(c *gin.Context) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		c.JSON(http.StatusOK, gin.H{
			"heap": gin.H{
				"alloc_mb":    m.HeapAlloc / 1024 / 1024,
				"sys_mb":      m.HeapSys / 1024 / 1024,
				"idle_mb":     m.HeapIdle / 1024 / 1024,
				"inuse_mb":    m.HeapInuse / 1024 / 1024,
				"objects":     m.HeapObjects,
			},
			"stack": gin.H{
				"inuse_mb": m.StackInuse / 1024 / 1024,
				"sys_mb":   m.StackSys / 1024 / 1024,
			},
			"gc": gin.H{
				"num_gc":           m.NumGC,
				"pause_total_ms":   float64(m.PauseTotalNs) / 1e6,
				"last_pause_ms":    float64(m.PauseNs[(m.NumGC+255)%256]) / 1e6,
				"gc_cpu_fraction":  fmt.Sprintf("%.4f%%", m.GCCPUFraction*100),
				"next_gc_mb":       m.NextGC / 1024 / 1024,
			},
			"total": gin.H{
				"total_alloc_mb": m.TotalAlloc / 1024 / 1024,
				"sys_mb":         m.Sys / 1024 / 1024,
				"mallocs":        m.Mallocs,
				"frees":          m.Frees,
			},
		})
	}
}

// handleDebugEnv returns sanitized environment variables (secrets masked).
func handleDebugEnv() gin.HandlerFunc {
	sensitiveKeys := []string{
		"PASSWORD", "SECRET", "TOKEN", "KEY", "CREDENTIAL",
	}

	return func(c *gin.Context) {
		envVars := make(map[string]string)
		for _, env := range os.Environ() {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key, val := parts[0], parts[1]

			// Only show CLOUDAI_*, OTEL_*, AI_*, GO* env vars
			if !strings.HasPrefix(key, "CLOUDAI_") &&
				!strings.HasPrefix(key, "OTEL_") &&
				!strings.HasPrefix(key, "AI_") &&
				!strings.HasPrefix(key, "GO") {
				continue
			}

			// Mask sensitive values
			masked := false
			for _, s := range sensitiveKeys {
				if strings.Contains(strings.ToUpper(key), s) {
					envVars[key] = val[:min(3, len(val))] + "***"
					masked = true
					break
				}
			}
			if !masked {
				envVars[key] = val
			}
		}

		// Sort keys for deterministic output
		keys := make([]string, 0, len(envVars))
		for k := range envVars {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		ordered := make([]gin.H, 0, len(keys))
		for _, k := range keys {
			ordered = append(ordered, gin.H{"key": k, "value": envVars[k]})
		}

		c.JSON(http.StatusOK, gin.H{
			"count":     len(ordered),
			"variables": ordered,
		})
	}
}

// min returns the smaller of a and b.
func minDebug(a, b int) int {
	if a < b {
		return a
	}
	return b
}
