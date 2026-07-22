package spec

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

// OpenAPI is the root object of an OpenAPI 3.x document (this generator handles
// both 3.0.x and 3.1.x). Only the fields the code generator consumes are
// represented as typed Go fields; unknown keys are silently ignored.
type OpenAPI struct {
	OpenAPI           string                `yaml:"openapi"`
	JSONSchemaDialect string                `yaml:"jsonSchemaDialect"` // 3.1: optional dialect URI for schemas
	Info              Info                  `yaml:"info"`
	Servers           []Server              `yaml:"servers"`
	Paths             map[string]PathItem   `yaml:"paths"`
	Webhooks          map[string]PathItem   `yaml:"webhooks"` // 3.1: out-of-band event endpoints
	Components        Components            `yaml:"components"`
	Security          []SecurityRequirement `yaml:"security"`
	Tags              []Tag                 `yaml:"tags"`
	ExternalDocs      *ExternalDocs         `yaml:"externalDocs"`
}

// Info holds top-level API metadata.
type Info struct {
	Title          string   `yaml:"title"`
	Summary        string   `yaml:"summary"` // 3.1: short summary alongside description
	Description    string   `yaml:"description"`
	TermsOfService string   `yaml:"termsOfService"`
	Contact        *Contact `yaml:"contact"`
	License        *License `yaml:"license"`
	Version        string   `yaml:"version"`
}

// Contact describes API support contact info.
type Contact struct {
	Name  string `yaml:"name"`
	URL   string `yaml:"url"`
	Email string `yaml:"email"`
}

// License describes the API's license. In 3.1 the Identifier field (SPDX) is
// mutually exclusive with URL but the spec does not require the parser to
// enforce that.
type License struct {
	Name       string `yaml:"name"`
	Identifier string `yaml:"identifier"` // 3.1: SPDX identifier (e.g. "Apache-2.0")
	URL        string `yaml:"url"`
}

// Server describes a base URL for the API.
type Server struct {
	URL         string                    `yaml:"url"`
	Description string                    `yaml:"description"`
	Variables   map[string]ServerVariable `yaml:"variables"`
}

// ServerVariable parameterises a Server URL.
type ServerVariable struct {
	Enum        []string `yaml:"enum"`
	Default     string   `yaml:"default"`
	Description string   `yaml:"description"`
}

// Tag groups operations under a label.
type Tag struct {
	Name         string        `yaml:"name"`
	Description  string        `yaml:"description"`
	ExternalDocs *ExternalDocs `yaml:"externalDocs"`
}

// ExternalDocs points at out-of-band documentation.
type ExternalDocs struct {
	Description string `yaml:"description"`
	URL         string `yaml:"url"`
}

// SecurityRequirement maps a scheme name (from components.securitySchemes)
// to the scopes required. An empty map = no auth required; an empty scope
// slice = the scheme is used without OAuth scopes.
type SecurityRequirement map[string][]string

// PathItem contains the operations defined on a single URL path.
// 3.1 also allows summary/description and $ref at this level, plus HEAD,
// OPTIONS, and TRACE operations.
type PathItem struct {
	Ref         string      `yaml:"$ref"`
	Summary     string      `yaml:"summary"`
	Description string      `yaml:"description"`
	Servers     []Server    `yaml:"servers"`
	Parameters  []Parameter `yaml:"parameters"`
	Get         *Operation  `yaml:"get"`
	Put         *Operation  `yaml:"put"`
	Post        *Operation  `yaml:"post"`
	Delete      *Operation  `yaml:"delete"`
	Options     *Operation  `yaml:"options"`
	Head        *Operation  `yaml:"head"`
	Patch       *Operation  `yaml:"patch"`
	Trace       *Operation  `yaml:"trace"`
}

// Operation describes a single API operation on a path.
type Operation struct {
	OperationID  string                `yaml:"operationId"`
	Summary      string                `yaml:"summary"`
	Description  string                `yaml:"description"`
	Tags         []string              `yaml:"tags"`
	Deprecated   bool                  `yaml:"deprecated"`
	Parameters   []Parameter           `yaml:"parameters"`
	RequestBody  *RequestBody          `yaml:"requestBody"`
	Responses    map[string]Response   `yaml:"responses"`
	Security     []SecurityRequirement `yaml:"security"`
	Servers      []Server              `yaml:"servers"`
	ExternalDocs *ExternalDocs         `yaml:"externalDocs"`
}

// Parameter describes a single operation parameter (path, query, header, or cookie).
// A non-empty Ref means the parameter is a $ref to components.parameters and
// the other fields are not populated until ResolveParameter is called.
type Parameter struct {
	Ref             string  `yaml:"$ref"`
	Name            string  `yaml:"name"`
	In              string  `yaml:"in"` // "path", "query", "header", or "cookie"
	Required        bool    `yaml:"required"`
	Deprecated      bool    `yaml:"deprecated"`
	AllowEmptyValue bool    `yaml:"allowEmptyValue"`
	Style           string  `yaml:"style"`
	Explode         *bool   `yaml:"explode"`
	AllowReserved   bool    `yaml:"allowReserved"`
	Description     string  `yaml:"description"`
	Schema          *Schema `yaml:"schema"`
}

// RequestBody describes the body of a request.
type RequestBody struct {
	Required    bool                 `yaml:"required"`
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content"`
}

// MediaType holds the schema for a specific content type (e.g. "application/json").
type MediaType struct {
	Schema *Schema `yaml:"schema"`
}

// Response describes a single response from an API operation.
type Response struct {
	Description string               `yaml:"description"`
	Content     map[string]MediaType `yaml:"content"`
	Headers     map[string]*Header   `yaml:"headers"`
}

// Header is a response header descriptor. Structurally a Parameter without
// "in" and "name" (which are implied by the map key and location).
type Header struct {
	Description string  `yaml:"description"`
	Required    bool    `yaml:"required"`
	Deprecated  bool    `yaml:"deprecated"`
	Schema      *Schema `yaml:"schema"`
}

// Components holds reusable definitions referenced from elsewhere in the spec.
type Components struct {
	Schemas         map[string]*Schema         `yaml:"schemas"`
	SecuritySchemes map[string]*SecurityScheme `yaml:"securitySchemes"`
	Parameters      map[string]*Parameter      `yaml:"parameters"`
	RequestBodies   map[string]*RequestBody    `yaml:"requestBodies"`
	Responses       map[string]*Response       `yaml:"responses"`
	Headers         map[string]*Header         `yaml:"headers"`
}

// SecurityScheme describes a single authentication mechanism. Only the fields
// the client generator consumes are modelled; oauth2 flows and openIdConnect
// URLs are accepted by name but not expanded into types.
type SecurityScheme struct {
	Type             string `yaml:"type"` // "apiKey" | "http" | "oauth2" | "openIdConnect" | "mutualTLS"
	Description      string `yaml:"description"`
	Name             string `yaml:"name"`             // apiKey only: the parameter name (e.g. "X-API-Key")
	In               string `yaml:"in"`               // apiKey only: "query" | "header" | "cookie"
	Scheme           string `yaml:"scheme"`           // http only: "bearer" | "basic" | …
	BearerFormat     string `yaml:"bearerFormat"`     // http+bearer only, informational (e.g. "JWT")
	OpenIDConnectURL string `yaml:"openIdConnectUrl"` // openIdConnect only
}

// SchemaType holds a JSON Schema 2020-12 / OpenAPI 3.1 "type" value. The
// field may be a single string ("type: string") or an array of strings
// ("type: [string, null]") — the array form is how 3.1 expresses what 3.0
// expressed via "nullable: true".
type SchemaType []string

// UnmarshalYAML accepts either a scalar string or a sequence of strings.
func (t *SchemaType) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" || node.Tag == "!!null" {
			*t = nil
			return nil
		}
		*t = SchemaType{node.Value}
		return nil
	case yaml.SequenceNode:
		var arr []string
		if err := node.Decode(&arr); err != nil {
			return fmt.Errorf("schema type array: %w", err)
		}
		*t = SchemaType(arr)
		return nil
	default:
		return fmt.Errorf("schema type: expected scalar or sequence, got node kind %d", node.Kind)
	}
}

// Primary returns the first non-"null" entry. This is the type used for
// Go-type selection; nullability is reported separately by [SchemaType.Nullable].
// Returns "" if no concrete type is set or the only entry is "null".
func (t SchemaType) Primary() string {
	for _, s := range t {
		if s != "null" {
			return s
		}
	}
	return ""
}

// Nullable reports whether "null" appears in the type list (the 3.1 way of
// marking a field as nullable; see also the deprecated [Schema.Nullable] field
// for 3.0 documents).
func (t SchemaType) Nullable() bool {
	return slices.Contains(t, "null")
}

// IsEmpty reports whether no type was set at all.
func (t SchemaType) IsEmpty() bool { return len(t) == 0 }

// ExclusiveBound represents the JSON Schema 2020-12 exclusiveMinimum /
// exclusiveMaximum value. In 3.1 it is a number (the boundary itself); in
// 3.0 it was a boolean flag that paired with minimum/maximum. Both forms
// unmarshal cleanly: the boolean form is preserved as the Bool field, the
// numeric form populates Value.
type ExclusiveBound struct {
	Value *float64 // 3.1: the exclusive boundary
	Bool  bool     // 3.0: whether the adjacent minimum/maximum is exclusive
	Set   bool     // distinguishes "not present" from "present and false"
}

// UnmarshalYAML accepts either a boolean (3.0) or a number (3.1).
func (e *ExclusiveBound) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("exclusive bound: expected scalar, got node kind %d", node.Kind)
	}
	e.Set = true
	switch node.Tag {
	case "!!bool":
		var b bool
		if err := node.Decode(&b); err != nil {
			return err
		}
		e.Bool = b
		return nil
	case "!!int", "!!float":
		var f float64
		if err := node.Decode(&f); err != nil {
			return err
		}
		e.Value = &f
		return nil
	default:
		// Try number first, fall back to bool.
		var f float64
		if err := node.Decode(&f); err == nil {
			e.Value = &f
			return nil
		}
		var b bool
		if err := node.Decode(&b); err == nil {
			e.Bool = b
			return nil
		}
		return fmt.Errorf("exclusive bound: expected boolean or number, got %q", node.Value)
	}
}

// AdditionalProperties wraps the "additionalProperties" keyword which in
// JSON Schema accepts either a schema (constraints on extra entries) or a
// boolean (false = strict, no extras; true = permissive, the default).
// Direct callers should use the Schema/Allowed/Set accessors via the
// helper methods on *Schema rather than reaching in here.
type AdditionalProperties struct {
	Schema  *Schema
	Allowed bool // meaningful only when Set is true
	Set     bool // distinguishes "explicit true/false" from "not present"
}

// UnmarshalYAML accepts either a boolean or a schema mapping.
func (a *AdditionalProperties) UnmarshalYAML(node *yaml.Node) error {
	a.Set = true
	if node.Kind == yaml.ScalarNode && node.Tag == "!!bool" {
		var b bool
		if err := node.Decode(&b); err != nil {
			return fmt.Errorf("additionalProperties: %w", err)
		}
		a.Allowed = b
		return nil
	}
	var s Schema
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("additionalProperties: %w", err)
	}
	a.Schema = &s
	// A schema form implicitly allows extras (constrained by the schema).
	a.Allowed = true
	return nil
}

// Schema represents an OpenAPI 3.x / JSON Schema 2020-12 node. A non-empty
// Ref field indicates the schema is a $ref that should be resolved via
// ResolveSchema. Only the keywords the code generator actually emits Go for
// are modelled with typed fields; the rest are accepted and ignored.
//
// Vendor extensions (the OpenAPI "x-…" namespace) used by this generator:
//
//   - x-go-type:    overrides the Go type used for this schema (e.g. "time.Time")
//   - x-go-import:  import path required by x-go-type (e.g. "time")
//
// See the README for the full extension contract.
type Schema struct {
	Ref                  string               `yaml:"$ref"`
	Type                 SchemaType           `yaml:"type"`
	Format               string               `yaml:"format"`
	Description          string               `yaml:"description"`
	Title                string               `yaml:"title"`
	Nullable             bool                 `yaml:"nullable"` // 3.0 only; in 3.1 use type: [..., "null"]
	ReadOnly             bool                 `yaml:"readOnly"`
	WriteOnly            bool                 `yaml:"writeOnly"`
	Deprecated           bool                 `yaml:"deprecated"`
	Properties           map[string]*Schema   `yaml:"properties"`
	Required             []string             `yaml:"required"`
	Items                *Schema              `yaml:"items"`
	AdditionalProperties AdditionalProperties `yaml:"additionalProperties"`
	Enum                 []any                `yaml:"enum"`
	Const                any                  `yaml:"const"` // JSON Schema 2020-12
	AllOf                []*Schema            `yaml:"allOf"`
	OneOf                []*Schema            `yaml:"oneOf"`
	AnyOf                []*Schema            `yaml:"anyOf"`
	Not                  *Schema              `yaml:"not"`
	Minimum              *float64             `yaml:"minimum"`
	Maximum              *float64             `yaml:"maximum"`
	ExclusiveMinimum     ExclusiveBound       `yaml:"exclusiveMinimum"`
	ExclusiveMaximum     ExclusiveBound       `yaml:"exclusiveMaximum"`
	MultipleOf           *float64             `yaml:"multipleOf"`
	MinLength            *int                 `yaml:"minLength"`
	MaxLength            *int                 `yaml:"maxLength"`
	MinItems             *int                 `yaml:"minItems"`
	MaxItems             *int                 `yaml:"maxItems"`
	UniqueItems          bool                 `yaml:"uniqueItems"`
	MinProperties        *int                 `yaml:"minProperties"`
	MaxProperties        *int                 `yaml:"maxProperties"`
	Pattern              string               `yaml:"pattern"`
	Default              any                  `yaml:"default"`
	Example              any                  `yaml:"example"`  // 3.0 single example, still allowed
	Examples             []any                `yaml:"examples"` // 3.1 / JSON Schema array

	// Vendor extensions consumed by the generator.
	XGoType   string `yaml:"x-go-type"`   // Go type to use instead of the inferred one
	XGoImport string `yaml:"x-go-import"` // import path required by XGoType
}

// IsNullable reports whether the schema admits null values, considering both
// the OpenAPI 3.0 form (nullable: true) and the 3.1 form (type contains "null").
func (s *Schema) IsNullable() bool {
	if s == nil {
		return false
	}
	return s.Nullable || s.Type.Nullable()
}

// AdditionalSchema returns the additionalProperties schema-form value, or
// nil when additionalProperties was absent, was a boolean, or had no
// schema. Most callers want this — code-gen for "map[string]X" only fires
// when there's a concrete element schema.
func (s *Schema) AdditionalSchema() *Schema {
	if s == nil {
		return nil
	}
	return s.AdditionalProperties.Schema
}

// AdditionalPropertiesForbidden reports whether the spec wrote
// `additionalProperties: false` — i.e. the schema is strict and unknown
// JSON fields must be rejected. The default (absent or `true`) is
// permissive and returns false.
func (s *Schema) AdditionalPropertiesForbidden() bool {
	if s == nil {
		return false
	}
	ap := s.AdditionalProperties
	return ap.Set && ap.Schema == nil && !ap.Allowed
}
