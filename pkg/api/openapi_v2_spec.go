package api

// OpenAPI v2 specification generator for the CloudAI Fusion API
// This module generates OpenAPI 2.0 (Swagger) compliant specifications

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OpenAPISpec represents the complete OpenAPI v2 specification
type OpenAPISpec struct {
	Swagger          string                 `json:"swagger"`
	Info             Info                   `json:"info"`
	Host             string                 `json:"host,omitempty"`
	BasePath         string                 `json:"basePath,omitempty"`
	Paths            Paths                  `json:"paths"`
	Definitions      map[string]Schema      `json:"definitions,omitempty"`
	ExternalDocs     *ExternalDocumentation `json:"externalDocs,omitempty"`
}

// Info contains metadata about the API
type Info struct {
	Title           string `json:"title"`
	Description       string `json:"description"`
	Version         string `json:"version"`
	TermsOfService  string `json:"termsOfService,omitempty"`
	Contact         *Contact `json:"contact,omitempty"`
	License         *License `json:"license,omitempty"`
}

// Contact information
type Contact struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// License information
type License struct {
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

// Paths stores all API endpoints
type Paths map[string]PathItem

// PathItem defines a single API endpoint
type PathItem struct {
	Get     *Operation `json:"get,omitempty"`
	Post    *Operation `json:"post,omitempty"`
	Put     *Operation `json:"put,omitempty"`
	Delete  *Operation `json:"delete,omitempty"`
	Patch   *Operation `json:"patch,omitempty"`
	Options *Operation `json:"options,omitempty"`
	Head    *Operation `json:"head,omitempty"`
}

// Operation defines an API operation
type Operation struct {
	Summary     string            `json:"summary,omitempty"`
	Description string            `json:"description,omitempty"`
	OperationID string            `json:"operationId"`
	Responses   map[string]Response `json:"responses"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
}

// Response defines an API response
type Response struct {
	Description string              `json:"description"`
	Schema      *Schema             `json:"schema,omitempty"`
	Headers     map[string]Header   `json:"headers,omitempty"`
}

// Header defines a response header
type Header struct {
	Description string    `json:"description,omitempty"`
	Type        string    `json:"type,omitempty"`
	Format      string    `json:"format,omitempty"`
}

// Schema defines a data schema
type Schema struct {
	Type        string              `json:"type,omitempty"`
	Format      string              `json:"format,omitempty"`
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Ref         string              `json:"$ref,omitempty"`
	Items       *Schema             `json:"items,omitempty"`
	Properties  map[string]Schema   `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
}

// ExternalDocumentation represents external documentation reference
type ExternalDocumentation struct {
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// Parameter defines an API parameter
type Parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"` // query, path, header, body
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Type        string `json:"type,omitempty"`
	Format      string `json:"format,omitempty"`
	Schema      *Schema `json:"schema,omitempty"`
}

// GenerateOpenAPISpec returns the complete OpenAPI v2 specification
func GenerateOpenAPISpec() (*OpenAPISpec, error) {
	spec := &OpenAPISpec{
		Swagger:  "2.0",
		Host:     "localhost:8080",
		BasePath: "/api/v1",
		Info: Info{
			Title:       "CloudAI Fusion Platform API",
			Description: "Enterprise-grade AI scheduling and orchestration platform with verifiable control plane evidence",
			Version:     "1.0.0",
		},
		Paths: make(Paths),
		Definitions: make(map[string]Schema),
	}

	// Add common paths
	addAuthEndpoints(spec)
	addClusterEndpoints(spec)
	addSchedulerEndpoints(spec)
	addRedteamEndpoints(spec)
	addEvidenceEndpoints(spec)

	return spec, nil
}

// HandleOpenAPI renders the OpenAPI spec as JSON
func HandleOpenAPI(c *gin.Context) {
	spec, err := GenerateOpenAPISpec()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "application/vnd.oai.openapi+json")
	c.JSON(http.StatusOK, spec)
}

// RegisterOpenAPIRoutes registers OpenAPI routes
func RegisterOpenAPIRoutes(r *gin.Engine) {
	r.GET("/api/v1/openapi.json", HandleOpenAPI)
	r.GET("/openapi.json", HandleOpenAPI)
}

// ToJSON marshals the spec to JSON
func (s *OpenAPISpec) ToJSON(indent bool) ([]byte, error) {
	if indent {
		return json.MarshalIndent(s, "", "  ")
	}
	return json.Marshal(s)
}

func addAuthEndpoints(spec *OpenAPISpec) {
	// TODO: Implement authentication endpoints
}

func addClusterEndpoints(spec *OpenAPISpec) {
	// TODO: Implement cluster management endpoints
}

func addSchedulerEndpoints(spec *OpenAPISpec) {
	// TODO: Implement scheduler endpoints
}

func addRedteamEndpoints(spec *OpenAPISpec) {
	// TODO: Implement red team endpoints
}

func addEvidenceEndpoints(spec *OpenAPISpec) {
	// TODO: Implement evidence chain endpoints
}
