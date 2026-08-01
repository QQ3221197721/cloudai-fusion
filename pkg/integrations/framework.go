// Package slack - Integration framework core definitions
package integrations

import (
	"context"
	"fmt"
	
	"github.com/sirupsen/logrus"
)

var log = logrus.New()

// ============================================================================
// Core Integration Interface
// ============================================================================

// SeverityLevel defines alert severity levels (shared across integrations)
type SeverityLevel string

const (
	Critical SeverityLevel = "critical"
	High     SeverityLevel = "high"
	Medium   SeverityLevel = "medium"
	Low      SeverityLevel = "low"
	Info     SeverityLevel = "info"
)

// Event represents an event that can be handled by integrations
type Event struct {
	Type      string
	Timestamp int64
	Data      map[string]any
}

// Integration defines the interface all integrations must implement
type Integration interface {
	// Name returns integration identifier
	Name() string
	
	// Version returns integration version
	Version() string
	
	// HealthCheck verifies the integration is operational
	HealthCheck(ctx context.Context) error
	
	// Configure updates integration configuration dynamically
	Configure(ctx context.Context, config map[string]any) error
	
	// EventHandler processes incoming events
	EventHandler(ctx context.Context, event Event) error
	
	// Cleanup releases resources when integration is shutting down
	Cleanup() error
}

// Registry manages integration lifecycle
type Registry struct {
	integrations map[string]Integration
	config       Config
	logger       any // Using 'any' to avoid import cycles in this base file
}

// Config holds integration registry configuration
type Config struct {
	EnabledIntegrations []string
	DefaultTimeout      int // seconds
	AuditAllOps         bool
}

// NewRegistry creates a new integration registry
func NewRegistry(config Config) *Registry {
	return &Registry{
		integrations: make(map[string]Integration),
		config:       config,
	}
}

// Register adds an integration to the registry
func (r *Registry) Register(name string, integration Integration) error {
	if _, exists := r.integrations[name]; exists {
		return fmt.Errorf("integration already registered: %s", name)
	}
	
	r.integrations[name] = integration
	
	log.Infof("Registered integration: %s (%s)", integration.Name(), integration.Version())
	return nil
}

// Get retrieves an integration by name
func (r *Registry) Get(name string) (Integration, error) {
	integration, exists := r.integrations[name]
	if !exists {
		return nil, fmt.Errorf("integration not found: %s", name)
	}
	
	return integration, nil
}

// List returns all registered integrations
func (r *Registry) List() []Integration {
	result := make([]Integration, 0, len(r.integrations))
	for _, integration := range r.integrations {
		result = append(result, integration)
	}
	return result
}

// HealthCheckAll performs health checks on all integrations
func (r *Registry) HealthCheckAll(ctx context.Context) map[string]error {
	results := make(map[string]error)
	
	for name, integration := range r.integrations {
		if err := integration.HealthCheck(ctx); err != nil {
			results[name] = err
		} else {
			results[name] = nil
		}
	}
	
	return results
}

// CleanupAll gracefully shuts down all integrations
func (r *Registry) CleanupAll(ctx context.Context) {
	for name, integration := range r.integrations {
		log.Infof("Cleaning up integration: %s", name)
		if err := integration.Cleanup(); err != nil {
			log.Errorf("Failed to cleanup integration %s: %v", name, err)
		}
	}
}
