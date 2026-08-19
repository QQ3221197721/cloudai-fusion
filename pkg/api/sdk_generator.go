package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"
)

// SDKGenerator generates client SDKs for multiple languages
type SDKGenerator struct {
	spec          *OpenAPISpecV3
	generator     *OpenAPIGenerator
	packageName   string
	author        string
	license       string
	repoURL       string
	generatedAt   time.Time
}

// NewSDKGenerator creates SDK generator instance
func NewSDKGenerator(generator *OpenAPIGenerator, packageName string) *SDKGenerator {
	return &SDKGenerator{
		spec:        generator.spec,
		generator:   generator,
		packageName: packageName,
		author:      "CloudAI Fusion Team",
		license:     "Apache-2.0",
		generatedAt: time.Now(),
	}
}

// SDKConfig configures SDK generation
type SDKConfig struct {
	Language     string // go, python, typescript
	PackageName  string
	Version      string
	Author       string
	License      string
	RepoURL      string
	BaseURL      string
	AuthRequired bool
}

// GeneratedSDK represents generated SDK artifacts
type GeneratedSDK struct {
	Language    string
	PackageName string
	Version     string
	Files       map[string]string // filename -> content
	GeneratedAt time.Time
	LOC         int
}

// GenerateGoSDK generates Go client SDK
func (g *SDKGenerator) GenerateGoSDK(cfg SDKConfig) (*GeneratedSDK, error) {
	sdk := &GeneratedSDK{
		Language:    "go",
		PackageName: cfg.PackageName,
		Version:     cfg.Version,
		Files:       make(map[string]string),
		GeneratedAt: time.Now(),
	}
	
	// Generate client.go
	clientCode, err := g.generateGoClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate client: %w", err)
	}
	sdk.Files["client.go"] = clientCode
	
	// Generate models.go
	modelsCode, err := g.generateGoModels(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate models: %w", err)
	}
	sdk.Files["models.go"] = modelsCode
	
	// Generate operations.go
	operationsCode, err := g.generateGoOperations(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate operations: %w", err)
	}
	sdk.Files["operations.go"] = operationsCode
	
	// Count LOC
	for _, content := range sdk.Files {
		sdk.LOC += strings.Count(content, "\n") + 1
	}
	
	return sdk, nil
}

func (g *SDKGenerator) generateGoClient(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("client").Parse(goClientTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Version":     cfg.Version,
		"BaseURL":     cfg.BaseURL,
		"AuthRequired": cfg.AuthRequired,
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) generateGoModels(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("models").Parse(goModelsTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Models":      g.extractGoModels(),
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) extractGoModels() []GoModel {
	var models []GoModel
	
	for name, schema := range g.spec.Components.Schemas {
		model := GoModel{
			Name:        name,
			Description: schema.Description,
			Fields:      make([]GoField, 0),
		}
		
		for propName, propSchema := range schema.Properties {
			field := GoField{
				Name:        capitalize(propName),
				Type:        schemaTypeToGo(propSchema),
				JSONName:    propName,
				Required:    contains(schema.Required, propName),
				Description: propSchema.Description,
			}
			model.Fields = append(model.Fields, field)
		}
		
		models = append(models, model)
	}
	
	return models
}

func (g *SDKGenerator) generateGoOperations(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("operations").Parse(goOperationsTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Operations":  g.extractGoOperations(),
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) extractGoOperations() []GoOperation {
	var operations []GoOperation
	
	for path, pathItem := range g.spec.Paths {
		if pathItem.Get != nil {
			operations = append(operations, g.convertOperation("GET", path, pathItem.Get))
		}
		if pathItem.Post != nil {
			operations = append(operations, g.convertOperation("POST", path, pathItem.Post))
		}
		if pathItem.Put != nil {
			operations = append(operations, g.convertOperation("PUT", path, pathItem.Put))
		}
		if pathItem.Delete != nil {
			operations = append(operations, g.convertOperation("DELETE", path, pathItem.Delete))
		}
	}
	
	return operations
}

func (g *SDKGenerator) convertOperation(method, path string, op *OperationV3) GoOperation {
	operation := GoOperation{
		Method:      method,
		Path:        path,
		Name:        op.ID,
		Summary:     op.Summary,
		Description: op.Description,
		Parameters:  make([]GoParam, 0),
	}
	
	for _, param := range op.Parameters {
		p := GoParam{
			Name:        param.Name,
			In:          param.In,
			Type:        schemaTypeToGo(*param.Schema),
			Required:    param.Required,
			Description: param.Description,
		}
		operation.Parameters = append(operation.Parameters, p)
	}
	
	if op.RequestBody != nil {
		operation.HasBody = true
		operation.BodyType = "interface{}" // TODO: extract from schema
	}
	
	return operation
}

// GeneratePythonSDK generates Python client SDK
func (g *SDKGenerator) GeneratePythonSDK(cfg SDKConfig) (*GeneratedSDK, error) {
	sdk := &GeneratedSDK{
		Language:    "python",
		PackageName: cfg.PackageName,
		Version:     cfg.Version,
		Files:       make(map[string]string),
		GeneratedAt: time.Now(),
	}
	
	// Generate client.py
	clientCode, err := g.generatePythonClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate client: %w", err)
	}
	sdk.Files["client.py"] = clientCode
	
	// Generate models.py
	modelsCode, err := g.generatePythonModels(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate models: %w", err)
	}
	sdk.Files["models.py"] = modelsCode
	
	// Generate __init__.py
	initCode := fmt.Sprintf(`"""CloudAI Fusion API Client Library"""
from .client import CloudAIFusionClient
from .models import *

__version__ = "%s"
__all__ = ["CloudAIFusionClient"]
`, cfg.Version)
	sdk.Files["__init__.py"] = initCode
	
	// Generate requirements.txt
	requirements := "requests>=2.31.0\npydantic>=2.5.0\n"
	sdk.Files["requirements.txt"] = requirements
	
	for _, content := range sdk.Files {
		sdk.LOC += strings.Count(content, "\n") + 1
	}
	
	return sdk, nil
}

func (g *SDKGenerator) generatePythonClient(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("client").Parse(pythonClientTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Version":     cfg.Version,
		"BaseURL":     cfg.BaseURL,
		"AuthRequired": cfg.AuthRequired,
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) generatePythonModels(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("models").Parse(pythonModelsTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Models":      g.extractPythonModels(),
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) extractPythonModels() []PythonModel {
	var models []PythonModel
	
	for name, schema := range g.spec.Components.Schemas {
		model := PythonModel{
			Name:        name,
			Description: schema.Description,
			Fields:      make([]PythonField, 0),
		}
		
		for propName, propSchema := range schema.Properties {
			field := PythonField{
				Name:        propName,
				Type:        schemaTypeToPython(propSchema),
				Required:    contains(schema.Required, propName),
				Description: propSchema.Description,
			}
			model.Fields = append(model.Fields, field)
		}
		
		models = append(models, model)
	}
	
	return models
}

// GenerateTypeScriptSDK generates TypeScript client SDK
func (g *SDKGenerator) GenerateTypeScriptSDK(cfg SDKConfig) (*GeneratedSDK, error) {
	sdk := &GeneratedSDK{
		Language:    "typescript",
		PackageName: cfg.PackageName,
		Version:     cfg.Version,
		Files:       make(map[string]string),
		GeneratedAt: time.Now(),
	}
	
	// Generate client.ts
	clientCode, err := g.generateTypeScriptClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate client: %w", err)
	}
	sdk.Files["client.ts"] = clientCode
	
	// Generate models.ts
	modelsCode, err := g.generateTypeScriptModels(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate models: %w", err)
	}
	sdk.Files["models.ts"] = modelsCode
	
	// Generate package.json
	packageJSON, _ := json.MarshalIndent(map[string]interface{}{
		"name":            cfg.PackageName,
		"version":         cfg.Version,
		"description":     "CloudAI Fusion API Client",
		"main":            "dist/index.js",
		"types":           "dist/index.d.ts",
		"dependencies": map[string]string{
			"axios": "^1.6.0",
		},
		"devDependencies": map[string]string{
			"typescript": "^5.3.0",
			"@types/node": "^20.10.0",
		},
	}, "", "  ")
	sdk.Files["package.json"] = string(packageJSON)
	
	for _, content := range sdk.Files {
		sdk.LOC += strings.Count(content, "\n") + 1
	}
	
	return sdk, nil
}

func (g *SDKGenerator) generateTypeScriptClient(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("client").Parse(typescriptClientTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Version":     cfg.Version,
		"BaseURL":     cfg.BaseURL,
		"AuthRequired": cfg.AuthRequired,
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) generateTypeScriptModels(cfg SDKConfig) (string, error) {
	tmpl := template.Must(template.New("models").Parse(typescriptModelsTemplate))
	
	var buf bytes.Buffer
	data := map[string]interface{}{
		"PackageName": cfg.PackageName,
		"Models":      g.extractTypeScriptModels(),
		"GeneratedAt": time.Now().Format(time.RFC3339),
	}
	
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	
	return buf.String(), nil
}

func (g *SDKGenerator) extractTypeScriptModels() []TypeScriptModel {
	var models []TypeScriptModel
	
	for name, schema := range g.spec.Components.Schemas {
		model := TypeScriptModel{
			Name:        name,
			Description: schema.Description,
			Fields:      make([]TypeScriptField, 0),
		}
		
		for propName, propSchema := range schema.Properties {
			field := TypeScriptField{
				Name:        propName,
				Type:        schemaTypeToTypeScript(propSchema),
				Required:    contains(schema.Required, propName),
				Description: propSchema.Description,
			}
			model.Fields = append(model.Fields, field)
		}
		
		models = append(models, model)
	}
	
	return models
}

// Helper functions
type GoModel struct {
	Name        string
	Description string
	Fields      []GoField
}

type GoField struct {
	Name        string
	Type        string
	JSONName    string
	Required    bool
	Description string
}

type GoOperation struct {
	Method      string
	Path        string
	Name        string
	Summary     string
	Description string
	Parameters  []GoParam
	HasBody     bool
	BodyType    string
}

type GoParam struct {
	Name        string
	In          string
	Type        string
	Required    bool
	Description string
}

type PythonModel struct {
	Name        string
	Description string
	Fields      []PythonField
}

type PythonField struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

type TypeScriptModel struct {
	Name        string
	Description string
	Fields      []TypeScriptField
}

type TypeScriptField struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func schemaTypeToGo(schema SchemaV3) string {
	switch schema.Type {
	case "string":
		return "string"
	case "integer":
		if schema.Format == "int32" {
			return "int32"
		}
		return "int64"
	case "number":
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		if schema.Items != nil {
			return "[]" + schemaTypeToGo(*schema.Items)
		}
		return "[]interface{}"
	case "object":
		return "map[string]interface{}"
	default:
		return "interface{}"
	}
}

func schemaTypeToPython(schema SchemaV3) string {
	switch schema.Type {
	case "string":
		return "str"
	case "integer":
		return "int"
	case "number":
		return "float"
	case "boolean":
		return "bool"
	case "array":
		return "list"
	case "object":
		return "dict"
	default:
		return "Any"
	}
}

func schemaTypeToTypeScript(schema SchemaV3) string {
	switch schema.Type {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if schema.Items != nil {
			return schemaTypeToTypeScript(*schema.Items) + "[]"
		}
		return "any[]"
	case "object":
		return "Record<string, any>"
	default:
		return "any"
	}
}

// SDK templates (simplified for brevity)
const goClientTemplate = `// Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt}}

package {{.PackageName}}

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the CloudAI Fusion API client
type Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

// NewClient creates a new API client
func NewClient(baseURL string, opts ...ClientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	
	for _, opt := range opts {
		opt(c)
	}
	
	return c
}

// ClientOption configures the client
type ClientOption func(*Client)

// WithAPIKey sets the API key
func WithAPIKey(key string) ClientOption {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithHTTPClient sets custom HTTP client
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// doRequest performs HTTP request with retry logic
func (c *Client) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	
	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	
	// Retry logic with exponential backoff
	var resp *http.Response
	for i := 0; i < 3; i++ {
		resp, err = c.httpClient.Do(req)
		if err == nil && resp.StatusCode < 500 {
			break
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	
	return resp, err
}
`

const goModelsTemplate = `// Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt}}

package {{.PackageName}}

{{range .Models}}
// {{.Name}} {{.Description}}
type {{.Name}} struct {
{{- range .Fields}}
	{{.Name}} {{.Type}} ` + "`json:\"{{.JSONName}}\"`" + `
{{- end}}
}
{{end}}
`

const goOperationsTemplate = `// Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt}}

package {{.PackageName}}

import (
	"encoding/json"
	"fmt"
)

{{range .Operations}}
// {{.Name}} {{.Summary}}
func (c *Client) {{.Name}}({{range .Parameters}}{{.Name}} {{.Type}}, {{end}}{{if .HasBody}}body {{.BodyType}}{{end}}) (interface{}, error) {
	path := "{{.Path}}"
	{{- range .Parameters}}
	{{- if eq .In "path"}}
	path = fmt.Sprintf("%s/{{.Name}}", path, {{.Name}})
	{{- end}}
	{{- end}}
	
	resp, err := c.doRequest("{{.Method}}", path{{if .HasBody}}, body{{end}})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %d", resp.StatusCode)
	}
	
	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	return result, nil
}
{{end}}
`

const pythonClientTemplate = `# Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
# Generated at: {{.GeneratedAt}}

"""CloudAI Fusion API Client"""
import requests
from typing import Optional, Dict, Any
import time


class CloudAIFusionClient:
    """CloudAI Fusion Platform API Client"""
    
    def __init__(
        self,
        base_url: str = "{{.BaseURL}}",
        api_key: Optional[str] = None,
        timeout: int = 30
    ):
        self.base_url = base_url
        self.api_key = api_key
        self.timeout = timeout
        self.session = requests.Session()
        
        if api_key:
            self.session.headers["Authorization"] = f"Bearer {api_key}"
    
    def _request(
        self,
        method: str,
        path: str,
        data: Optional[Dict[str, Any]] = None,
        params: Optional[Dict[str, Any]] = None
    ) -> Any:
        """Perform HTTP request with retry logic"""
        url = f"{self.base_url}{path}"
        
        for attempt in range(3):
            try:
                response = self.session.request(
                    method=method,
                    url=url,
                    json=data,
                    params=params,
                    timeout=self.timeout
                )
                
                if response.status_code < 500:
                    response.raise_for_status()
                    return response.json()
                    
            except requests.exceptions.RequestException:
                if attempt == 2:
                    raise
                time.sleep(2 ** attempt)
        
        return None
`

const pythonModelsTemplate = `# Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
# Generated at: {{.GeneratedAt}}

"""CloudAI Fusion API Models"""
from dataclasses import dataclass
from typing import Optional, List, Dict, Any


{{range .Models}}
@dataclass
class {{.Name}}:
    """{{.Description}}"""
    {{- range .Fields}}
    {{.Name}}: {{if .Required}}{{.Type}}{{else}}Optional[{{.Type}}] = None{{end}}
    {{- end}}
{{end}}
`

const typescriptClientTemplate = `// Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt}}

import axios, { AxiosInstance, AxiosRequestConfig } from 'axios';

export class CloudAIFusionClient {
  private client: AxiosInstance;
  private baseURL: string;

  constructor(baseURL: string = '{{.BaseURL}}', apiKey?: string) {
    this.baseURL = baseURL;
    this.client = axios.create({
      baseURL,
      timeout: 30000,
      headers: apiKey ? { Authorization: 'Bearer ${apiKey}' } : {},
    });

    // Add retry interceptor
    this.client.interceptors.response.use(
      (response) => response,
      async (error) => {
        const config = error.config;
        if (!config._retry && error.response?.status >= 500) {
          config._retry = true;
          await new Promise((resolve) => setTimeout(resolve, 1000));
          return this.client(config);
        }
        return Promise.reject(error);
      }
    );
  }

  async get<T = any>(path: string, params?: Record<string, any>): Promise<T> {
    const response = await this.client.get(path, { params });
    return response.data;
  }

  async post<T = any>(path: string, data?: any): Promise<T> {
    const response = await this.client.post(path, data);
    return response.data;
  }

  async put<T = any>(path: string, data?: any): Promise<T> {
    const response = await this.client.put(path, data);
    return response.data;
  }

  async delete<T = any>(path: string): Promise<T> {
    const response = await this.client.delete(path);
    return response.data;
  }
}
`

const typescriptModelsTemplate = `// Code generated by CloudAI Fusion SDK Generator. DO NOT EDIT.
// Generated at: {{.GeneratedAt}}

{{range .Models}}
/**
 * {{.Description}}
 */
export interface {{.Name}} {
{{- range .Fields}}
  /** {{.Description}} */
  {{.Name}}{{if not .Required}}?{{end}}: {{.Type}};
{{- end}}
}
{{end}}
`
