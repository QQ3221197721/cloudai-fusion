package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// OpenAPIGenerator auto-generates OpenAPI 3.1 specifications from registered handlers
type OpenAPIGenerator struct {
	spec         *OpenAPISpecV3
	mu           sync.RWMutex
	baseURL      string
	apiVersion   string
	description  string
	authSchemes  map[string]*SecurityScheme
	registeredAt time.Time
}

// OpenAPISpecV3 represents OpenAPI 3.1 specification
type OpenAPISpecV3 struct {
	OpenAPI      string                   `json:"openapi"`
	Info         Info                     `json:"info"`
	Servers      []Server                 `json:"servers,omitempty"`
	Paths        PathsV3                  `json:"paths"`
	Components   Components               `json:"components"`
	Security     []SecurityRequirement    `json:"security,omitempty"`
	Tags         []Tag                    `json:"tags,omitempty"`
}

// Server defines API server URL
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Components holds reusable components
type Components struct {
	Schemas      map[string]SchemaV3   `json:"schemas"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes"`
	Responses    map[string]*ResponseV3 `json:"responses,omitempty"`
	Headers      map[string]*HeaderV3   `json:"headers,omitempty"`
}

// SecurityScheme defines authentication scheme
type SecurityScheme struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`      // For header/query auth
	In          string `json:"in,omitempty"`        // For header/query auth
	BearerFormat string `json:"bearerFormat,omitempty"` // For HTTP bearer
}

// SecurityRequirement defines required security schemes
type SecurityRequirement struct {
	SchemeName []string `json:"scheme_name"`
}

// SchemaV3 defines OpenAPI 3.1 schema with better support
type SchemaV3 struct {
	Type              string            `json:"type,omitempty"`
	Title             string            `json:"title,omitempty"`
	Description       string            `json:"description,omitempty"`
	Format            string            `json:"format,omitempty"`
	Default           interface{}       `json:"default,omitempty"`
	Example           interface{}       `json:"example,omitempty"`
	Items             *SchemaV3         `json:"items,omitempty"`
	Properties        map[string]SchemaV3 `json:"properties,omitempty"`
	Required          []string          `json:"required,omitempty"`
	Ref               string            `json:"$ref,omitempty"`
	OneOf             []SchemaV3        `json:"oneOf,omitempty"`
	AllOf             []SchemaV3        `json:"allOf,omitempty"`
	Enum              []interface{}     `json:"enum,omitempty"`
	Pattern           string            `json:"pattern,omitempty"`
	MinLength         int               `json:"minLength,omitempty"`
	MaxLength         int               `json:"maxLength,omitempty"`
	Minimum           float64           `json:"minimum,omitempty"`
	Maximum           float64           `json:"maximum,omitempty"`
	ExclusiveMinimum  bool              `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum  bool              `json:"exclusiveMaximum,omitempty"`
	Nullable          bool              `json:"nullable,omitempty"`
	AdditionalProperties *SchemaV3      `json:"additionalProperties,omitempty"`
}

// PathV3 defines a path item in OpenAPI 3.1
type PathItemV3 struct {
	Ref           string            `json:"$ref,omitempty"`
	Description   string            `json:"description,omitempty"`
	Server        *Server           `json:"server,omitempty"`
	Summary       string            `json:"summary,omitempty"`
	Get           *OperationV3      `json:"get,omitempty"`
	Put           *OperationV3      `json:"put,omitempty"`
	Post          *OperationV3      `json:"post,omitempty"`
	Delete        *OperationV3      `json:"delete,omitempty"`
	Options       *OperationV3      `json:"options,omitempty"`
	Head          *OperationV3      `json:"head,omitempty"`
	Patch         *OperationV3      `json:"patch,omitempty"`
	Trace         *OperationV3      `json:"trace,omitempty"`
	Servers       []*Server         `json:"servers,omitempty"`
	Parameters    []ParameterV3     `json:"parameters,omitempty"`
}

// OperationV3 defines an operation
type OperationV3 struct {
	Summary       string            `json:"summary,omitempty"`
	Description   string            `json:"description,omitempty"`
	ID            string            `json:"operationId,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Parameters    []ParameterV3     `json:"parameters,omitempty"`
	RequestBody   *RequestBodyV3    `json:"requestBody,omitempty"`
	Responses     ResponsesV3       `json:"responses"`
	Callbacks     map[string]CallbackV3 `json:"callbacks,omitempty"`
	Deprecated    bool              `json:"deprecated,omitempty"`
	Security      []SecurityRequirement `json:"security,omitempty"`
	ExternalDocs  *ExternalDocsV3   `json:"externalDocs,omitempty"`
}

// ParameterV3 defines operation parameter
type ParameterV3 struct {
	Name            string       `json:"name"`
	In              string       `json:"in"` // query, path, header, cookie
	Description     string       `json:"description,omitempty"`
	Required        bool         `json:"required,omitempty"`
	Deprecated      bool         `json:"deprecated,omitempty"`
	Schema          *SchemaV3    `json:"schema,omitempty"`
	Example         interface{}  `json:"example,omitempty"`
	Examples        map[string]ExampleV3 `json:"examples,omitempty"`
	Content         map[string]MediaTypeV3 `json:"content,omitempty"`
	Style           string       `json:"style,omitempty"`
	Explode         bool         `json:"explode,omitempty"`
}

// ExampleV3 defines an example
type ExampleV3 struct {
	Summary     string      `json:"summary,omitempty"`
	Description string      `json:"description,omitempty"`
	Value       interface{} `json:"value,omitempty"`
	ExternalURL string      `json:"externalValue,omitempty"`
}

// MediaTypeV3 defines media type
type MediaTypeV3 struct {
	Schema  *SchemaV3    `json:"schema,omitempty"`
	Example interface{}  `json:"example,omitempty"`
	Examples map[string]ExampleV3 `json:"examples,omitempty"`
}

// RequestBodyV3 defines request body
type RequestBodyV3 struct {
	Description string                `json:"description,omitempty"`
	Content     map[string]MediaTypeV3 `json:"content"`
	Required    bool                  `json:"required,omitempty"`
}

// ResponsesV3 defines responses
type ResponsesV3 map[string]*ResponseV3

// ResponseV3 defines response
type ResponseV3 struct {
	Description string                        `json:"description"`
	Headers     map[string]*HeaderV3          `json:"headers,omitempty"`
	Content     map[string]MediaTypeV3        `json:"content,omitempty"`
	Links       map[string]*LinkV3            `json:"links,omitempty"`
}

// HeaderV3 defines response header
type HeaderV3 struct {
	Description     string       `json:"description,omitempty"`
	Schema          *SchemaV3    `json:"schema,omitempty"`
	Example         interface{}  `json:"example,omitempty"`
	Examples        map[string]ExampleV3 `json:"examples,omitempty"`
	Deprecated      bool         `json:"deprecated,omitempty"`
}

// LinkV3 defines link
type LinkV3 struct {
	Description     string       `json:"description,omitempty"`
	OperationRef    string       `json:"operationRef,omitempty"`
	OperationID     string       `json:"operationId,omitempty"`
	Parameters      map[string]interface{} `json:"parameters,omitempty"`
	RequestBody     interface{}  `json:"requestBody,omitempty"`
}

// ExternalDocsV3 defines external documentation
type ExternalDocsV3 struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// CallbackV3 defines callback
type CallbackV3 map[string]*PathItemV3

// Tag defines API tag
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ExternalDoc *ExternalDocsV3 `json:"externalDocs,omitempty"`
}

// NewOpenAPIGenerator creates generator instance
func NewOpenAPIGenerator(baseURL, apiVersion, description string) *OpenAPIGenerator {
	return &OpenAPIGenerator{
		spec: &OpenAPISpecV3{
			OpenAPI: "3.1.0",
			Info: Info{
				Title:       "CloudAI Fusion Platform API",
				Description: description,
				Version:     apiVersion,
			},
			Servers: []Server{{URL: baseURL}},
			Paths:   make(PathsV3),
			Components: Components{
				Schemas:     make(map[string]SchemaV3),
				SecuritySchemes: make(map[string]*SecurityScheme),
				Responses:   make(map[string]*ResponseV3),
				Headers:     make(map[string]*HeaderV3),
			},
		},
		authSchemes: make(map[string]*SecurityScheme),
		registeredAt: time.Now(),
	}
}

// AddSecurityScheme registers authentication scheme
func (g *OpenAPIGenerator) AddSecurityScheme(name string, scheme *SecurityScheme) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.authSchemes[name] = scheme
	g.spec.Components.SecuritySchemes[name] = scheme
}

// RegisterRoute adds route to specification
func (g *OpenAPIGenerator) RegisterRoute(method, path string, op *OperationV3) {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.spec.Paths == nil {
		g.spec.Paths = make(PathsV3)
	}
	
	if _, exists := g.spec.Paths[path]; !exists {
		g.spec.Paths[path] = &PathItemV3{}
	}
	
	switch method {
	case http.MethodGet:
		g.spec.Paths[path].Get = op
	case http.MethodPost:
		g.spec.Paths[path].Post = op
	case http.MethodPut:
		g.spec.Paths[path].Put = op
	case http.MethodDelete:
		g.spec.Paths[path].Delete = op
	case http.MethodPatch:
		g.spec.Paths[path].Patch = op
	}
}

// GenerateSpec returns complete OpenAPI spec
func (g *OpenAPIGenerator) GenerateSpec() (*OpenAPISpecV3, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	
	// Sort paths for consistent output
	sortedPaths := make([]string, 0, len(g.spec.Paths))
	for path := range g.spec.Paths {
		sortedPaths = append(sortedPaths, path)
	}
	
	return g.spec, nil
}

// HandleSpec renders OpenAPI spec as JSON with performance optimization
func (g *OpenAPIGenerator) HandleSpec(c *gin.Context) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	
	specBytes, err := json.MarshalIndent(g.spec, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	c.Header("Content-Type", "application/vnd.oai.openapi+json")
	c.Data(http.StatusOK, "application/vnd.oai.openapi+json", specBytes)
}

// ReflectOnType generates schema from Go type using reflection with caching
func (g *OpenAPIGenerator) ReflectOnType(name string, typ reflect.Type) SchemaV3 {
	key := fmt.Sprintf("%s.%s", name, typ.String())
	
	g.mu.RLock()
	existing, cached := g.spec.Components.Schemas[key]
	g.mu.RUnlock()
	
	if cached {
		return existing
	}
	
	g.mu.Lock()
	defer g.mu.Unlock()
	
	schema := g.reflectType(typ)
	g.spec.Components.Schemas[key] = schema
	
	return schema
}

func (g *OpenAPIGenerator) reflectType(t reflect.Type) SchemaV3 {
	schema := SchemaV3{}
	
	switch t.Kind() {
	case reflect.Ptr:
		if t.Elem().Kind() >= reflect.Int && t.Elem().Kind() <= reflect.Uintptr {
			return SchemaV3{Type: "integer", Nullable: true}
		}
		return g.reflectType(t.Elem())
		
	case reflect.Struct:
		name := getJSONTagName(t)
		if name != "" {
			schema.Title = name
		}
		
		if t.NumField() > 0 {
			schema.Type = "object"
			schema.Properties = make(map[string]SchemaV3)
			
			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				
				// Skip unexported fields
				if field.PkgPath != "" {
					continue
				}
				
				propName := getFieldJSONName(field)
				if propName == "-" {
					continue
				}
				
				property := g.reflectType(field.Type)
				property.Description = field.Tag.Get("description")
				
				// Parse validate tags
				if val := field.Tag.Get("validate"); val != "" {
					parts := strings.Split(val, ",")
					for _, part := range parts {
						kv := strings.Split(part, "=")
						switch kv[0] {
						case "required":
							if schema.Required == nil {
								schema.Required = []string{}
							}
							schema.Required = append(schema.Required, propName)
						case "min:", "max:":
							// Parse numeric constraints
						case "min_len:", "max_len:":
							// Parse string length constraints
						}
					}
				}
				
				schema.Properties[propName] = property
			}
		}
		
	case reflect.Slice, reflect.Array:
		itemSchema := g.reflectType(t.Elem())
		schema.Type = "array"
		schema.Items = &itemSchema
		
	case reflect.Map:
		schema.Type = "object"
		schema.AdditionalProperties = &SchemaV3{
			Type: "string",
		}
		
	case reflect.String:
		schema.Type = "string"
		
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
		schema.Format = "int64"
		
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
		schema.Format = "uint64"
		
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
		schema.Format = "double"
		
	case reflect.Bool:
		schema.Type = "boolean"
		
	default:
		schema.Type = "string"
	}
	
	return schema
}

// Helper functions for schema reflection
func getJSONTagName(t reflect.Type) string {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag != "" {
			parts := strings.Split(tag, ",")
			return parts[0]
		}
	}
	return ""
}

func getFieldJSONName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	
	parts := strings.Split(tag, ",")
	name := parts[0]
	
	if name == "" {
		name = f.Name
	}
	
	return name
}

// PathsV3 maps path strings to PathItem
type PathsV3 map[string]*PathItemV3

// WithExample adds example data to schema
func (s *SchemaV3) WithExample(value interface{}) *SchemaV3 {
	s.Example = value
	return s
}

// WithPattern adds regex pattern constraint
func (s *SchemaV3) WithPattern(pattern string) *SchemaV3 {
	s.Pattern = pattern
	return s
}

// WithEnum adds enum values
func (s *SchemaV3) WithEnum(values ...interface{}) *SchemaV3 {
	s.Enum = values
	return s
}

// AddToDefinitions adds schema to API definitions
func (g *OpenAPIGenerator) AddToDefinitions(name string, schema SchemaV3) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.spec.Components.Schemas == nil {
		g.spec.Components.Schemas = make(map[string]SchemaV3)
	}
	g.spec.Components.Schemas[name] = schema
}

// ToPrettyJSON marshals spec with indentation for readability
func (g *OpenAPIGenerator) ToPrettyJSON() ([]byte, error) {
	return json.MarshalIndent(g.spec, "", "  ")
}

// GetRegisteredAt returns when the generator was first created
func (g *OpenAPIGenerator) GetRegisteredAt() time.Time {
	return g.registeredAt
}

// UpdateBaseURL updates the base server URL dynamically
func (g *OpenAPIGenerator) UpdateBaseURL(newURL string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.spec.Servers) > 0 {
		g.spec.Servers[0].URL = newURL
	} else {
		g.spec.Servers = []Server{{URL: newURL}}
	}
}

// GenerateSummary returns quick summary of API surface area
func (g *OpenAPIGenerator) GenerateSummary() map[string]interface{} {
	g.mu.RLock()
	defer g.mu.RUnlock()
	
	pathCount := len(g.spec.Paths)
	definitionCount := len(g.spec.Components.Schemas)
	
	return map[string]interface{}{
		"openapi_version": g.spec.OpenAPI,
		"base_url":        g.spec.Servers[0].URL,
		"title":           g.spec.Info.Title,
		"version":         g.spec.Info.Version,
		"path_count":      pathCount,
		"definition_count": definitionCount,
		"registered_at":   g.registeredAt.Format(time.RFC3339),
	}
}
