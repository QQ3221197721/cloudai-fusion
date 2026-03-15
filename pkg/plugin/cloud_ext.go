package plugin

import (
	"context"
	"time"
)

// ============================================================================
// Cloud Provider Extension Point
// ============================================================================

// CloudClusterInfo is a provider-agnostic cluster representation.
type CloudClusterInfo struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`
	Region     string            `json:"region"`
	Status     string            `json:"status"`
	Version    string            `json:"version"`
	NodeCount  int               `json:"nodeCount"`
	Labels     map[string]string `json:"labels,omitempty"`
	CostPerDay float64           `json:"costPerDay,omitempty"`
}

// CloudProviderPlugin allows third-party cloud providers to be plugged in.
type CloudProviderPlugin interface {
	Plugin
	// ProviderName returns the cloud provider identifier (e.g., "aws", "gcp").
	ProviderName() string
	// ListClusters returns all Kubernetes clusters managed by this provider.
	ListClusters(ctx context.Context) ([]*CloudClusterInfo, error)
	// GetCluster returns a specific cluster by ID.
	GetCluster(ctx context.Context, clusterID string) (*CloudClusterInfo, error)
	// EstimateCost returns estimated cost for the given time range.
	EstimateCost(ctx context.Context, clusterID string, start, end time.Time) (float64, error)
}

// ============================================================================
// Monitoring Collector Extension Point
// ============================================================================

// MetricSample is a single metric data point.
type MetricSample struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels,omitempty"`
	Unit      string            `json:"unit,omitempty"`
}

// CollectorPlugin gathers metrics from external sources.
type CollectorPlugin interface {
	Plugin
	// Collect gathers metrics. Called periodically by the monitoring subsystem.
	Collect(ctx context.Context) ([]MetricSample, error)
	// MetricNames returns the list of metric names this collector produces.
	MetricNames() []string
}

// ============================================================================
// Monitoring Alerter Extension Point
// ============================================================================

// Alert represents an alert to be dispatched.
type Alert struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Severity string            `json:"severity"` // info, warning, critical
	Message  string            `json:"message"`
	Source   string            `json:"source"`
	Labels   map[string]string `json:"labels,omitempty"`
	FiredAt  time.Time         `json:"firedAt"`
}

// AlerterPlugin dispatches alerts to external channels (Slack, PagerDuty, etc.).
type AlerterPlugin interface {
	Plugin
	// SendAlert dispatches an alert notification.
	SendAlert(ctx context.Context, alert *Alert) error
	// SupportedChannels returns channel names (e.g., "slack", "pagerduty").
	SupportedChannels() []string
}

// ============================================================================
// Auth Authenticator Extension Point
// ============================================================================

// AuthIdentity is the authenticated identity returned by an authenticator.
type AuthIdentity struct {
	UserID   string            `json:"userId"`
	Username string            `json:"username"`
	Email    string            `json:"email,omitempty"`
	Groups   []string          `json:"groups,omitempty"`
	Claims   map[string]string `json:"claims,omitempty"`
	Provider string            `json:"provider"` // e.g., "oidc", "ldap", "saml"
}

// AuthenticatorPlugin provides custom authentication mechanisms.
type AuthenticatorPlugin interface {
	Plugin
	// Authenticate validates credentials/token and returns an identity.
	Authenticate(ctx context.Context, token string) (*AuthIdentity, error)
	// AuthType returns the authentication type (e.g., "bearer", "basic", "oidc").
	AuthType() string
}

// ============================================================================
// Auth Authorizer Extension Point
// ============================================================================

// AuthzRequest is an authorization check request.
type AuthzRequest struct {
	Identity   *AuthIdentity `json:"identity"`
	Action     string        `json:"action"`     // "get", "list", "create", "update", "delete"
	Resource   string        `json:"resource"`   // "clusters", "workloads", "policies"
	ResourceID string        `json:"resourceId,omitempty"`
	Namespace  string        `json:"namespace,omitempty"`
}

// AuthzDecision is the result of an authorization check.
type AuthzDecision struct {
	Allowed    bool   `json:"allowed"`
	Reason     string `json:"reason,omitempty"`
	PluginName string `json:"pluginName"`
}

// AuthorizerPlugin evaluates whether an authenticated identity is allowed
// to perform an action.
type AuthorizerPlugin interface {
	Plugin
	// Authorize checks if the action is permitted.
	Authorize(ctx context.Context, req *AuthzRequest) (*AuthzDecision, error)
}

// ============================================================================
// Cloud/Monitoring/Auth Plugin Chains
// ============================================================================

// CloudPluginChain orchestrates cloud provider plugins.
type CloudPluginChain struct {
	registry *Registry
}

// NewCloudPluginChain creates a new cloud plugin chain.
func NewCloudPluginChain(reg *Registry) *CloudPluginChain {
	return &CloudPluginChain{registry: reg}
}

// ListAllClusters aggregates clusters from all cloud provider plugins.
func (c *CloudPluginChain) ListAllClusters(ctx context.Context) ([]*CloudClusterInfo, error) {
	var all []*CloudClusterInfo
	for _, p := range c.registry.GetByExtension(ExtCloudProvider) {
		cp, ok := p.(CloudProviderPlugin)
		if !ok {
			continue
		}
		clusters, err := cp.ListClusters(ctx)
		if err != nil {
			return all, err
		}
		all = append(all, clusters...)
	}
	return all, nil
}

// MonitorPluginChain orchestrates monitoring plugins.
type MonitorPluginChain struct {
	registry *Registry
}

// NewMonitorPluginChain creates a new monitoring plugin chain.
func NewMonitorPluginChain(reg *Registry) *MonitorPluginChain {
	return &MonitorPluginChain{registry: reg}
}

// CollectAll gathers metrics from all collector plugins.
func (c *MonitorPluginChain) CollectAll(ctx context.Context) ([]MetricSample, error) {
	var all []MetricSample
	for _, p := range c.registry.GetByExtension(ExtMonitorCollector) {
		cp, ok := p.(CollectorPlugin)
		if !ok {
			continue
		}
		samples, err := cp.Collect(ctx)
		if err != nil {
			return all, err
		}
		all = append(all, samples...)
	}
	return all, nil
}

// DispatchAlert sends an alert to all alerter plugins.
func (c *MonitorPluginChain) DispatchAlert(ctx context.Context, alert *Alert) error {
	for _, p := range c.registry.GetByExtension(ExtMonitorAlerter) {
		ap, ok := p.(AlerterPlugin)
		if !ok {
			continue
		}
		if err := ap.SendAlert(ctx, alert); err != nil {
			return err
		}
	}
	return nil
}

// AuthPluginChain orchestrates auth plugins.
type AuthPluginChain struct {
	registry *Registry
}

// NewAuthPluginChain creates a new auth plugin chain.
func NewAuthPluginChain(reg *Registry) *AuthPluginChain {
	return &AuthPluginChain{registry: reg}
}

// Authenticate tries each authenticator plugin until one succeeds.
func (c *AuthPluginChain) Authenticate(ctx context.Context, token string) (*AuthIdentity, error) {
	for _, p := range c.registry.GetByExtension(ExtAuthAuthenticator) {
		ap, ok := p.(AuthenticatorPlugin)
		if !ok {
			continue
		}
		identity, err := ap.Authenticate(ctx, token)
		if err == nil && identity != nil {
			return identity, nil
		}
	}
	return nil, &ErrPluginNotFound{Name: "authenticator"}
}

// Authorize checks all authorizer plugins. All must allow.
func (c *AuthPluginChain) Authorize(ctx context.Context, req *AuthzRequest) (*AuthzDecision, error) {
	for _, p := range c.registry.GetByExtension(ExtAuthAuthorizer) {
		ap, ok := p.(AuthorizerPlugin)
		if !ok {
			continue
		}
		decision, err := ap.Authorize(ctx, req)
		if err != nil {
			return nil, err
		}
		if !decision.Allowed {
			return decision, nil
		}
	}
	return &AuthzDecision{Allowed: true}, nil
}
