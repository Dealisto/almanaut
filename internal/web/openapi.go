package web

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

// This file generates an OpenAPI 3 document and a human-readable docs page
// directly from the entity catalog. The whole point is that adding a catalog
// entity requires no manual spec edits: every schema is reflected from the
// entity's Go struct (its json tags), and every path is derived from the
// resource's route base — exactly like the CRUD handlers and global search.

// oaSchema is a minimal OpenAPI/JSON-Schema node. When Ref is set the node
// marshals as a bare {"$ref": …} and all other fields are ignored, matching the
// JSON-Schema reference rule.
type oaSchema struct {
	Ref                  string
	Type                 string
	Format               string
	Nullable             bool
	Items                *oaSchema
	Properties           *oaProps
	AdditionalProperties *oaSchema // non-nil marks a free-form/typed object map; empty node → `true`
	Description          string
}

func (s *oaSchema) isEmpty() bool {
	return s.Ref == "" && s.Type == "" && s.Format == "" && !s.Nullable &&
		s.Items == nil && s.Properties == nil && s.AdditionalProperties == nil && s.Description == ""
}

func (s *oaSchema) MarshalJSON() ([]byte, error) {
	if s.Ref != "" {
		return json.Marshal(map[string]string{"$ref": s.Ref})
	}
	m := map[string]any{}
	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if s.Nullable {
		m["nullable"] = true
	}
	if s.Items != nil {
		m["items"] = s.Items
	}
	if s.Properties != nil && len(s.Properties.keys) > 0 {
		m["properties"] = s.Properties
	}
	if s.AdditionalProperties != nil {
		if s.AdditionalProperties.isEmpty() {
			m["additionalProperties"] = true // free-form object
		} else {
			m["additionalProperties"] = s.AdditionalProperties
		}
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	return json.Marshal(m)
}

// oaProps is an insertion-ordered property set. JSON objects are unordered, but
// preserving struct field order keeps the generated spec (and docs page) stable
// and readable rather than alphabetized.
type oaProps struct {
	keys []string
	vals map[string]*oaSchema
}

func newProps() *oaProps { return &oaProps{vals: map[string]*oaSchema{}} }

func (p *oaProps) set(k string, v *oaSchema) {
	if _, ok := p.vals[k]; !ok {
		p.keys = append(p.keys, k)
	}
	p.vals[k] = v
}

func (p *oaProps) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range p.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(p.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

var timeType = reflect.TypeOf(time.Time{})

// schemaForType reflects a Go type into a JSON-Schema node, mapping kinds the
// way encoding/json serializes them. Pointers become nullable; time.Time is a
// date-time string; structs recurse into ordered object properties using their
// json tags (matching the actual API output).
func schemaForType(t reflect.Type) *oaSchema {
	switch t.Kind() {
	case reflect.Pointer:
		s := schemaForType(t.Elem())
		s.Nullable = true
		return s
	case reflect.String:
		return &oaSchema{Type: "string"}
	case reflect.Bool:
		return &oaSchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &oaSchema{Type: "integer"}
	case reflect.Float32, reflect.Float64:
		return &oaSchema{Type: "number"}
	case reflect.Slice, reflect.Array:
		return &oaSchema{Type: "array", Items: schemaForType(t.Elem())}
	case reflect.Map:
		return &oaSchema{Type: "object", AdditionalProperties: schemaForType(t.Elem())}
	case reflect.Struct:
		if t == timeType {
			return &oaSchema{Type: "string", Format: "date-time"}
		}
		props := newProps()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, skip := jsonFieldName(f)
			if skip {
				continue
			}
			props.set(name, schemaForType(f.Type))
		}
		return &oaSchema{Type: "object", Properties: props}
	default:
		return &oaSchema{}
	}
}

// jsonFieldName resolves a struct field's serialized name from its json tag,
// mirroring encoding/json: `json:"-"` is skipped, an empty/absent name falls
// back to the Go field name, and tag options after the first comma are ignored.
func jsonFieldName(f reflect.StructField) (name string, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", true
	}
	first, _, _ := strings.Cut(tag, ",")
	if first == "" {
		return f.Name, false
	}
	return first, false
}

// entitySchema is a resource's reflected object schema with the API's synthetic
// custom_fields object folded in, so the spec matches what the JSON handlers
// actually emit (see mergeCustomFields).
func entitySchema(t reflect.Type) *oaSchema {
	s := schemaForType(t)
	if s.Properties == nil {
		s.Properties = newProps()
	}
	s.Properties.set("custom_fields", &oaSchema{
		Type:                 "object",
		AdditionalProperties: &oaSchema{},
		Description:          "User-defined custom field values keyed by field name.",
	})
	return s
}

func schemaRef(name string) *oaSchema { return &oaSchema{Ref: "#/components/schemas/" + name} }

// apiResourceInfo is one entity's contribution to the OpenAPI document: its
// route base, the component-schema name, and the reflected schema itself.
type apiResourceInfo struct {
	Title      string // list title, used as the OpenAPI tag ("Hosts")
	Singular   string // "host"
	Base       string // "/api/hosts"
	SchemaName string // "Host"
	Schema     *oaSchema
}

// apiResource describes this resource for the OpenAPI document. The type
// parameter T is the entity struct, reflected for its schema — this is what
// makes the spec catalog-driven (no per-entity spec code).
func (rs resource[T]) apiResource() apiResourceInfo {
	var zero T
	t := reflect.TypeOf(zero)
	return apiResourceInfo{
		Title:      rs.title,
		Singular:   rs.sing,
		Base:       "/api" + rs.basePath(),
		SchemaName: t.Name(),
		Schema:     entitySchema(t),
	}
}

// --- OpenAPI document structure (top-level ordering fixed by struct fields;
// maps like paths/schemas are unordered in JSON, which is fine — only property
// order within a schema matters for readability, and oaProps preserves that). ---

type openAPIDoc struct {
	OpenAPI    string                 `json:"openapi"`
	Info       oaInfo                 `json:"info"`
	Servers    []oaServer             `json:"servers"`
	Security   []map[string][]string  `json:"security"`
	Paths      map[string]*oaPathItem `json:"paths"`
	Components oaComponents           `json:"components"`
}

type oaInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type oaServer struct {
	URL string `json:"url"`
}

type oaComponents struct {
	Schemas         map[string]*oaSchema        `json:"schemas"`
	SecuritySchemes map[string]oaSecurityScheme `json:"securitySchemes"`
}

type oaSecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme,omitempty"`
	Description string `json:"description,omitempty"`
}

type oaPathItem struct {
	Get    *oaOperation `json:"get,omitempty"`
	Post   *oaOperation `json:"post,omitempty"`
	Put    *oaOperation `json:"put,omitempty"`
	Delete *oaOperation `json:"delete,omitempty"`
}

type oaOperation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary"`
	Parameters  []oaParameter         `json:"parameters,omitempty"`
	RequestBody *oaRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]oaResponse `json:"responses"`
}

type oaParameter struct {
	Name        string    `json:"name"`
	In          string    `json:"in"`
	Required    bool      `json:"required"`
	Schema      *oaSchema `json:"schema"`
	Description string    `json:"description,omitempty"`
}

type oaRequestBody struct {
	Required bool                   `json:"required"`
	Content  map[string]oaMediaType `json:"content"`
}

type oaMediaType struct {
	Schema *oaSchema `json:"schema"`
}

type oaResponse struct {
	Description string                 `json:"description"`
	Content     map[string]oaMediaType `json:"content,omitempty"`
}

func jsonBody(ref string) *oaRequestBody {
	return &oaRequestBody{Required: true, Content: map[string]oaMediaType{
		"application/json": {Schema: schemaRef(ref)},
	}}
}

func jsonResp(desc, ref string) oaResponse {
	return oaResponse{Description: desc, Content: map[string]oaMediaType{
		"application/json": {Schema: schemaRef(ref)},
	}}
}

func jsonArrayResp(desc, ref string) oaResponse {
	return oaResponse{Description: desc, Content: map[string]oaMediaType{
		"application/json": {Schema: &oaSchema{Type: "array", Items: schemaRef(ref)}},
	}}
}

func errorResp(desc string) oaResponse { return jsonResp(desc, "Error") }

var idParam = oaParameter{Name: "id", In: "path", Required: true, Schema: &oaSchema{Type: "integer", Format: "int64"}}

// buildOpenAPIDoc assembles the whole document from the catalog resources.
func buildOpenAPIDoc(resources []mountable, version string) *openAPIDoc {
	if version == "" {
		version = "dev"
	}
	doc := &openAPIDoc{
		OpenAPI: "3.0.3",
		Info: oaInfo{
			Title:   "Almanaut API",
			Version: version,
			Description: "Read-write JSON API for the almanaut homelab CMDB. Reads accept a " +
				"session cookie or a bearer API token; writes require a bearer token.",
		},
		Servers:  []oaServer{{URL: "/"}},
		Security: []map[string][]string{{"bearerToken": {}}},
		Paths:    map[string]*oaPathItem{},
		Components: oaComponents{
			Schemas: map[string]*oaSchema{
				"Error": {Type: "object", Properties: func() *oaProps {
					p := newProps()
					p.set("error", &oaSchema{Type: "string"})
					return p
				}()},
				"Relationship": schemaForType(reflect.TypeOf(domain.Relationship{})),
				"SearchResult": schemaForType(reflect.TypeOf(searchEntry{})),
			},
			SecuritySchemes: map[string]oaSecurityScheme{
				"bearerToken": {
					Type:        "http",
					Scheme:      "bearer",
					Description: "Personal API token (alm_…) created at /account/tokens.",
				},
			},
		},
	}

	for _, rs := range resources {
		info := rs.apiResource()
		doc.Components.Schemas[info.SchemaName] = info.Schema
		tag := info.Title

		doc.Paths[info.Base] = &oaPathItem{
			Get: &oaOperation{
				Tags: []string{tag}, Summary: "List all " + info.Title,
				Responses: map[string]oaResponse{"200": jsonArrayResp("A JSON array of "+info.Singular+" objects", info.SchemaName)},
			},
			Post: &oaOperation{
				Tags: []string{tag}, Summary: "Create a " + info.Singular,
				RequestBody: jsonBody(info.SchemaName),
				Responses: map[string]oaResponse{
					"201": jsonResp("The created "+info.Singular, info.SchemaName),
					"400": errorResp("Validation or malformed-JSON error"),
				},
			},
		}
		doc.Paths[info.Base+"/{id}"] = &oaPathItem{
			Get: &oaOperation{
				Tags: []string{tag}, Summary: "Get one " + info.Singular, Parameters: []oaParameter{idParam},
				Responses: map[string]oaResponse{
					"200": jsonResp("The requested "+info.Singular, info.SchemaName),
					"404": errorResp(info.Singular + " not found"),
				},
			},
			Put: &oaOperation{
				Tags: []string{tag}, Summary: "Replace a " + info.Singular + " (full update, not a partial patch)",
				Parameters: []oaParameter{idParam}, RequestBody: jsonBody(info.SchemaName),
				Responses: map[string]oaResponse{
					"200": jsonResp("The updated "+info.Singular, info.SchemaName),
					"400": errorResp("Validation or malformed-JSON error"),
					"404": errorResp(info.Singular + " not found"),
				},
			},
			Delete: &oaOperation{
				Tags: []string{tag}, Summary: "Delete a " + info.Singular, Parameters: []oaParameter{idParam},
				Responses: map[string]oaResponse{
					"204": {Description: "Deleted"},
					"404": errorResp(info.Singular + " not found"),
				},
			},
		}
	}

	// Cross-entity endpoints, tagged "General".
	doc.Paths["/api/search"] = &oaPathItem{Get: &oaOperation{
		Tags: []string{"General"}, Summary: "Search entities by free-text query",
		Parameters: []oaParameter{{Name: "q", In: "query", Required: true, Schema: &oaSchema{Type: "string"}, Description: "Case-insensitive search term"}},
		Responses:  map[string]oaResponse{"200": jsonArrayResp("Matching entities", "SearchResult")},
	}}
	doc.Paths["/api/relationships"] = &oaPathItem{Get: &oaOperation{
		Tags: []string{"General"}, Summary: "List all relationships",
		Responses: map[string]oaResponse{"200": jsonArrayResp("All relationships", "Relationship")},
	}}
	doc.Paths["/metrics"] = &oaPathItem{Get: &oaOperation{
		Tags: []string{"General"}, Summary: "Prometheus metrics (text exposition format)",
		Responses: map[string]oaResponse{"200": {Description: "Prometheus metrics", Content: map[string]oaMediaType{
			"text/plain": {Schema: &oaSchema{Type: "string"}},
		}}},
	}}
	return doc
}

// openAPISpec serves the generated OpenAPI 3 document. The document is built and
// marshaled once at construction (it is a pure function of the fixed catalog and
// version) and the same bytes are served on every request.
func openAPISpec(resources []mountable, version string) http.HandlerFunc {
	body, err := json.MarshalIndent(buildOpenAPIDoc(resources, version), "", "  ")
	return func(w http.ResponseWriter, r *http.Request) {
		if err != nil {
			apiServerError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	}
}

// --- Human-readable docs page ---

type apiDocsData struct {
	Title    string
	Version  string
	SpecURL  string
	Sections []apiDocSection
	Schemas  []apiDocSchema
}

type apiDocSection struct {
	Name      string
	Endpoints []apiDocEndpoint
}

type apiDocEndpoint struct {
	Method  string
	Path    string
	Summary string
}

type apiDocSchema struct {
	Name   string
	Fields []apiDocField
}

type apiDocField struct {
	Name string
	Type string
}

// typeLabel renders a schema node as a short human type for the docs table,
// e.g. "string", "integer", "array<string>", "object", or a schema name for a
// $ref.
func typeLabel(s *oaSchema) string {
	switch {
	case s.Ref != "":
		return strings.TrimPrefix(s.Ref, "#/components/schemas/")
	case s.Type == "array":
		if s.Items != nil {
			return "array<" + typeLabel(s.Items) + ">"
		}
		return "array"
	case s.Format != "":
		return s.Type + " (" + s.Format + ")"
	default:
		return s.Type
	}
}

// buildAPIDocs projects the OpenAPI document into the view model the docs
// template renders: one endpoint section per entity (plus a General section)
// and a field table per component schema.
func buildAPIDocs(resources []mountable, version string) apiDocsData {
	if version == "" {
		version = "dev"
	}
	data := apiDocsData{Title: "API docs", Version: version, SpecURL: "/api/openapi.json"}

	for _, rs := range resources {
		info := rs.apiResource()
		data.Sections = append(data.Sections, apiDocSection{
			Name: info.Title,
			Endpoints: []apiDocEndpoint{
				{"GET", info.Base, "List all " + info.Title},
				{"POST", info.Base, "Create a " + info.Singular},
				{"GET", info.Base + "/{id}", "Get one " + info.Singular},
				{"PUT", info.Base + "/{id}", "Replace a " + info.Singular},
				{"DELETE", info.Base + "/{id}", "Delete a " + info.Singular},
			},
		})
		data.Schemas = append(data.Schemas, apiDocSchema{Name: info.SchemaName, Fields: schemaFields(info.Schema)})
	}
	data.Sections = append(data.Sections, apiDocSection{
		Name: "General",
		Endpoints: []apiDocEndpoint{
			{"GET", "/api/search?q=", "Search entities by free-text query"},
			{"GET", "/api/relationships", "List all relationships"},
			{"GET", "/metrics", "Prometheus metrics"},
			{"GET", "/api/openapi.json", "This API's OpenAPI 3 document"},
		},
	})
	return data
}

// schemaFields flattens an object schema's top-level properties (in order) into
// name/type rows for the docs table.
func schemaFields(s *oaSchema) []apiDocField {
	if s == nil || s.Properties == nil {
		return nil
	}
	fields := make([]apiDocField, 0, len(s.Properties.keys))
	for _, k := range s.Properties.keys {
		fields = append(fields, apiDocField{Name: k, Type: typeLabel(s.Properties.vals[k])})
	}
	return fields
}

// apiDocsPage renders the server-side API documentation page.
func apiDocsPage(resources []mountable, version string) http.HandlerFunc {
	data := buildAPIDocs(resources, version)
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "apidocs.html", data)
	}
}
