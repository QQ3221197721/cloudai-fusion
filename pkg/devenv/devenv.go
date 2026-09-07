// Package devenv provides stub implementations for local development environment management.
package devenv

import (
	"fmt"
)

// Environment represents a local dev environment configuration
type Environment struct {
	Name        string
	Status      string
	Components  []string
	Ports       map[string]int
	StartedAt   string
}

// Starter manages development environment startup
type Starter struct{}

// NewStarter creates a new environment starter
func NewStarter() *Starter {
	return &Starter{}
}

// Start starts a mock development environment
func (s *Starter) Start(name string) (*Environment, error) {
	if name == "" {
		name = "default"
	}
	
	return &Environment{
		Name:      name,
		Status:    "running",
		Components: []string{"api-server", "redis", "postgres", "grafana", "prometheus"},
		Ports: map[string]int{
			"api":     8080,
			"redis":   6379,
			"postgres": 5432,
			"grafana": 3000,
			"prometheus": 9090,
		},
		StartedAt: "just now",
	}, nil
}

// Stop stops a development environment
func (s *Starter) Stop(name string) error {
	return nil
}

// List lists available environments
func (s *Starter) List() ([]*Environment, error) {
	return []*Environment{
		{
			Name:     "local-dev",
			Status:   "running",
			Components: []string{"api-server", "redis", "postgres"},
			Ports: map[string]int{
				"api":     8080,
				"redis":   6379,
				"postgres": 5432,
			},
		},
		{
			Name:     "full-stack",
			Status:   "stopped",
			Components: []string{"api-server", "redis", "postgres", "grafana", "prometheus"},
			Ports: map[string]int{
				"api":     8080,
				"redis":   6379,
				"postgres": 5432,
				"grafana": 3000,
			},
		},
	}, nil
}

// Status returns the status of an environment
func (s *Starter) Status(name string) (*Environment, error) {
	if name == "" {
		name = "default"
	}
	return &Environment{
		Name:   name,
		Status: "running",
		Components: []string{"api-server", "redis"},
		Ports: map[string]int{
			"api":   8080,
			"redis": 6379,
		},
	}, nil
}

// MockConfig returns a mock configuration for testing
func MockConfig() string {
	return `# Mock development environment config
API_SERVER_URL=http://localhost:8080
REDIS_URL=redis://localhost:6379
POSTGRES_URL=postgres://localhost:5432/db
GRAFANA_URL=http://localhost:3000
`
}

// Verify implements a verification check
func Verify(name string) error {
	fmt.Printf("Verifying environment %s...\n", name)
	return nil
}
