// Package api provides HTTP handlers for M40 API Security Gateway
package api

import (
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// APIKey represents API key credential
type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Key         string    `json:"key,omitempty"` // Only returned on creation
	Secret      string    `json:"secret,omitempty"`
	OwnerID     string    `json:"ownerId"`
	Permissions []string  `json:"permissions"`
	Scopes      []string  `json:"scopes"`
	RateLimit   int       `json:"rateLimit"` // requests per minute
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
	Metadata    map[string]any `json:"metadata"`
}

// JWTConfig holds JWT validation configuration
type JWTConfig struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Issuer          string            `json:"issuer"`
	Audience        []string          `json:"audience"`
	PublicKey       string            `json:"publicKey"`
	SigningAlgorithm string           `json:"signingAlgorithm"` // RS256, HS256, ES256
	RequiredClaims  []string          `json:"requiredClaims"`
	ValidIATOffset  int               `json:"validIatOffset"` // seconds in future allowed
	CacheTTL        int               `json:"cacheTtl"` // cache JWKS TTL in seconds
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

// RateLimitRule defines rate limiting rules
type RateLimitRule struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	TargetType     string                 `json:"targetType"` // apiKey, ip, user, endpoint
	TargetValue    string                 `json:"targetValue"`
	Limit          int                    `json:"limit"`
	BurstLimit     int                    `json:"burstLimit"`
	DurationSecs   int                    `json:"durationSecs"`
	Action         string                 `json:"action"` // allow, deny, throttle, alert
	Priority       int                    `json:"priority"`
	Enabled        bool                   `json:"enabled"`
	CreatedAt      time.Time              `json:"createdAt"`
	UpdatedAt      time.Time              `json:"updatedAt"`
}

// OAuth2Client represents OAuth2 client application
type OAuth2Client struct {
	ID             string                  `json:"id"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	ClientID       string                  `json:"clientId,omitempty"`
	ClientSecret   string                  `json:"clientSecret,omitempty"`
	GrantTypes     []string                `json:"grantTypes"` // authorization_code, client_credentials, etc.
	RedirectURIs   []string                `json:"redirectUris"`
	Scopes         []string                `json:"scopes"`
	TokenEndpointAuthMethod string `json:"tokenEndpointAuthMethod"` // client_secret_post, none, etc.
	AllowedOrigins []string                `json:"allowedOrigins"`
	Active         bool                    `json:"active"`
	CreatedAt      time.Time               `json:"createdAt"`
	UpdatedAt      time.Time               `json:"updatedAt"`
}

// GraphQLQueryAnalysis analyzes complex GraphQL queries
type GraphQLQueryAnalysis struct {
	ID             string                `json:"id"`
	QueryHash      string                `json:"queryHash"`
	Depth          int                   `json:"depth"`
	Breadth        int                   `json:"breadth"`
	Complexity     int                   `json:"complexity"`
	OperationCount int                   `json:"operationCount"`
	MutationCount  int                   `json:"mutationCount"`
	SubscriptionCount int               `json:"subscriptionCount"`
	Violations     []GraphQLViolation    `json:"violations"`
	AnalyzedAt     time.Time             `json:"analyzedAt"`
	Allowed        bool                  `json:"allowed"`
}

// GraphQLViolation represents a violated query limit
type GraphQLViolation struct {
	Type        string `json:"type"` // depth_exceeded, breadth_exceeded, complexity_high, too_many_operations
	Message     string `json:"message"`
	Threshold   int    `json:"threshold"`
	Actual      int    `json:"actual"`
	FieldPath   string `json:"fieldPath,omitempty"`
}

// APITrafficRecord represents recorded API request
type APITrafficRecord struct {
	ID            string        `json:"id"`
	RequestID     string        `json:"requestId"`
	Endpoint      string        `json:"endpoint"`
	Method        string        `json:"method"`
	PathParams    map[string]any `json:"pathParams"`
	QueryParams   map[string]any `json:"queryParams"`
	APIKeyID      *string       `json:"apiKeyId,omitempty"`
	ClientID      *string       `json:"clientId,omitempty"`
	UserAgent     string        `json:"userAgent"`
	IPAddress     string        `json:"ipAddress"`
	LatencyMs     int           `json:"latencyMs"`
	StatusCode    int           `json:"statusCode"`
	ResponseSize  int64         `json:"responseSize"`
	RequestBodySize int64       `json:"requestBodySize"`
	Errors        []string      `json:"errors,omitempty"`
	Violations    []Violation   `json:"violations"`
	RateLimitRemaining int       `json:"rateLimitRemaining"`
	Timestamp     time.Time     `json:"timestamp"`
}

// Violation detected during request processing
type Violation struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // rate_limit, auth_failed, invalid_schema, malicious_payload
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Details   map[string]any `json:"details"`
	Blocked   bool      `json:"blocked"`
	CreatedAt time.Time `json:"createdAt"`
}

// ThreatDetectionLog threat detection entry
type ThreatDetectionLog struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"` // sql_injection, xss, ssrf, command_injection, sqli, path_traversal
	Severity       string          `json:"severity"`
	SourceIP       string          `json:"sourceIp"`
	TargetEndpoint string          `json:"targetEndpoint"`
	Payload        string          `json:"payload"`
	MatchedPattern string          `json:"matchedPattern"`
	Blocked        bool            `json:"blocked"`
	ActionTaken    string          `json:"actionTaken"`
	Evidence       []string        `json:"evidence"`
	DetectedAt     time.Time       `json:"detectedAt"`
	AnalysisResult map[string]any  `json:"analysisResult"`
}

// ComplianceStatus API security compliance summary
type ComplianceStatus struct {
	ID                        string                 `json:"id"`
	AssessedAt                time.Time              `json:"assessedAt"`
	SecurityPolicy            string                 `json:"securityPolicy"`
	Version                   string                 `json:"version"`
	OverallScore              float64                `json:"overallScore"` // 0-100
	SecurityControlsApplied   int                    `json:"securityControlsApplied"`
	SecurityControlsExpected  int                    `json:"securityControlsExpected"`
	CompliantControls         []string               `json:"compliantControls"`
	NonCompliantControls      []string               `json:"nonCompliantControls"`
	Warnings                  []ControlWarning       `json:"warnings"`
	Status                    string                 `json:"status"` // compliant, non_compliant, partial
}

// ControlWarning individual control warning
type ControlWarning struct {
	ControlID   string  `json:"controlId"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Score       float64 `json:"score"`
}

// APISecurityHandler handles API gateway security operations
type APISecurityHandler struct {
	store      *APISecurityStore
	evidence   *evidence.Ledger
	logger     *logrus.Logger
}

// NewAPISecurityHandler creates handler instance
func NewAPISecurityHandler(
	store *APISecurityStore,
	evidenceLedger *evidence.Ledger,
	logger *logrus.Logger,
) *APISecurityHandler {
	return &APISecurityHandler{
		store:      store,
		evidence:   evidenceLedger,
		logger:     logger,
	}
}

// RegisterRoutes registers REST endpoints
func (h *APISecurityHandler) RegisterRoutes(router *echo.Echo) {
	gateway := router.Group("/api/m40/gateway")

	// API Key management
	keys := gateway.Group("/api-keys")
	keys.POST("/", h.createAPIKey)
	keys.GET("/", h.listAPIKeys)
	keys.GET("/:id", h.getAPIKey)
	keys.PUT("/:id", h.updateAPIKey)
	keys.DELETE("/:id", h.deleteAPIKey)
	keys.POST("/:id/rotate", h.rotateAPIKey)
	keys.POST("/:id/disable", h.disableAPIKey)
	keys.POST("/:id/enable", h.enableAPIKey)
	keys.GET("/:id/usage", h.getAPIKeyUsage)

	// JWT configuration
	jwt := gateway.Group("/jwt")
	jwt.POST("/", h.createJWTConfig)
	jwt.GET("/", h.listJWTConfigs)
	jwt.GET("/:id", h.getJWTConfig)
	jwt.PUT("/:id", h.updateJWTConfig)
	jwt.DELETE("/:id", h.deleteJWTConfig)
	jwt.POST("/:id/validate", h.validateJWTToken)
	jwt.POST("/verify-jwks", h.verifyJWKS)

	// Rate limiting
	ratelimit := gateway.Group("/rate-limits")
	ratelimit.POST("/", h.createRateLimitRule)
	ratelimit.GET("/", h.listRateLimitRules)
	ratelimit.GET("/:id", h.getRateLimitRule)
	ratelimit.PUT("/:id", h.updateRateLimitRule)
	ratelimit.DELETE("/:id", h.deleteRateLimitRule)
	ratelimit.POST("/:id/test", h.testRateLimitRule)
	ratelimit.POST("/bulk-apply", h.bulkApplyRateLimits)

	// OAuth2 clients
	oauth := gateway.Group("/oauth-clients")
	oauth.POST("/", h.createOAuthClient)
	oauth.GET("/", h.listOAuthClients)
	oauth.GET("/:id", h.getOAuthClient)
	oauth.PUT("/:id", h.updateOAuthClient)
	oauth.DELETE("/:id", h.deleteOAuthClient)
	oauth.POST("/:id/regenerate-secret", h.regenerateClientSecret)
	oauth.POST("/token", h.issueToken)

	// GraphQL analysis
	graphql := gateway.Group("/graphql")
	graphql.POST("/analyze", h.analyzeGraphQLQuery)
	graphql.POST("/validate-complexity", h.validateQueryComplexity)
	graphql.GET("/operations", h.getOperationHistory)

	// Traffic monitoring
	traffic := gateway.Group("/traffic")
	traffic.GET("/requests", h.queryTrafficRequests)
	traffic.GET("/requests/:id", h.getTrafficRequest)
	traffic.GET("/metrics", h.getTrafficMetrics)
	traffic.POST("/export", h.exportTrafficData)

	// Threat detection and logging
	threats := gateway.Group("/threats")
	threats.POST("/detect", h.detectThreat)
	threats.GET("/", h.listThreats)
	threats.GET("/:id", h.getThreat)
	threats.PUT("/:id/block", h.blockThreatSource)
	threats.PUT("/:id/unblock", h.unblockThreatSource)

	// Compliance assessment
	compliance := gateway.Group("/compliance")
	compliance.POST("/assess", h.assessAPICompliance)
	compliance.GET("/assessments/:id", h.getComplianceAssessment)
	compliance.GET("/standards", h.listComplianceStandards)
}

// createAPIKey creates new API key
func (h *APISecurityHandler) createAPIKey(c echo.Context) error {
	var key APIKey
	if err := c.Bind(&key); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	key.ID = uuid.New().String()
	key.CreatedAt = time.Now()
	key.Active = true
	
	// Generate random key pair
	key.Key = "ak_" + uuid.New().String()[:16]
	key.Secret = uuid.New().String()

	if err := h.store.CreateAPIKey(&key); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, key)
}

// listAPIKeys retrieves all API keys
func (h *APISecurityHandler) listAPIKeys(c echo.Context) error {
	ownerID := c.QueryParam("ownerId")
	activeOnly := c.QueryParam("active") == "true"

	keys, err := h.store.ListAPIKeys(ownerID, activeOnly)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, keys)
}

// getAPIKey retrieves specific key
func (h *APISecurityHandler) getAPIKey(c echo.Context) error {
	id := c.Param("id")
	key, err := h.store.GetAPIKey(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, key)
}

// updateAPIKey updates key config
func (h *APISecurityHandler) updateAPIKey(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	key, err := h.store.UpdateAPIKey(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, key)
}

// deleteAPIKey deletes API key
func (h *APISecurityHandler) deleteAPIKey(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteAPIKey(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "API key deleted"})
}

// rotateAPIKey rotates credentials
func (h *APISecurityHandler) rotateAPIKey(c echo.Context) error {
	id := c.Param("id")
	newKey := "ak_" + uuid.New().String()[:16]
	newSecret := uuid.New().String()

	if err := h.store.RotateAPIKey(id, newKey, newSecret); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"id":      id,
		"newKey":  newKey,
		"message": "Key rotated successfully - save the new secret immediately",
	})
}

// disableAPIKey disables key
func (h *APISecurityHandler) disableAPIKey(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DisableAPIKey(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "API key disabled"})
}

// enableAPIKey enables key
func (h *APISecurityHandler) enableAPIKey(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.EnableAPIKey(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "API key enabled"})
}

// getAPIKeyUsage returns usage statistics
func (h *APISecurityHandler) getAPIKeyUsage(c echo.Context) error {
	id := c.Param("id")
	startTime := c.QueryParam("startTime")
	endTime := c.QueryParam("endTime")

	usage, err := h.store.GetAPIKeyUsage(id, startTime, endTime)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Key not found"})
	}
	return c.JSON(http.StatusOK, usage)
}

// createJWTConfig creates JWT validation config
func (h *APISecurityHandler) createJWTConfig(c echo.Context) error {
	var config JWTConfig
	if err := c.Bind(&config); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	config.ID = uuid.New().String()
	config.CreatedAt = time.Now()
	config.UpdatedAt = time.Now()

	if err := h.store.CreateJWTConfig(&config); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, config)
}

// listJWTConfigs retrieves all configs
func (h *APISecurityHandler) listJWTConfigs(c echo.Context) error {
	configs, err := h.store.ListJWTConfigs()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, configs)
}

// getJWTConfig retrieves specific config
func (h *APISecurityHandler) getJWTConfig(c echo.Context) error {
	id := c.Param("id")
	config, err := h.store.GetJWTConfig(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}
	return c.JSON(http.StatusOK, config)
}

// updateJWTConfig updates config
func (h *APISecurityHandler) updateJWTConfig(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	config, err := h.store.UpdateJWTConfig(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}
	return c.JSON(http.StatusOK, config)
}

// deleteJWTConfig deletes config
func (h *APISecurityHandler) deleteJWTConfig(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteJWTConfig(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Config not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Config deleted"})
}

// validateJWTToken validates JWT token
func (h *APISecurityHandler) validateJWTToken(c echo.Context) error {
	var req struct {
		Token   string `json:"token"`
		ConfigID string `json:"configId"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	result := h.store.ValidateJWTToken(req.Token, req.ConfigID)
	return c.JSON(http.StatusOK, result)
}

// verifyJWKS verifies JWKS endpoint
func (h *APISecurityHandler) verifyJWKS(c echo.Context) error {
	var req struct {
		JWKSEndpoint string `json:"jwksEndpoint"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	result := h.store.VerifyJWKS(req.JWKSEndpoint)
	return c.JSON(http.StatusOK, result)
}

// createRateLimitRule creates rule
func (h *APISecurityHandler) createRateLimitRule(c echo.Context) error {
	var rule RateLimitRule
	if err := c.Bind(&rule); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	rule.ID = uuid.New().String()
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()
	rule.Enabled = true

	if err := h.store.CreateRateLimitRule(&rule); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, rule)
}

// listRateLimitRules retrieves all rules
func (h *APISecurityHandler) listRateLimitRules(c echo.Context) error {
	rules, err := h.store.ListRateLimitRules()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, rules)
}

// getRateLimitRule retrieves specific rule
func (h *APISecurityHandler) getRateLimitRule(c echo.Context) error {
	id := c.Param("id")
	rule, err := h.store.GetRateLimitRule(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Rule not found"})
	}
	return c.JSON(http.StatusOK, rule)
}

// updateRateLimitRule updates rule
func (h *APISecurityHandler) updateRateLimitRule(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	rule, err := h.store.UpdateRateLimitRule(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Rule not found"})
	}
	return c.JSON(http.StatusOK, rule)
}

// deleteRateLimitRule deletes rule
func (h *APISecurityHandler) deleteRateLimitRule(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteRateLimitRule(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Rule not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Rule deleted"})
}

// testRateLimitRule tests rule effectiveness
func (h *APISecurityHandler) testRateLimitRule(c echo.Context) error {
	id := c.Param("id")
	var req struct {
		TestRequests int `json:"testRequests"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	results := h.store.TestRateLimitRule(id, req.TestRequests)
	return c.JSON(http.StatusOK, results)
}

// bulkApplyRateLimits applies multiple rules at once
func (h *APISecurityHandler) bulkApplyRateLimits(c echo.Context) error {
	var req struct {
		RuleIDs []string `json:"ruleIds"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	affected, err := h.store.BulkApplyRateLimits(req.RuleIDs)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Failed to apply rules"})
	}

	return c.JSON(http.StatusOK, map[string]any{
		"affectedRules": len(affected),
		"message":       "Rate limits applied successfully",
	})
}

// createOAuthClient creates OAuth2 client
func (h *APISecurityHandler) createOAuthClient(c echo.Context) error {
	var client OAuth2Client
	if err := c.Bind(&client); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	client.ID = uuid.New().String()
	client.CreatedAt = time.Now()
	client.UpdatedAt = time.Now()
	client.Active = true
	client.ClientID = "oc_" + uuid.New().String()[:16]
	client.ClientSecret = uuid.New().String()

	if err := h.store.CreateOAuthClient(&client); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}

	return c.JSON(http.StatusCreated, client)
}

// listOAuthClients retrieves all clients
func (h *APISecurityHandler) listOAuthClients(c echo.Context) error {
	clients, err := h.store.ListOAuthClients()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{"error": "Database error"})
	}
	return c.JSON(http.StatusOK, clients)
}

// getOAuthClient retrieves specific client
func (h *APISecurityHandler) getOAuthClient(c echo.Context) error {
	id := c.Param("id")
	client, err := h.store.GetOAuthClient(id)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Client not found"})
	}
	return c.JSON(http.StatusOK, client)
}

// updateOAuthClient updates client config
func (h *APISecurityHandler) updateOAuthClient(c echo.Context) error {
	id := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	updates["updatedAt"] = time.Now()
	client, err := h.store.UpdateOAuthClient(id, updates)
	if err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Client not found"})
	}
	return c.JSON(http.StatusOK, client)
}

// deleteOAuthClient deletes client
func (h *APISecurityHandler) deleteOAuthClient(c echo.Context) error {
	id := c.Param("id")
	if err := h.store.DeleteOAuthClient(id); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Client not found"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "Client deleted"})
}

// regenerateClientSecret generates new secret
func (h *APISecurityHandler) regenerateClientSecret(c echo.Context) error {
	id := c.Param("id")
	newSecret := uuid.New().String()

	if err := h.store.RegenerateClientSecret(id, newSecret); err != nil {
		return c.JSON(http.StatusNotFound, ErrorResponse{"error": "Client not found"})
	}

	return c.JSON(http.StatusOK, map[string]string{
		"id":         id,
		"clientSecret": newSecret,
		"message":    "New secret generated - save it immediately as old one is invalidated",
	})
}

// issueToken issues OAuth2 token
func (h *APISecurityHandler) issueToken(c echo.Context) error {
	var req struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		GrantType    string `json:"grantType"`
		Scopes       []string `json:"scopes"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	token, err := h.store.IssueToken(req.ClientID, req.ClientSecret, req.GrantType, req.Scopes)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, ErrorResponse{"error": "Token issuance failed"})
	}
	return c.JSON(http.StatusOK, token)
}

// analyzeGraphQLQuery analyzes GraphQL query
func (h *APISecurityHandler) analyzeGraphQLQuery(c echo.Context) error {
	var req struct {
		Query string `json:"query"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	analysis := h.store.AnalyzeGraphQLQuery(req.Query)
	return c.JSON(http.StatusOK, analysis)
}

// validateQueryComplexity checks query complexity limits
func (h *APISecurityHandler) validateQueryComplexity(c echo.Context) error {
	var req struct {
		Query     string `json:"query"`
		MaxDepth  int    `json:"maxDepth"`
		MaxBreadth int   `json:"maxBreadth"`
	}

	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{"error": "Invalid payload"})
	}

	result := h.store.ValidateQueryComplexity(req.Query, req.MaxDepth, req.MaxBreadth)
	return c.JSON(http.StatusOK, result)
}

// getOperationHistory retrieves GraphQL operation history
func (h *APISecurityHandler) getOperationHistory(c echo.Context) error {
	limit := 100
	page := 1

	if l := c.QueryParam("limit"); l != "" {
		limit, _ = parseInt(l)
	}
	if p := c.QueryParam("page"); p != "" {
		page, _ = parseInt(p)
	}

	history := h.store.GetOperationHistory(limit, page)
	return c.JSON(http.StatusOK, history)
}

// parseInt helper function
func parseInt(s string) (int, error) {
	return 0, nil
}
