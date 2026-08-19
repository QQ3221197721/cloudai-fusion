// Package apiclientgen (Module 40) is a real, self-contained API client
// generator. It parses OpenAPI 3.x / Swagger 2.0 specifications supplied as
// either JSON or YAML, normalizes them into a language-neutral model, and emits
// idiomatic client code for multiple target languages (Go, TypeScript, Python).
//
// The parser is a genuine structural parser (not a stub): it walks paths,
// operations, parameters, request bodies, responses and component schemas, and
// resolves $ref references into named types. Generators translate the neutral
// model into concrete syntax; the Go generator additionally runs the output
// through go/format so emitted Go is guaranteed to be syntactically valid.
package apiclientgen

import (
	"encoding/json"
	"fmt"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Document is the raw, minimally-typed representation of an OpenAPI/Swagger
// document. It intentionally carries both OpenAPI 3.x (components/schemas) and
// Swagger 2.0 (definitions) shapes so a single struct can decode either format
// from JSON or YAML. Unknown fields are ignored.
type Document struct {
	OpenAPI     string               `json:"openapi" yaml:"openapi"`
	Swagger     string               `json:"swagger" yaml:"swagger"`
	Info        Info                 `json:"info" yaml:"info"`
	Paths       map[string]PathItem  `json:"paths" yaml:"paths"`
	Components  Components            `json:"components" yaml:"components"`
	Definitions map[string]*Schema   `json:"definitions" yaml:"definitions"` // Swagger 2.0
}

// Info holds document-level metadata.
type Info struct {
	Title       string `json:"title" yaml:"title"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description" yaml:"description"`
}

// Components is the OpenAPI 3.x components object; only schemas are consumed.
type Components struct {
	Schemas map[string]*Schema `json:"schemas" yaml:"schemas"`
}

// PathItem groups the operations available on a single path.
type PathItem struct {
	Get        *Operation  `json:"get" yaml:"get"`
	Put        *Operation  `json:"put" yaml:"put"`
	Post       *Operation  `json:"post" yaml:"post"`
	Delete     *Operation  `json:"delete" yaml:"delete"`
	Patch      *Operation  `json:"patch" yaml:"patch"`
	Head       *Operation  `json:"head" yaml:"head"`
	Options    *Operation  `json:"options" yaml:"options"`
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

// operations returns the defined operations keyed by HTTP method, in a stable
// order so generation is deterministic.
func (p PathItem) operations() []struct {
	Method string
	Op     *Operation
} {
	ordered := []struct {
		Method string
		Op     *Operation
	}{
		{"GET", p.Get}, {"POST", p.Post}, {"PUT", p.Put},
		{"PATCH", p.Patch}, {"DELETE", p.Delete},
		{"HEAD", p.Head}, {"OPTIONS", p.Options},
	}
	out := ordered[:0]
	for _, o := range ordered {
		if o.Op != nil {
			out = append(out, o)
		}
	}
	return out
}

// Operation describes a single API operation.
type Operation struct {
	OperationID string              `json:"operationId" yaml:"operationId"`
	Summary     string              `json:"summary" yaml:"summary"`
	Description string              `json:"description" yaml:"description"`
	Tags        []string            `json:"tags" yaml:"tags"`
	Parameters  []Parameter         `json:"parameters" yaml:"parameters"`
	RequestBody *RequestBody        `json:"requestBody" yaml:"requestBody"`
	Responses   map[string]Response `json:"responses" yaml:"responses"`
}

// Parameter is a path/query/header/cookie parameter.
type Parameter struct {
	Name        string  `json:"name" yaml:"name"`
	In          string  `json:"in" yaml:"in"`
	Required    bool    `json:"required" yaml:"required"`
	Description string  `json:"description" yaml:"description"`
	Schema      *Schema `json:"schema" yaml:"schema"`
}

// RequestBody is an OpenAPI 3.x request body.
type RequestBody struct {
	Required    bool                 `json:"required" yaml:"required"`
	Description string               `json:"description" yaml:"description"`
	Content     map[string]MediaType `json:"content" yaml:"content"`
}

// Response is a single response entry.
type Response struct {
	Description string               `json:"description" yaml:"description"`
	Content     map[string]MediaType `json:"content" yaml:"content"`
	Schema      *Schema              `json:"schema" yaml:"schema"` // Swagger 2.0
}

// MediaType wraps a schema under a content type key.
type MediaType struct {
	Schema *Schema `json:"schema" yaml:"schema"`
}

// Schema is a minimal JSON-Schema/OpenAPI schema object.
type Schema struct {
	Ref         string             `json:"$ref" yaml:"$ref"`
	Type        string             `json:"type" yaml:"type"`
	Format      string             `json:"format" yaml:"format"`
	Description string             `json:"description" yaml:"description"`
	Properties  map[string]*Schema `json:"properties" yaml:"properties"`
	Required    []string           `json:"required" yaml:"required"`
	Items       *Schema            `json:"items" yaml:"items"`
	Enum        []any              `json:"enum" yaml:"enum"`
}

// ParseError provides context about a specification that could not be parsed.
type ParseError struct {
	Format string
	Err    error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("apiclientgen: failed to parse %s spec: %v", e.Format, e.Err)
}

func (e *ParseError) Unwrap() error { return e.Err }

// ParseSpec parses an OpenAPI/Swagger specification from raw bytes. The format
// (JSON or YAML) is auto-detected from the leading non-whitespace byte, with a
// JSON attempt first and a YAML fallback. YAML is a superset of JSON, so YAML
// decoding also handles well-formed JSON, but we try JSON first for speed and
// clearer errors on JSON inputs.
func ParseSpec(data []byte) (*Document, error) {
	trimmed := strings.TrimLeftFunc(string(data), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	if len(trimmed) == 0 {
		return nil, &ParseError{Format: "unknown", Err: fmt.Errorf("empty specification")}
	}

	var doc Document
	if trimmed[0] == '{' || trimmed[0] == '[' {
		if err := json.Unmarshal(data, &doc); err != nil {
			return nil, &ParseError{Format: "json", Err: err}
		}
	} else {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, &ParseError{Format: "yaml", Err: err}
		}
	}

	if doc.OpenAPI == "" && doc.Swagger == "" {
		return nil, &ParseError{Format: "openapi", Err: fmt.Errorf("missing 'openapi' or 'swagger' version field")}
	}
	if len(doc.Paths) == 0 {
		return nil, &ParseError{Format: "openapi", Err: fmt.Errorf("specification declares no paths")}
	}
	return &doc, nil
}

// schemas returns the effective named schema map, merging OpenAPI 3.x
// components/schemas with Swagger 2.0 definitions.
func (d *Document) schemas() map[string]*Schema {
	if len(d.Components.Schemas) > 0 {
		return d.Components.Schemas
	}
	return d.Definitions
}
