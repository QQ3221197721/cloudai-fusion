// Package metasploit - Stubs for external dependencies
package metasploit

import "fmt"

// msfrpc provides stub types for Metasploit RPC client
var msfrpc = struct {
	NewRPC func(opts map[string]interface{}) *msfrpcClient
}{
	NewRPC: func(opts map[string]interface{}) *msfrpcClient {
		return &msfrpcClient{opts: opts}
	},
}

type msfrpcClient struct {
	opts map[string]interface{}
}

// RPCClient is the interface for Metasploit RPC operations
type RPCClient interface {
	IsOpen() bool
	Close() error
	ModuleUse(name interface{}) (*ModuleResult, error)
	ModuleRun(opts interface{}, payload map[string]interface{}) ([]byte, error)
	Search(cmd map[string]interface{}) (*SearchResult, error)
	Options(data []byte) error
	RunModule(opts interface{}) (map[string]interface{}, error)
	Command(cmd string, args map[string]interface{}) (map[string]interface{}, error)
}

type ModuleResult struct {
	Options interface{}
}

// SearchResult holds search results from Metasploit
type SearchResult struct {
	Results []map[string]interface{}
}

func (c *msfrpcClient) Open() (RPCClient, error) {
	return &msfrpcConn{}, nil
}

type msfrpcConn struct{}

func (c *msfrpcConn) IsOpen() bool { return true }
func (c *msfrpcConn) Close() error { return nil }
func (c *msfrpcConn) ModuleUse(name interface{}) (*ModuleResult, error) {
	return &ModuleResult{}, nil
}
func (c *msfrpcConn) ModuleRun(opts interface{}, payload map[string]interface{}) ([]byte, error) {
	return nil, fmt.Errorf("msfrpc not available in this build")
}
func (c *msfrpcConn) Search(cmd map[string]interface{}) (*SearchResult, error) {
	return &SearchResult{}, nil
}
func (c *msfrpcConn) Options(data []byte) error {
	return nil
}
func (c *msfrpcConn) RunModule(opts interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"sessions": []interface{}{}}, nil
}
func (c *msfrpcConn) Command(cmd string, args map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{}, nil
}

// ConfigValidator validates Metasploit configuration
type ConfigValidator struct{}

// NewConfigValidator creates a new validator
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{}
}

// Validate checks configuration for correctness
func (cv *ConfigValidator) Validate(config GlobalConfig) error {
	if config.RPC.Host == "" {
		return fmt.Errorf("RPC host is required")
	}
	if config.RPC.Port <= 0 || config.RPC.Port > 65535 {
		return fmt.Errorf("RPC port must be between 1 and 65535")
	}
	return nil
}
