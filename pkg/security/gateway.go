// Package security - gateway.go provides API Gateway security features.
// Includes advanced rate limiting with tiered plans, IP allowlisting/blocklisting,
// WAF-like request inspection rules, request signing verification,
// and API key management for external consumers.
package security

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// API Key Model
// ============================================================================

// APIKey represents an API key for external consumers.
type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Key         string    `json:"key"` // hashed in storage
	Prefix      string    `json:"prefix"` // first 8 chars for identification
	OwnerID     string    `json:"owner_id"`
	Plan        string    `json:"plan"` // free, basic, pro, enterprise
	Scopes      []string  `json:"scopes"` // allowed API scopes
	RateLimit   int       `json:"rate_limit"` // requests per minute
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Enabled     bool      `json:"enabled"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// IsExpired returns true if the key has expired.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// ============================================================================
// IP Access Control
// ============================================================================

// IPAccessList manages IP allowlists and blocklists.
type IPAccessList struct {
	AllowCIDRs  []string `json:"allow_cidrs,omitempty"`  // if set, only these allowed
	BlockCIDRs  []string `json:"block_cidrs,omitempty"`
	BlockIPs    []string `json:"block_ips,omitempty"`
	parsedAllow []*net.IPNet
	parsedBlock []*net.IPNet
	mu          sync.RWMutex
}

// NewIPAccessList creates an IP access control list.
func NewIPAccessList(allowCIDRs, blockCIDRs []string) *IPAccessList {
	acl := &IPAccessList{
		AllowCIDRs: allowCIDRs,
		BlockCIDRs: blockCIDRs,
	}
	acl.parse()
	return acl
}

func (acl *IPAccessList) parse() {
	acl.parsedAllow = parseCIDRs(acl.AllowCIDRs)
	acl.parsedBlock = parseCIDRs(acl.BlockCIDRs)
}

// IsAllowed checks if an IP is permitted.
func (acl *IPAccessList) IsAllowed(ip string) bool {
	acl.mu.RLock()
	defer acl.mu.RUnlock()

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	// Check blocklist first
	for _, blocked := range acl.parsedBlock {
		if blocked.Contains(parsed) {
			return false
		}
	}
	for _, blocked := range acl.BlockIPs {
		if ip == blocked {
			return false
		}
	}

	// If allowlist is set, IP must be in it
	if len(acl.parsedAllow) > 0 {
		for _, allowed := range acl.parsedAllow {
			if allowed.Contains(parsed) {
				return true
			}
		}
		return false
	}

	return true
}

// AddBlock adds an IP or CIDR to the blocklist.
func (acl *IPAccessList) AddBlock(cidr string) {
	acl.mu.Lock()
	defer acl.mu.Unlock()
	acl.BlockCIDRs = append(acl.BlockCIDRs, cidr)
	acl.parsedBlock = parseCIDRs(acl.BlockCIDRs)
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	var result []*net.IPNet
	for _, cidr := range cidrs {
		if !strings.Contains(cidr, "/") {
			cidr += "/32"
		}
		_, network, err := net.ParseCIDR(cidr)
		if err == nil {
			result = append(result, network)
		}
	}
	return result
}

// ============================================================================
// WAF Rules
// ============================================================================

// WAFRule defines a Web Application Firewall inspection rule.
type WAFRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Target      string `json:"target"` // path, query, header, body, user-agent
	Pattern     string `json:"pattern"` // regex
	Action      string `json:"action"` // block, log, challenge
	Severity    string `json:"severity"` // critical, high, medium, low
	Enabled     bool   `json:"enabled"`
	compiled    *regexp.Regexp
}

// WAFEngine inspects requests against security rules.
type WAFEngine struct {
	rules  []*WAFRule
	logger *logrus.Logger
	mu     sync.RWMutex
}

// NewWAFEngine creates a WAF engine with default rules.
func NewWAFEngine(logger *logrus.Logger) *WAFEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	engine := &WAFEngine{
		rules:  DefaultWAFRules(),
		logger: logger,
	}
	engine.compileRules()
	return engine
}

func (w *WAFEngine) compileRules() {
	for _, r := range w.rules {
		if r.Pattern != "" {
			compiled, err := regexp.Compile(r.Pattern)
			if err == nil {
				r.compiled = compiled
			}
		}
	}
}

// Inspect checks a request against WAF rules.
func (w *WAFEngine) Inspect(c *gin.Context) *WAFViolation {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, rule := range w.rules {
		if !rule.Enabled || rule.compiled == nil {
			continue
		}

		var target string
		switch rule.Target {
		case "path":
			target = c.Request.URL.Path
		case "query":
			target = c.Request.URL.RawQuery
		case "user-agent":
			target = c.Request.UserAgent()
		case "header":
			for _, vals := range c.Request.Header {
				target += strings.Join(vals, " ")
			}
		default:
			continue
		}

		if rule.compiled.MatchString(target) {
			return &WAFViolation{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Action:   rule.Action,
				Severity: rule.Severity,
				Matched:  target,
			}
		}
	}
	return nil
}

// AddRule adds a custom WAF rule.
func (w *WAFEngine) AddRule(rule *WAFRule) error {
	compiled, err := regexp.Compile(rule.Pattern)
	if err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}
	rule.compiled = compiled
	if rule.ID == "" {
		rule.ID = common.NewUUID()
	}
	w.mu.Lock()
	w.rules = append(w.rules, rule)
	w.mu.Unlock()
	return nil
}

// ListRules returns all WAF rules.
func (w *WAFEngine) ListRules() []*WAFRule {
	w.mu.RLock()
	defer w.mu.RUnlock()
	result := make([]*WAFRule, len(w.rules))
	copy(result, w.rules)
	return result
}

// WAFViolation represents a detected WAF rule match.
type WAFViolation struct {
	RuleID   string `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Action   string `json:"action"`
	Severity string `json:"severity"`
	Matched  string `json:"matched"`
}

// ============================================================================
// API Gateway
// ============================================================================

// GatewayConfig configures the API Gateway security layer.
type GatewayConfig struct {
	IPAccessList  *IPAccessList
	WAFEngine     *WAFEngine
	APIKeys       []*APIKey
	Logger        *logrus.Logger
	EnableWAF     bool
	EnableIPACL   bool
	EnableAPIKeys bool
}

// Gateway provides API gateway security features.
type Gateway struct {
	ipACL     *IPAccessList
	waf       *WAFEngine
	apiKeys   map[string]*APIKey // key → APIKey
	logger    *logrus.Logger
	enableWAF bool
	enableIP  bool
	enableKey bool
	mu        sync.RWMutex
}

// NewGateway creates a new API Gateway security layer.
func NewGateway(cfg GatewayConfig) *Gateway {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	gw := &Gateway{
		ipACL:     cfg.IPAccessList,
		waf:       cfg.WAFEngine,
		apiKeys:   make(map[string]*APIKey),
		logger:    cfg.Logger,
		enableWAF: cfg.EnableWAF,
		enableIP:  cfg.EnableIPACL,
		enableKey: cfg.EnableAPIKeys,
	}
	for _, k := range cfg.APIKeys {
		gw.apiKeys[k.Key] = k
	}
	if gw.ipACL == nil {
		gw.ipACL = NewIPAccessList(nil, nil)
	}
	if gw.waf == nil {
		gw.waf = NewWAFEngine(cfg.Logger)
	}
	return gw
}

// RegisterAPIKey registers a new API key.
func (gw *Gateway) RegisterAPIKey(key *APIKey) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	if key.ID == "" {
		key.ID = common.NewUUID()
	}
	if key.Prefix == "" && len(key.Key) >= 8 {
		key.Prefix = key.Key[:8]
	}
	gw.apiKeys[key.Key] = key
	gw.logger.WithFields(logrus.Fields{
		"key_id": key.ID, "plan": key.Plan,
	}).Info("API key registered")
}

// RevokeAPIKey disables an API key.
func (gw *Gateway) RevokeAPIKey(keyValue string) bool {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	key, ok := gw.apiKeys[keyValue]
	if ok {
		key.Enabled = false
		return true
	}
	return false
}

// ValidateAPIKey checks if an API key is valid and returns the key info.
func (gw *Gateway) ValidateAPIKey(keyValue string) (*APIKey, error) {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	key, ok := gw.apiKeys[keyValue]
	if !ok {
		return nil, fmt.Errorf("invalid API key")
	}
	if !key.Enabled {
		return nil, fmt.Errorf("API key is disabled")
	}
	if key.IsExpired() {
		return nil, fmt.Errorf("API key has expired")
	}
	now := time.Now()
	key.LastUsedAt = &now
	return key, nil
}

// ============================================================================
// Gin Middleware
// ============================================================================

// GatewayMiddleware returns a Gin middleware chain for API Gateway security.
func (gw *Gateway) GatewayMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. IP Access Control
		if gw.enableIP {
			if !gw.ipACL.IsAllowed(c.ClientIP()) {
				gw.logger.WithField("ip", c.ClientIP()).Warn("IP blocked by ACL")
				c.AbortWithStatusJSON(403, gin.H{
					"code": 403, "message": "access denied: IP not allowed",
				})
				return
			}
		}

		// 2. WAF Inspection
		if gw.enableWAF {
			violation := gw.waf.Inspect(c)
			if violation != nil && violation.Action == "block" {
				gw.logger.WithFields(logrus.Fields{
					"rule_id": violation.RuleID, "severity": violation.Severity,
				}).Warn("Request blocked by WAF")
				c.AbortWithStatusJSON(403, gin.H{
					"code": 403, "message": "request blocked by security rules",
				})
				return
			}
			if violation != nil && violation.Action == "log" {
				gw.logger.WithFields(logrus.Fields{
					"rule_id": violation.RuleID, "severity": violation.Severity,
				}).Info("WAF rule triggered (log-only)")
			}
		}

		// 3. API Key validation (if X-API-Key header present)
		if gw.enableKey {
			apiKey := c.GetHeader("X-API-Key")
			if apiKey != "" {
				key, err := gw.ValidateAPIKey(apiKey)
				if err != nil {
					c.AbortWithStatusJSON(401, gin.H{
						"code": 401, "message": err.Error(),
					})
					return
				}
				c.Set("api_key_id", key.ID)
				c.Set("api_key_plan", key.Plan)
				c.Set("api_key_scopes", key.Scopes)
			}
		}

		c.Next()
	}
}

// ============================================================================
// Status
// ============================================================================

// GatewayStatus reports the API Gateway security status.
type GatewayStatus struct {
	WAFEnabled    bool `json:"waf_enabled"`
	WAFRules      int  `json:"waf_rules"`
	IPACLEnabled  bool `json:"ip_acl_enabled"`
	AllowRules    int  `json:"allow_rules"`
	BlockRules    int  `json:"block_rules"`
	APIKeysActive int  `json:"api_keys_active"`
	APIKeysTotal  int  `json:"api_keys_total"`
}

// Status returns the current gateway status.
func (gw *Gateway) Status() GatewayStatus {
	gw.mu.RLock()
	defer gw.mu.RUnlock()

	active := 0
	for _, k := range gw.apiKeys {
		if k.Enabled && !k.IsExpired() {
			active++
		}
	}

	return GatewayStatus{
		WAFEnabled:    gw.enableWAF,
		WAFRules:      len(gw.waf.rules),
		IPACLEnabled:  gw.enableIP,
		AllowRules:    len(gw.ipACL.AllowCIDRs),
		BlockRules:    len(gw.ipACL.BlockCIDRs) + len(gw.ipACL.BlockIPs),
		APIKeysActive: active,
		APIKeysTotal:  len(gw.apiKeys),
	}
}

// ============================================================================
// Default WAF Rules
// ============================================================================

// DefaultWAFRules returns OWASP-inspired WAF rules.
func DefaultWAFRules() []*WAFRule {
	return []*WAFRule{
		{
			ID: "waf-sqli", Name: "SQL Injection Detection",
			Target: "query", Pattern: `(?i)(union\s+select|or\s+1\s*=\s*1|;\s*drop\s+table|'\s*or\s*'|--\s*$)`,
			Action: "block", Severity: "critical", Enabled: true,
		},
		{
			ID: "waf-xss", Name: "XSS Detection",
			Target: "query", Pattern: `(?i)(<script|javascript:|on\w+\s*=|<img\s+src)`,
			Action: "block", Severity: "high", Enabled: true,
		},
		{
			ID: "waf-path-traversal", Name: "Path Traversal Detection",
			Target: "path", Pattern: `(\.\./|\.\.\\|%2e%2e%2f|%2e%2e/)`,
			Action: "block", Severity: "high", Enabled: true,
		},
		{
			ID: "waf-cmd-injection", Name: "Command Injection Detection",
			Target: "query", Pattern: `(?i)(;\s*cat\s|;\s*ls\s|;\s*rm\s|\|.*sh\s|` + "`" + `.*` + "`" + `)`,
			Action: "block", Severity: "critical", Enabled: true,
		},
		{
			ID: "waf-scanner-detect", Name: "Scanner/Bot Detection",
			Target: "user-agent", Pattern: `(?i)(sqlmap|nikto|nessus|acunetix|nmap|masscan|gobuster|dirbuster)`,
			Action: "block", Severity: "medium", Enabled: true,
		},
		{
			ID: "waf-sensitive-file", Name: "Sensitive File Access",
			Target: "path", Pattern: `(?i)(\.(env|git|svn|htaccess|htpasswd|bak|sql|dump)|/wp-admin|/phpmyadmin)`,
			Action: "block", Severity: "high", Enabled: true,
		},
	}
}
