// Package apiclientgen model normalization: converts a raw parsed Document into
// a language-neutral Model that generators translate into concrete code.
package apiclientgen

import (
	"sort"
	"strings"
	"unicode"
)

// Model is the normalized, language-neutral representation of an API. It is the
// single input consumed by every code generator.
type Model struct {
	Title       string
	Version     string
	Description string
	Types       []TypeDef // named schemas, sorted by name
	Operations  []OpDef   // flattened operations, sorted by ID
}

// TypeDef is a named type derived from a component schema.
type TypeDef struct {
	Name        string
	Description string
	Fields      []FieldDef // populated when the type is an object
	Enum        []string   // populated when the type is a string enum
	Alias       *TypeRef   // populated when the type is an alias (array/primitive)
}

// IsObject reports whether the type is a struct-like object.
func (t TypeDef) IsObject() bool { return len(t.Fields) > 0 || (t.Alias == nil && len(t.Enum) == 0) }

// IsEnum reports whether the type is a string enumeration.
func (t TypeDef) IsEnum() bool { return len(t.Enum) > 0 }

// FieldDef is a single object field.
type FieldDef struct {
	Name        string // original property name (wire name)
	Type        *TypeRef
	Required    bool
	Description string
}

// OpDef is a normalized operation.
type OpDef struct {
	ID                  string // operationId or synthesized method+path identifier
	Method              string // GET, POST, ...
	Path                string // /pets/{petId}
	Summary             string
	Description         string
	PathParams          []ParamDef
	QueryParams         []ParamDef
	HeaderParams        []ParamDef
	RequestBody         *TypeRef
	RequestBodyRequired bool
	Success             *TypeRef // 2xx response body type, nil for no content
	SuccessCode         string
}

// ParamDef is a normalized parameter.
type ParamDef struct {
	Name        string
	Type        *TypeRef
	Required    bool
	Description string
}

// TypeRef is a language-neutral type reference. Exactly one classification
// applies, resolved via the Kind field.
type TypeRef struct {
	Kind      TypeKind
	Primitive string   // for KindPrimitive: string|integer|number|boolean
	Format    string   // OpenAPI format hint (int64, date-time, ...)
	RefName   string   // for KindRef: the named type
	Elem      *TypeRef // for KindArray/KindMap
}

// TypeKind enumerates neutral type classifications.
type TypeKind int

const (
	// KindAny is an unconstrained value (object without properties, missing type).
	KindAny TypeKind = iota
	// KindPrimitive is a scalar (string/number/integer/boolean).
	KindPrimitive
	// KindRef references a named type.
	KindRef
	// KindArray is a homogeneous list.
	KindArray
	// KindMap is a string-keyed map.
	KindMap
)

// BuildModel normalizes a parsed Document into a Model.
func BuildModel(doc *Document) *Model {
	m := &Model{
		Title:       strings.TrimSpace(doc.Info.Title),
		Version:     strings.TrimSpace(doc.Info.Version),
		Description: strings.TrimSpace(doc.Info.Description),
	}
	if m.Title == "" {
		m.Title = "API"
	}

	// Named types from components/definitions.
	schemas := doc.schemas()
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m.Types = append(m.Types, buildTypeDef(name, schemas[name]))
	}

	// Flatten operations across all paths.
	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		item := doc.Paths[p]
		for _, mo := range item.operations() {
			m.Operations = append(m.Operations, buildOpDef(p, mo.Method, mo.Op, item.Parameters))
		}
	}
	sort.SliceStable(m.Operations, func(i, j int) bool {
		return m.Operations[i].ID < m.Operations[j].ID
	})
	return m
}

func buildTypeDef(name string, s *Schema) TypeDef {
	td := TypeDef{Name: name, Description: strings.TrimSpace(s.Description)}
	switch {
	case len(s.Enum) > 0 && (s.Type == "string" || s.Type == ""):
		for _, e := range s.Enum {
			if str, ok := e.(string); ok {
				td.Enum = append(td.Enum, str)
			}
		}
		if len(td.Enum) > 0 {
			return td
		}
		fallthrough
	case s.Type == "object" || len(s.Properties) > 0:
		req := make(map[string]bool, len(s.Required))
		for _, r := range s.Required {
			req[r] = true
		}
		propNames := make([]string, 0, len(s.Properties))
		for pn := range s.Properties {
			propNames = append(propNames, pn)
		}
		sort.Strings(propNames)
		for _, pn := range propNames {
			td.Fields = append(td.Fields, FieldDef{
				Name:        pn,
				Type:        resolveType(s.Properties[pn]),
				Required:    req[pn],
				Description: strings.TrimSpace(s.Properties[pn].Description),
			})
		}
	default:
		// Alias for arrays / scalars.
		td.Alias = resolveType(s)
	}
	return td
}

func buildOpDef(path, method string, op *Operation, pathLevelParams []Parameter) OpDef {
	def := OpDef{
		Method:      method,
		Path:        path,
		Summary:     strings.TrimSpace(op.Summary),
		Description: strings.TrimSpace(op.Description),
	}
	def.ID = operationID(op.OperationID, method, path)

	params := append([]Parameter{}, pathLevelParams...)
	params = append(params, op.Parameters...)
	for _, p := range params {
		pd := ParamDef{
			Name:        p.Name,
			Type:        resolveType(p.Schema),
			Required:    p.Required,
			Description: strings.TrimSpace(p.Description),
		}
		switch p.In {
		case "path":
			pd.Required = true
			def.PathParams = append(def.PathParams, pd)
		case "query":
			def.QueryParams = append(def.QueryParams, pd)
		case "header":
			def.HeaderParams = append(def.HeaderParams, pd)
		}
	}

	if op.RequestBody != nil {
		def.RequestBodyRequired = op.RequestBody.Required
		if s := preferredSchema(op.RequestBody.Content); s != nil {
			def.RequestBody = resolveType(s)
		}
	}

	// Pick the lowest 2xx response as the success body.
	def.SuccessCode, def.Success = successResponse(op.Responses)
	return def
}

func successResponse(responses map[string]Response) (string, *TypeRef) {
	codes := make([]string, 0, len(responses))
	for c := range responses {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	for _, c := range codes {
		if len(c) == 3 && c[0] == '2' {
			r := responses[c]
			if s := preferredSchema(r.Content); s != nil {
				return c, resolveType(s)
			}
			if r.Schema != nil { // Swagger 2.0
				return c, resolveType(r.Schema)
			}
			return c, nil
		}
	}
	return "", nil
}

// preferredSchema returns the JSON media type schema when available, otherwise
// the first content schema in a deterministic order.
func preferredSchema(content map[string]MediaType) *Schema {
	if content == nil {
		return nil
	}
	if mt, ok := content["application/json"]; ok && mt.Schema != nil {
		return mt.Schema
	}
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if content[k].Schema != nil {
			return content[k].Schema
		}
	}
	return nil
}

// resolveType maps a schema to a neutral TypeRef.
func resolveType(s *Schema) *TypeRef {
	if s == nil {
		return &TypeRef{Kind: KindAny}
	}
	if s.Ref != "" {
		return &TypeRef{Kind: KindRef, RefName: refName(s.Ref)}
	}
	switch s.Type {
	case "array":
		return &TypeRef{Kind: KindArray, Elem: resolveType(s.Items)}
	case "object":
		if len(s.Properties) == 0 {
			return &TypeRef{Kind: KindMap, Elem: &TypeRef{Kind: KindAny}}
		}
		return &TypeRef{Kind: KindAny}
	case "string", "integer", "number", "boolean":
		return &TypeRef{Kind: KindPrimitive, Primitive: s.Type, Format: s.Format}
	default:
		return &TypeRef{Kind: KindAny}
	}
}

// refName extracts the trailing schema name from a $ref path such as
// "#/components/schemas/Pet" or "#/definitions/Pet".
func refName(ref string) string {
	idx := strings.LastIndex(ref, "/")
	if idx >= 0 && idx+1 < len(ref) {
		return ref[idx+1:]
	}
	return ref
}

// operationID returns a stable identifier for an operation, synthesizing one
// from the method and path when operationId is absent.
func operationID(id, method, path string) string {
	if id = strings.TrimSpace(id); id != "" {
		return id
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	for _, seg := range strings.Split(path, "/") {
		seg = strings.Trim(seg, "{}")
		if seg == "" {
			continue
		}
		b.WriteString(capitalizeASCII(seg))
	}
	return b.String()
}

// capitalizeASCII upper-cases the first rune of s.
func capitalizeASCII(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
