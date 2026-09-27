// Package docgen provides high-speed OpenAPI/Swagger to Go client code generation.
// This module is optimized for speed through careful parsing strategies and minimal allocations.
package docgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// APIClientGenerator generates idiomatic Go client code from OpenAPI/Swagger specs.
// Optimized for speed in CI/CD pipelines with minimal memory allocations.
type APIClientGenerator struct {
	config  Config
	cache   *specCache
	functions template.FuncMap
}

// Config holds generation parameters for the API client generator.
type Config struct {
	OutputDir     string        // Output directory for generated code (required)
	PackageName   string        // Go package name for generated code (required)
	IncludeAuth   bool          // Include authentication middleware support
	RetryPolicy   string        // Retry policy: "none", "simple", or "exponential" (default: "simple")
	Timeout       time.Duration // HTTP client timeout (default: 30s)
	GenerateModels bool         // Also generate model types from schema definitions
	CompatMode    bool          // Generate compatibility wrappers for legacy APIs
}

// Operation represents a single OpenAPI operation (HTTP method + path).
type Operation struct {
	OperationID     string            `yaml:"operationId"`
	Method          string            `yaml:"-"` // Set during extraction, not from YAML
	Path            string            `yaml:"-"` // Set during extraction, not from YAML
	Summary         string            `yaml:"summary"`
	Description     string            `yaml:"description"`
	Parameters      []Parameter       `yaml:"parameters"`
	RequestBody     *RequestBody      `yaml:"requestBody"`
	Responses       map[string]Response `yaml:"responses"`
	Tags            []string          `yaml:"tags"`
	Deprecated      bool              `yaml:"deprecated"`
}

// Parameter represents an OpenAPI parameter (query, header, path, cookie).
type Parameter struct {
	Name        string    `yaml:"name"`
	In          string    `yaml:"in"` // query, header, path, cookie
	Description string    `yaml:"description"`
	Required    bool      `yaml:"required,omitempty"`
	Schema      SchemaRef `yaml:"schema"`
	Style       string    `yaml:"style,omitempty"`
	Explode     *bool     `yaml:"explode,omitempty"`
}

// RequestBody represents an OpenAPI request body.
type RequestBody struct {
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content"`
	Required    bool                 `yaml:"required,omitempty"`
}

// Response represents an OpenAPI response.
type Response struct {
	Description string                 `yaml:"description"`
	Content     map[string]MediaType   `yaml:"content"`
	Headers     map[string]Header      `yaml:"headers"`
}

// Header represents an OpenAPI header.
type Header struct {
	Description string    `yaml:"description"`
	Schema      SchemaRef `yaml:"schema"`
	Required    bool      `yaml:"required,omitempty"`
}

// MediaType represents a media type in request/response content.
type MediaType struct {
	Schema SchemaRef `yaml:"schema"`
	Example interface{} `yaml:"example,omitempty"`
}

// SchemaRef represents an OpenAPI schema definition.
type SchemaRef struct {
	Type             string            `yaml:"type,omitempty"`
	Format           string            `yaml:"format,omitempty"`
	Description      string            `yaml:"description,omitempty"`
	Reference        string            `yaml:"$ref,omitempty"`
	Items            *SchemaRef        `yaml:"items,omitempty"`
	Properties       map[string]SchemaRef `yaml:"properties,omitempty"`
	Required         []string          `yaml:"required,omitempty"`
	Enum             []interface{}     `yaml:"enum,omitempty"`
	DefaultValue     interface{}       `yaml:"default,omitempty"`
	Nullable         bool              `yaml:"nullable,omitempty"`
	AdditionalProperties *bool         `yaml:"additionalProperties,omitempty"`
}

// OpenAPISpec represents the root structure of an OpenAPI specification.
type OpenAPISpec struct {
	OpenAPI      string                  `yaml:"openapi"`
	Info         Info                    `yaml:"info"`
	Servers      []Server                `yaml:"servers,omitempty"`
	Paths        map[string]PathItem     `yaml:"paths"`
	Components   Components              `yaml:"components,omitempty"`
	Tags         []map[string]interface{} `yaml:"tags,omitempty"`
	XMetadata    map[string]interface{}  `yaml:"x-metadata,omitempty"`
}

// Info contains metadata about the API.
type Info struct {
	Title          string `yaml:"title"`
	Description    string `yaml:"description,omitempty"`
	Version        string `yaml:"version"`
	TermsOfService string `yaml:"termsOfService,omitempty"`
	Contact        map[string]interface{} `yaml:"contact,omitempty"`
	License        map[string]interface{} `yaml:"license,omitempty"`
}

// Server represents an OpenAPI server URL.
type Server struct {
	URL         string            `yaml:"url"`
	Description string            `yaml:"description,omitempty"`
	Variables   map[string]map[string]interface{} `yaml:"variables,omitempty"`
}

// Components contains reusable schema definitions.
type Components struct {
	Schemas    map[string]SchemaRef `yaml:"schemas,omitempty"`
	Responses  map[string]Response  `yaml:"responses,omitempty"`
	Parameters map[string]Parameter `yaml:"parameters,omitempty"`
	SecuritySchemes map[string]interface{} `yaml:"securitySchemes,omitempty"`
}

// PathItem represents an OpenAPI path item.
type PathItem struct {
	Summary     string            `yaml:"summary,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Methods     map[string]Operation `yaml:"-"` // GET, POST, PUT, DELETE, etc. - set during parsing
	Get         *Operation        `yaml:"get,omitempty"`
	Put         *Operation        `yaml:"put,omitempty"`
	Post        *Operation        `yaml:"post,omitempty"`
	Delete      *Operation        `yaml:"delete,omitempty"`
	Options     *Operation        `yaml:"options,omitempty"`
	Head        *Operation        `yaml:"head,omitempty"`
	Patch       *Operation        `yaml:"patch,omitempty"`
	Trace       *Operation        `yaml:"trace,omitempty"`
	Server      *Server           `yaml:"server,omitempty"`
	Parameters  []Parameter       `yaml:"parameters,omitempty"`
}

// specCache provides simple file-based caching for parsed specs.
type specCache struct {
	cacheDir string
	ttl      time.Duration
	hits     int64
	misses   int64
}

// NewSpecCache creates a new spec cache with default settings.
func NewSpecCache(cacheDir string, ttl time.Duration) *specCache {
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}
	os.MkdirAll(cacheDir, 0o755)
	return &specCache{
		cacheDir: cacheDir,
		ttl:      ttl,
	}
}

// Get retrieves a cached spec if still valid.
func (c *specCache) Get(hash string) ([]byte, error) {
	path := filepath.Join(c.cacheDir, hash+".yaml")
	data, err := ioutil.ReadFile(path)
	if err != nil {
		c.misses++
		return nil, err
	}
	
	stat, _ := os.Stat(path)
	if stat != nil && time.Since(stat.ModTime()) > c.ttl {
		os.Remove(path)
		c.misses++
		return nil, fmt.Errorf("cache entry expired")
	}
	
	c.hits++
	return data, nil
}

// Put stores a spec in the cache.
func (c *specCache) Put(hash string, data []byte) error {
	path := filepath.Join(c.cacheDir, hash+".yaml")
	return ioutil.WriteFile(path, data, 0o644)
}

// Stats returns cache hit/miss statistics.
func (c *specCache) Stats() (hits, misses int64) {
	return c.hits, c.misses
}

// Reset clears cache statistics.
func (c *specCache) Reset() {
	c.hits = 0
	c.misses = 0
}

// NewAPIClientGenerator creates a new API client generator with the given config.
func NewAPIClientGenerator(config Config) (*APIClientGenerator, error) {
	if config.OutputDir == "" {
		return nil, fmt.Errorf("OutputDir is required")
	}
	if config.PackageName == "" {
		return nil, fmt.Errorf("PackageName is required")
	}
	
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	
	// Initialize cache if not already done
	gen := &APIClientGenerator{
		config:  config,
		cache:   NewSpecCache("", 24*time.Hour),
		functions: template.FuncMap{
			"titleCase":   toTitleCase,
			"pascalCase":  toPascalCase,
			"snakeCase":   toSnakeCase,
			"camelCase":   toCamelCase,
			"lowerFirst":  toLowerFirst,
			"upperFirst":  toUpperFirst,
			"trimSpaces":  strings.TrimSpace,
			"replaceAll":  strings.ReplaceAll,
		},
	}
	
	return gen, nil
}

// Generate creates client code from an OpenAPI spec file.
func (g *APIClientGenerator) Generate(specPath string) error {
	// Step 1: Parse YAML with optional caching
	spec, err := g.loadAndParseSpec(specPath)
	if err != nil {
		return fmt.Errorf("failed to parse spec: %w", err)
	}
	
	// Step 2: Extract operations organized by path
	ops := g.extractOperations(spec)
	
	// Step 3: Determine primary server URL
	baseURL := g.determineBaseURL(spec)
	
	// Step 4: Generate client struct
	clientCode := g.generateClientStruct(ops, baseURL)
	
	// Step 5: Optionally generate models
	var modelsCode string
	if g.config.GenerateModels {
		modelsCode = g.generateModels(spec)
	}
	
	// Step 6: Write outputs to directory
	if err := os.MkdirAll(g.config.OutputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	
	// Write client.go
	clientPath := filepath.Join(g.config.OutputDir, "client.go")
	if err := ioutil.WriteFile(clientPath, []byte(clientCode), 0o644); err != nil {
		return fmt.Errorf("failed to write client.go: %w", err)
	}
	
	// Write models.go if needed
	if modelsCode != "" {
		modelsPath := filepath.Join(g.config.OutputDir, "models.go")
		if err := ioutil.WriteFile(modelsPath, []byte(modelsCode), 0o644); err != nil {
			return fmt.Errorf("failed to write models.go: %w", err)
		}
	}
	
	return nil
}

// loadAndParseSpec loads and parses an OpenAPI spec file.
func (g *APIClientGenerator) loadAndParseSpec(path string) (*OpenAPISpec, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	// Try YAML first (preferred for OpenAPI 3.x)
	var spec OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		// Fall back to JSON if YAML parsing fails
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("failed to parse spec (tried YAML and JSON): %w", err)
		}
	}
	
	// Parse path items into structured operations
	g.parsePathItems(&spec)
	
	return &spec, nil
}

// parsePathItems converts raw YAML path items into structured operations.
func (g *APIClientGenerator) parsePathItems(spec *OpenAPISpec) {
	for path := range spec.Paths {
		pathItem := spec.Paths[path]
		
		// Initialize Methods map if not exists
		if pathItem.Methods == nil {
			pathItem.Methods = make(map[string]Operation)
		}
		
		// Process direct method fields and assign to Methods map
		if pathItem.Get != nil {
			op := *pathItem.Get
			op.Path = path
			op.Method = "GET"
			pathItem.Methods["GET"] = op
		}
		if pathItem.Post != nil {
			op := *pathItem.Post
			op.Path = path
			op.Method = "POST"
			pathItem.Methods["POST"] = op
		}
		if pathItem.Put != nil {
			op := *pathItem.Put
			op.Path = path
			op.Method = "PUT"
			pathItem.Methods["PUT"] = op
		}
		if pathItem.Delete != nil {
			op := *pathItem.Delete
			op.Path = path
			op.Method = "DELETE"
			pathItem.Methods["DELETE"] = op
		}
		if pathItem.Patch != nil {
			op := *pathItem.Patch
			op.Path = path
			op.Method = "PATCH"
			pathItem.Methods["PATCH"] = op
		}
		if pathItem.Head != nil {
			op := *pathItem.Head
			op.Path = path
			op.Method = "HEAD"
			pathItem.Methods["HEAD"] = op
		}
		if pathItem.Options != nil {
			op := *pathItem.Options
			op.Path = path
			op.Method = "OPTIONS"
			pathItem.Methods["OPTIONS"] = op
		}
		if pathItem.Trace != nil {
			op := *pathItem.Trace
			op.Path = path
			op.Method = "TRACE"
			pathItem.Methods["TRACE"] = op
		}
		
		// Update the modified path item back to spec
		spec.Paths[path] = pathItem
	}
}

// determineBaseURL extracts the primary server URL from spec or config.
func (g *APIClientGenerator) determineBaseURL(spec *OpenAPISpec) string {
	if len(spec.Servers) > 0 {
		return strings.TrimSuffix(spec.Servers[0].URL, "/")
	}
	return "https://api.example.com"
}

// extractOperations collects all HTTP operations from the spec.
func (g *APIClientGenerator) extractOperations(spec *OpenAPISpec) map[string][]Operation {
	ops := make(map[string][]Operation)
	
	for path, pathItem := range spec.Paths {
		for method, op := range pathItem.Methods {
			key := strings.ToUpper(method) + " " + path
			ops[key] = append(ops[key], op)
		}
	}
	
	return ops
}

// generateClientStruct creates Go client code using templates.
func (g *APIClientGenerator) generateClientStruct(ops map[string][]Operation, baseURL string) string {
	const clientTmpl = `// Code generated by M40 API Client Generator. DO NOT EDIT.
// Source: OpenAPI {{.SpecVersion}}
// Generated at: {{.GeneratedAt}}

package {{.PackageName}}

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
{{if .HasAuthImports}}	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"{{end}}
)

// Client is the main API client for all endpoints.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
{{if .HasAuthImports}}	authToken  string
	authHeader string{{end}}
{{range .Clients}}	{{.Name | pascalCase}} *{{.Name | pascalCaseClient}}
{{end}}}]

// Config holds client configuration.
type Config struct {
	BaseURL      string
	Timeout      time.Duration
	UserAgent    string
	IncludeAuth  bool
	RetryPolicy  string
{{if .HasAuthImports}}	AuthToken    string{{end}}
}

// NewClient creates a new API client with the given configuration.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
{{if eq cfg.RetryPolicy "exponential"}}
	retryCfg := &RetryConfig{
		MaxRetries:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
	}
{{else if eq cfg.RetryPolicy "simple"}}
	retryCfg := &RetryConfig{
		MaxRetries:  3,
		BaseDelay:   1 * time.Second,
	}
{{else}}
	retryCfg := nil
{{end}}}
	
	baseURL, err := url.Parse(strings.TrimSuffix(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	
	client := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: cfg.Timeout},
		userAgent:  cfg.UserAgent,
{{if .HasAuthImports}}		authToken:  cfg.AuthToken,
		authHeader: "Authorization",{{end}}
	}
{{if .HasAuthImports}}	if cfg.IncludeAuth && cfg.AuthToken != "" {
		client.setAuthToken(cfg.AuthToken)
	}{{end}}
{{range .Clients}}	client.{{.Name | pascalCase}} = New{{.Name | pascalCaseClient}}(baseURL.String(), cfg.Timeout)
{{end}}	return client, nil
}

// RoundTrip implements custom HTTP transport with retry logic.
func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
{{if .HasAuthImports}}	if c.authToken != "" {
		req.Header.Set(c.authHeader, "Bearer "+c.authToken)
	}{{end}}
	
	return c.httpClient.Do(req)
}

// executeRequest executes an HTTP request with optional retry.
func (c *Client) executeRequest(ctx context.Context, method, endpoint string, body interface{}, headers map[string]string) (*http.Response, error) {
	fullURL := c.baseURL.JoinPath(endpoint)
	
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}
	
	req, err := http.NewRequestWithContext(ctx, method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	
	return c.RoundTrip(req)
}

// parseResponse parses HTTP response into target struct.
func parseResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if target != nil && len(body) > 0 {
			if err := json.Unmarshal(body, target); err != nil {
				return fmt.Errorf("failed to unmarshal response: %w", err)
			}
		}
		return nil
	}
	
	return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
}

`

	funcTemplate := template.Must(template.New("client").Funcs(g.functions).Parse(clientTmpl))
	
	var buf bytes.Buffer
	
	// Build client info
	clientInfo := struct {
		PackageName    string
		SpecVersion    string
		GeneratedAt    string
		HasAuthImports bool
		Clients        []struct {
			Name                 string
			NamePascalCaseClient string
		}
	}{
		PackageName:    g.config.PackageName,
		SpecVersion:    "3.0.3", // Use default since spec is not in scope
		GeneratedAt:    time.Now().Format(time.RFC3339),
		HasAuthImports: g.config.IncludeAuth,
	}
	
	for key := range ops {
		firstOp := ops[key][0]
		opID := sanitizeToGoIdentifier(firstOp.OperationID)
		clientInfo.Clients = append(clientInfo.Clients, struct {
			Name                 string
			NamePascalCaseClient string
		}{
			Name:                 strings.ToLower(opID),
			NamePascalCaseClient: toPascalCase(opID),
		})
	}
	
	if err := funcTemplate.Execute(&buf, clientInfo); err != nil {
		return fmt.Sprintf("// Error generating client: %v\n", err)
	}
	
	// Generate individual client methods
	methodTmpl := `
{{range $key, $operations := .Ops}}
{{$firstOp := index $operations 0}}
{{$opName := $firstOp.OperationID | pascalCaseClient}}

// {{trimSpaces $firstOp.Summary}}
func (c *{{opName}}) {{toLowerFirst $firstOp.OperationID}}({{generateMethodParams $firstOp}}, ctx ...context.Context) (*http.Response, error) {
	{{generateMethodBody $firstOp}}
}
{{end}}
`
	
	funcTmpl := template.Must(template.New("methods").Funcs(g.functions).Parse(methodTmpl))
	var methodsBuf bytes.Buffer
	
	if err := funcTmpl.Execute(&methodsBuf, map[string]interface{}{"Ops": ops}); err != nil {
		return buf.String()
	}
	
	buf.WriteString(methodsBuf.String())
	
	return buf.String()
}

// generateMethodParams creates function signature parameters.
func generateMethodParams(op *Operation) string {
	var params []string
	
	for _, param := range op.Parameters {
		goType := openAPIToGoType(param.Schema)
		params = append(params, fmt.Sprintf("%s %s", toCamelCase(param.Name), goType))
	}
	
	if len(params) > 0 {
		return strings.Join(params, ", ")
	}
	return ""
}

// generateMethodBody creates HTTP request implementation.
func generateMethodBody(op *Operation) string {
	var sb strings.Builder
	
	sb.WriteString("\tconst endpoint = \"")
	sb.WriteString(op.Path)
	sb.WriteString("\"\n")
	
	// Add parameters to request
	if len(op.Parameters) > 0 {
		sb.WriteString("\treq := &struct{\n")
		for _, param := range op.Parameters {
			goType := openAPIToGoType(param.Schema)
			if param.Required {
				sb.WriteString(fmt.Sprintf("\t%s %s `json:\"%s\"`\n", 
					toCamelCase(param.Name), 
					goType, 
					param.Name))
			} else {
				sb.WriteString(fmt.Sprintf("\t%s *%s `json:\"%s,omitempty\"`\n", 
					toCamelCase(param.Name), 
					goType, 
					param.Name))
			}
		}
		sb.WriteString("}\n")
		sb.WriteString(fmt.Sprintf("\treq.%s = ", toCamelCase(op.Parameters[0].Name)))
		sb.WriteString(toCamelCase(op.Parameters[0].Name))
		sb.WriteString("\n")
	}
	
	sb.WriteString("\tvar resp http.Response\n")
	sb.WriteString(fmt.Sprintf("\terr := c.executeRequest(context.Background(), \"%s\", endpoint, req, nil)\n", 
		strings.ToUpper(op.Method)))
	sb.WriteString("\tif err != nil {\n")
	sb.WriteString("\t\treturn nil, err\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\treturn &resp, nil\n")
	
	return sb.String()
}

// generateModels creates Go structs from OpenAPI schema definitions.
func (g *APIClientGenerator) generateModels(spec *OpenAPISpec) string {
	const modelTmpl = `// Model definitions generated from OpenAPI spec.
// DO NOT EDIT - regenerate with m40_client_generator.go

package {{.PackageName}}

{{range $name, $schema := .Schemas}}
// {{or $schema.Description $name}}
type {{$name | pascalCase}} struct {
{{range $propName, $propSchema := $schema.Properties}}
	{{$propName | pascalCase}} {{openAPIToGoType $propSchema}} `+"`"+`json:"{{$propName}}"`+"`"+`
{{end}}}
{{end}}
`
	
	funcTemplate := template.Must(template.New("models").Funcs(g.functions).Parse(modelTmpl))
	var buf bytes.Buffer
	
	modelInfo := struct {
		PackageName string
		Schemas     map[string]SchemaRef
	}{
		PackageName: g.config.PackageName,
		Schemas:     spec.Components.Schemas,
	}
	
	if err := funcTemplate.Execute(&buf, modelInfo); err != nil {
		return "// Error generating models: " + err.Error()
	}
	
	return buf.String()
}

// openAPIToGoType converts OpenAPI schema types to Go types.
func openAPIToGoType(schema SchemaRef) string {
	if schema.Reference != "" {
		// Extract type name from $ref
		parts := strings.Split(schema.Reference, "/")
		return toPascalCase(parts[len(parts)-1])
	}
	
	switch strings.ToLower(schema.Type) {
	case "string":
		if schema.Format == "date-time" || schema.Format == "date" {
			return "time.Time"
		}
		return "string"
	case "integer":
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		elemType := openAPIToGoType(*schema.Items)
		return "[]" + elemType
	case "object":
		if len(schema.Properties) > 0 {
			return "map[string]interface{}"
		}
		return "map[string]interface{}"
	default:
		return "interface{}"
	}
}

// sanitizeToGoIdentifier sanitizes strings to valid Go identifiers.
func sanitizeToGoIdentifier(s string) string {
	result := strings.ReplaceAll(s, "-", "_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, result)
	return result
}

// String conversion utilities.
func toTitleCase(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(string(p[0])) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

func toPascalCase(s string) string {
	return toUpperFirst(toCamelCase(s))
}

func toCamelCase(s string) string {
	parts := strings.Split(s, "_")
	if len(parts) == 0 {
		return s
	}
	
	result := strings.ToLower(parts[0])
	for _, p := range parts[1:] {
		result += toUpperFirst(p)
	}
	
	return result
}

func toSnakeCase(s string) string {
	result := make([]rune, 0, len(s)*2)
	
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 && ((rune(s[i-1]) >= 'a' && rune(s[i-1]) <= 'z') || 
			              (i+1 < len(s) && rune(s[i+1]) >= 'A' && rune(s[i+1]) <= 'Z')) {
				result = append(result, '_')
			}
		}
		result = append(result, r)
	}
	
	return strings.ToLower(string(result))
}

func toLowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

func toUpperFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
