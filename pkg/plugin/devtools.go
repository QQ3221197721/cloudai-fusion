// Package plugin provides developer tools for CloudAI Fusion plugin development.
// Includes scaffolding generator, plugin test framework, and plugin linter
// for rapid plugin development and quality assurance.
package plugin

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Scaffolding Generator — Generate Plugin Boilerplate
// ============================================================================

// ScaffoldConfig defines the parameters for generating a new plugin scaffold.
type ScaffoldConfig struct {
	Name            string            `json:"name" yaml:"name"`
	Version         string            `json:"version" yaml:"version"`
	Author          string            `json:"author" yaml:"author"`
	License         string            `json:"license" yaml:"license"`
	Description     string            `json:"description" yaml:"description"`
	GoModule        string            `json:"go_module" yaml:"goModule"`
	ExtensionPoints []ExtensionPoint  `json:"extension_points" yaml:"extensionPoints"`
	Dependencies    []string          `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	WithTest        bool              `json:"with_test" yaml:"withTest"`
	WithConfig      bool              `json:"with_config" yaml:"withConfig"`
	WithMetrics     bool              `json:"with_metrics" yaml:"withMetrics"`
	Tags            map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// ScaffoldOutput holds generated scaffold files.
type ScaffoldOutput struct {
	Files     map[string]string `json:"files"`     // filename → content
	Manifest  *PluginManifest   `json:"manifest"`
	CreatedAt time.Time         `json:"created_at"`
}

// ScaffoldGenerator generates plugin project scaffolding.
type ScaffoldGenerator struct {
	templates map[string]*template.Template
	logger    *logrus.Logger
}

// NewScaffoldGenerator creates a scaffold generator with built-in templates.
func NewScaffoldGenerator(logger *logrus.Logger) *ScaffoldGenerator {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	g := &ScaffoldGenerator{
		templates: make(map[string]*template.Template),
		logger:    logger,
	}
	g.registerBuiltinTemplates()
	return g
}

// Generate creates a complete plugin scaffold from config.
func (g *ScaffoldGenerator) Generate(cfg ScaffoldConfig) (*ScaffoldOutput, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("plugin name is required")
	}
	if cfg.Version == "" {
		cfg.Version = "0.1.0"
	}
	if cfg.License == "" {
		cfg.License = "Apache-2.0"
	}
	if len(cfg.ExtensionPoints) == 0 {
		return nil, fmt.Errorf("at least one extension point is required")
	}

	output := &ScaffoldOutput{
		Files:     make(map[string]string),
		CreatedAt: time.Now().UTC(),
	}

	// Generate manifest
	manifest := &PluginManifest{
		APIVersion: "v1",
		Kind:       "CloudAIPlugin",
		Metadata: Metadata{
			Name:            cfg.Name,
			Version:         cfg.Version,
			Description:     cfg.Description,
			Author:          cfg.Author,
			License:         cfg.License,
			ExtensionPoints: cfg.ExtensionPoints,
			Dependencies:    cfg.Dependencies,
			Priority:        100,
			Tags:            cfg.Tags,
		},
		Spec: PluginSpec{
			MinPlatformVersion: "1.0.0",
			GoModule:           cfg.GoModule,
			EntryPoint:         "New" + toPascalCase(cfg.Name) + "Plugin",
		},
	}
	output.Manifest = manifest

	// Template data
	data := map[string]interface{}{
		"Name":            cfg.Name,
		"PascalName":      toPascalCase(cfg.Name),
		"Version":         cfg.Version,
		"Author":          cfg.Author,
		"License":         cfg.License,
		"Description":     cfg.Description,
		"GoModule":        cfg.GoModule,
		"ExtensionPoints": cfg.ExtensionPoints,
		"Dependencies":    cfg.Dependencies,
		"WithConfig":      cfg.WithConfig,
		"WithMetrics":     cfg.WithMetrics,
	}

	// Generate main plugin file
	mainContent, err := g.renderTemplate("plugin_main", data)
	if err != nil {
		return nil, fmt.Errorf("failed to generate main plugin file: %w", err)
	}
	output.Files["plugin.go"] = mainContent

	// Generate config file if requested
	if cfg.WithConfig {
		configContent, err := g.renderTemplate("plugin_config", data)
		if err != nil {
			return nil, fmt.Errorf("failed to generate config file: %w", err)
		}
		output.Files["config.go"] = configContent
	}

	// Generate test file if requested
	if cfg.WithTest {
		testContent, err := g.renderTemplate("plugin_test", data)
		if err != nil {
			return nil, fmt.Errorf("failed to generate test file: %w", err)
		}
		output.Files["plugin_test.go"] = testContent
	}

	// Generate metrics file if requested
	if cfg.WithMetrics {
		metricsContent, err := g.renderTemplate("plugin_metrics", data)
		if err != nil {
			return nil, fmt.Errorf("failed to generate metrics file: %w", err)
		}
		output.Files["metrics.go"] = metricsContent
	}

	g.logger.WithFields(logrus.Fields{
		"plugin":     cfg.Name,
		"files":      len(output.Files),
		"extensions": len(cfg.ExtensionPoints),
	}).Info("Plugin scaffold generated")

	return output, nil
}

func (g *ScaffoldGenerator) renderTemplate(name string, data interface{}) (string, error) {
	tmpl, ok := g.templates[name]
	if !ok {
		return "", fmt.Errorf("template %q not found", name)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (g *ScaffoldGenerator) registerBuiltinTemplates() {
	g.templates["plugin_main"] = template.Must(template.New("plugin_main").Parse(pluginMainTmpl))
	g.templates["plugin_config"] = template.Must(template.New("plugin_config").Parse(pluginConfigTmpl))
	g.templates["plugin_test"] = template.Must(template.New("plugin_test").Parse(pluginTestTmpl))
	g.templates["plugin_metrics"] = template.Must(template.New("plugin_metrics").Parse(pluginMetricsTmpl))
}

// ============================================================================
// Plugin Test Framework
// ============================================================================

// PluginTestHarness provides a testing framework for plugin development.
type PluginTestHarness struct {
	registry  *Registry
	plugins   map[string]Plugin
	configs   map[string]map[string]interface{}
	events    []TestEvent
	mu        sync.Mutex
	logger    *logrus.Logger
}

// TestEvent records lifecycle events during testing.
type TestEvent struct {
	Time      time.Time `json:"time"`
	Plugin    string    `json:"plugin"`
	EventType string    `json:"event_type"` // init, start, stop, health, error
	Message   string    `json:"message,omitempty"`
	Duration  time.Duration `json:"duration"`
	Error     error     `json:"-"`
}

// TestResult holds the results of a plugin test run.
type TestResult struct {
	PluginName  string        `json:"plugin_name"`
	Passed      bool          `json:"passed"`
	Events      []TestEvent   `json:"events"`
	InitTime    time.Duration `json:"init_time"`
	StartTime   time.Duration `json:"start_time"`
	StopTime    time.Duration `json:"stop_time"`
	HealthOK    bool          `json:"health_ok"`
	Errors      []string      `json:"errors,omitempty"`
	Warnings    []string      `json:"warnings,omitempty"`
}

// NewPluginTestHarness creates a new test harness.
func NewPluginTestHarness(logger *logrus.Logger) *PluginTestHarness {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &PluginTestHarness{
		registry: NewRegistry(),
		plugins:  make(map[string]Plugin),
		configs:  make(map[string]map[string]interface{}),
		logger:   logger,
	}
}

// RegisterPlugin registers a plugin for testing.
func (h *PluginTestHarness) RegisterPlugin(p Plugin, config map[string]interface{}) {
	name := p.Metadata().Name
	h.plugins[name] = p
	h.configs[name] = config
}

// RunLifecycleTest tests a plugin's complete lifecycle: Init → Start → Health → Stop.
func (h *PluginTestHarness) RunLifecycleTest(ctx context.Context, pluginName string) *TestResult {
	result := &TestResult{PluginName: pluginName, Passed: true}

	p, ok := h.plugins[pluginName]
	if !ok {
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("plugin %q not registered", pluginName))
		return result
	}
	config := h.configs[pluginName]

	// Test Init
	start := time.Now()
	err := p.Init(ctx, config)
	result.InitTime = time.Since(start)
	h.recordEvent(pluginName, "init", err)
	if err != nil {
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("Init failed: %v", err))
		return result
	}

	// Test Start
	start = time.Now()
	err = p.Start(ctx)
	result.StartTime = time.Since(start)
	h.recordEvent(pluginName, "start", err)
	if err != nil {
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("Start failed: %v", err))
		return result
	}

	// Test Health
	err = p.Health(ctx)
	result.HealthOK = err == nil
	h.recordEvent(pluginName, "health", err)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Health check failed: %v", err))
	}

	// Test Stop
	start = time.Now()
	err = p.Stop(ctx)
	result.StopTime = time.Since(start)
	h.recordEvent(pluginName, "stop", err)
	if err != nil {
		result.Passed = false
		result.Errors = append(result.Errors, fmt.Sprintf("Stop failed: %v", err))
	}

	// Lifecycle timing validations
	if result.InitTime > 5*time.Second {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Init took %v (>5s threshold)", result.InitTime))
	}
	if result.StopTime > 10*time.Second {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Stop took %v (>10s threshold)", result.StopTime))
	}

	h.mu.Lock()
	result.Events = append(result.Events, h.events...)
	h.mu.Unlock()

	return result
}

// RunAllTests tests all registered plugins.
func (h *PluginTestHarness) RunAllTests(ctx context.Context) []*TestResult {
	results := make([]*TestResult, 0, len(h.plugins))
	for name := range h.plugins {
		results = append(results, h.RunLifecycleTest(ctx, name))
	}
	return results
}

func (h *PluginTestHarness) recordEvent(plugin, eventType string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	event := TestEvent{
		Time:      time.Now(),
		Plugin:    plugin,
		EventType: eventType,
		Error:     err,
	}
	if err != nil {
		event.Message = err.Error()
	}
	h.events = append(h.events, event)
}

// ============================================================================
// Plugin Linter — Static Analysis
// ============================================================================

// LintRule defines a single linting rule for plugins.
type LintRule struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"` // error, warning, info
	Description string `json:"description"`
	Check       func(Plugin) []LintIssue `json:"-"`
}

// LintIssue represents a single issue found by the linter.
type LintIssue struct {
	RuleID      string `json:"rule_id"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Suggestion  string `json:"suggestion,omitempty"`
}

// LintReport holds the complete linting results.
type LintReport struct {
	PluginName string      `json:"plugin_name"`
	Issues     []LintIssue `json:"issues"`
	Score      int         `json:"score"` // 0-100
	Passed     bool        `json:"passed"`
}

// PluginLinter runs static analysis checks on plugins.
type PluginLinter struct {
	rules []LintRule
}

// NewPluginLinter creates a linter with default rules.
func NewPluginLinter() *PluginLinter {
	l := &PluginLinter{}
	l.registerDefaultRules()
	return l
}

// AddRule adds a custom lint rule.
func (l *PluginLinter) AddRule(rule LintRule) {
	l.rules = append(l.rules, rule)
}

// Lint runs all rules against a plugin and returns a report.
func (l *PluginLinter) Lint(p Plugin) *LintReport {
	meta := p.Metadata()
	report := &LintReport{
		PluginName: meta.Name,
		Score:      100,
		Passed:     true,
	}

	for _, rule := range l.rules {
		issues := rule.Check(p)
		report.Issues = append(report.Issues, issues...)
	}

	// Calculate score
	for _, issue := range report.Issues {
		switch issue.Severity {
		case "error":
			report.Score -= 20
			report.Passed = false
		case "warning":
			report.Score -= 5
		case "info":
			report.Score -= 1
		}
	}
	if report.Score < 0 {
		report.Score = 0
	}

	return report
}

func (l *PluginLinter) registerDefaultRules() {
	l.rules = []LintRule{
		{
			ID:          "PLG001",
			Severity:    "error",
			Description: "Plugin name must not be empty",
			Check: func(p Plugin) []LintIssue {
				if p.Metadata().Name == "" {
					return []LintIssue{{RuleID: "PLG001", Severity: "error", Message: "plugin name is empty", Suggestion: "Set a unique name in Metadata()"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG002",
			Severity:    "error",
			Description: "Plugin must have at least one extension point",
			Check: func(p Plugin) []LintIssue {
				if len(p.Metadata().ExtensionPoints) == 0 {
					return []LintIssue{{RuleID: "PLG002", Severity: "error", Message: "no extension points declared", Suggestion: "Add at least one ExtensionPoint to metadata"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG003",
			Severity:    "warning",
			Description: "Plugin version should follow semver",
			Check: func(p Plugin) []LintIssue {
				if !semverRe.MatchString(p.Metadata().Version) {
					return []LintIssue{{RuleID: "PLG003", Severity: "warning", Message: fmt.Sprintf("version %q is not valid semver", p.Metadata().Version), Suggestion: "Use format like 1.0.0 or v1.0.0"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG004",
			Severity:    "warning",
			Description: "Plugin should have a description",
			Check: func(p Plugin) []LintIssue {
				if p.Metadata().Description == "" {
					return []LintIssue{{RuleID: "PLG004", Severity: "warning", Message: "plugin has no description", Suggestion: "Add a description to improve discoverability"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG005",
			Severity:    "info",
			Description: "Plugin priority should be within recommended range",
			Check: func(p Plugin) []LintIssue {
				priority := p.Metadata().Priority
				if priority < 0 || priority > 10000 {
					return []LintIssue{{RuleID: "PLG005", Severity: "info", Message: fmt.Sprintf("priority %d is outside recommended range [0, 10000]", priority), Suggestion: "Use priority between 0 and 10000"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG006",
			Severity:    "warning",
			Description: "Plugin author should be specified",
			Check: func(p Plugin) []LintIssue {
				if p.Metadata().Author == "" {
					return []LintIssue{{RuleID: "PLG006", Severity: "warning", Message: "plugin author is not specified", Suggestion: "Set author in metadata for attribution"}}
				}
				return nil
			},
		},
		{
			ID:          "PLG007",
			Severity:    "info",
			Description: "Plugin should have a license",
			Check: func(p Plugin) []LintIssue {
				if p.Metadata().License == "" {
					return []LintIssue{{RuleID: "PLG007", Severity: "info", Message: "plugin license is not specified", Suggestion: "Set an SPDX license identifier (e.g., Apache-2.0)"}}
				}
				return nil
			},
		},
	}
}

// ============================================================================
// Plugin Dependency Checker
// ============================================================================

// DependencyChecker validates plugin dependency graphs.
type DependencyChecker struct {
	registry *Registry
}

// NewDependencyChecker creates a dependency checker.
func NewDependencyChecker(registry *Registry) *DependencyChecker {
	return &DependencyChecker{registry: registry}
}

// CheckDependencies validates that all dependencies for a plugin are available.
func (dc *DependencyChecker) CheckDependencies(pluginName string) []string {
	var issues []string

	registry := dc.registry
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	p, ok := registry.plugins[pluginName]
	if !ok {
		return []string{fmt.Sprintf("plugin %q not found in registry", pluginName)}
	}

	for _, dep := range p.Metadata().Dependencies {
		if _, exists := registry.plugins[dep]; !exists {
			issues = append(issues, fmt.Sprintf("missing dependency: %q required by %q", dep, pluginName))
		}
	}
	return issues
}

// DetectCircularDeps checks for circular dependencies in the registry.
func (dc *DependencyChecker) DetectCircularDeps() []string {
	registry := dc.registry
	registry.mu.RLock()
	defer registry.mu.RUnlock()

	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var cycles []string

	var dfs func(name string, path []string) bool
	dfs = func(name string, path []string) bool {
		visited[name] = true
		inStack[name] = true
		path = append(path, name)

		p, ok := registry.plugins[name]
		if !ok {
			return false
		}

		for _, dep := range p.Metadata().Dependencies {
			if !visited[dep] {
				if dfs(dep, path) {
					return true
				}
			} else if inStack[dep] {
				cycle := append(path, dep)
				cycles = append(cycles, fmt.Sprintf("circular dependency: %s", strings.Join(cycle, " → ")))
				return true
			}
		}

		inStack[name] = false
		return false
	}

	for name := range registry.plugins {
		if !visited[name] {
			dfs(name, nil)
		}
	}

	return cycles
}

// ============================================================================
// Scaffold Templates
// ============================================================================

var pluginMainTmpl = `package main

import (
	"context"
	"fmt"

	plugin "github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// {{.PascalName}}Plugin implements a CloudAI Fusion plugin.
type {{.PascalName}}Plugin struct {
	plugin.BasePlugin
	config map[string]interface{}
}

// New{{.PascalName}}Plugin creates a new {{.Name}} plugin instance.
func New{{.PascalName}}Plugin() (plugin.Plugin, error) {
	return &{{.PascalName}}Plugin{
		BasePlugin: plugin.NewBasePlugin(plugin.Metadata{
			Name:            "{{.Name}}",
			Version:         "{{.Version}}",
			Description:     "{{.Description}}",
			Author:          "{{.Author}}",
			License:         "{{.License}}",
			ExtensionPoints: []plugin.ExtensionPoint{ {{- range .ExtensionPoints}}"{{.}}", {{end -}} },
			Priority:        100,
		}),
	}, nil
}

func (p *{{.PascalName}}Plugin) Init(ctx context.Context, config map[string]interface{}) error {
	p.config = config
	return nil
}

func (p *{{.PascalName}}Plugin) Start(ctx context.Context) error {
	fmt.Printf("[%s] Plugin started\n", p.Metadata().Name)
	return nil
}

func (p *{{.PascalName}}Plugin) Stop(ctx context.Context) error {
	fmt.Printf("[%s] Plugin stopped\n", p.Metadata().Name)
	return nil
}

func (p *{{.PascalName}}Plugin) Health(ctx context.Context) error {
	return nil
}
`

var pluginConfigTmpl = `package main

// {{.PascalName}}Config holds configuration for the {{.Name}} plugin.
type {{.PascalName}}Config struct {
	Enabled  bool   ` + "`json:\"enabled\" yaml:\"enabled\"`" + `
	LogLevel string ` + "`json:\"log_level\" yaml:\"logLevel\"`" + `
}

// Default{{.PascalName}}Config returns sensible defaults.
func Default{{.PascalName}}Config() *{{.PascalName}}Config {
	return &{{.PascalName}}Config{
		Enabled:  true,
		LogLevel: "info",
	}
}
`

var pluginTestTmpl = `package main

import (
	"context"
	"testing"
)

func TestNew{{.PascalName}}Plugin(t *testing.T) {
	p, err := New{{.PascalName}}Plugin()
	if err != nil {
		t.Fatalf("failed to create plugin: %v", err)
	}
	meta := p.Metadata()
	if meta.Name != "{{.Name}}" {
		t.Errorf("expected name %q, got %q", "{{.Name}}", meta.Name)
	}
	if meta.Version != "{{.Version}}" {
		t.Errorf("expected version %q, got %q", "{{.Version}}", meta.Version)
	}
}

func Test{{.PascalName}}Plugin_Lifecycle(t *testing.T) {
	ctx := context.Background()
	p, err := New{{.PascalName}}Plugin()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Init(ctx, nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := p.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := p.Health(ctx); err != nil {
		t.Fatalf("Health failed: %v", err)
	}
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}
`

var pluginMetricsTmpl = `package main

import "sync/atomic"

// {{.PascalName}}Metrics collects runtime metrics for the {{.Name}} plugin.
type {{.PascalName}}Metrics struct {
	RequestsTotal   int64
	ErrorsTotal     int64
	LatencySum      int64
	LatencyCount    int64
}

func (m *{{.PascalName}}Metrics) RecordRequest(latencyMs int64, errored bool) {
	atomic.AddInt64(&m.RequestsTotal, 1)
	atomic.AddInt64(&m.LatencySum, latencyMs)
	atomic.AddInt64(&m.LatencyCount, 1)
	if errored {
		atomic.AddInt64(&m.ErrorsTotal, 1)
	}
}

func (m *{{.PascalName}}Metrics) AvgLatency() float64 {
	count := atomic.LoadInt64(&m.LatencyCount)
	if count == 0 {
		return 0
	}
	return float64(atomic.LoadInt64(&m.LatencySum)) / float64(count)
}
`

// ============================================================================
// Helpers
// ============================================================================

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, "")
}
