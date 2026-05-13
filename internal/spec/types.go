package spec

// OpenAPI is the root object of an OpenAPI 3.x document.
type OpenAPI struct {
	OpenAPI    string               `yaml:"openapi"`
	Info       Info                 `yaml:"info"`
	Paths      map[string]PathItem  `yaml:"paths"`
	Components Components           `yaml:"components"`
}

// Info holds API metadata such as its title and version.
type Info struct {
	Title   string `yaml:"title"`
	Version string `yaml:"version"`
}

// PathItem contains the operations defined on a single URL path.
type PathItem struct {
	Get    *Operation `yaml:"get"`
	Post   *Operation `yaml:"post"`
	Put    *Operation `yaml:"put"`
	Delete *Operation `yaml:"delete"`
	Patch  *Operation `yaml:"patch"`
}

// Operation describes a single API operation on a path.
type Operation struct {
	OperationID string              `yaml:"operationId"`
	Summary     string              `yaml:"summary"`
	Description string              `yaml:"description"`
	Tags        []string            `yaml:"tags"`
	Parameters  []Parameter         `yaml:"parameters"`
	RequestBody *RequestBody        `yaml:"requestBody"`
	Responses   map[string]Response `yaml:"responses"`
}

// Parameter describes a single operation parameter (path, query, header, or cookie).
type Parameter struct {
	Name        string  `yaml:"name"`
	In          string  `yaml:"in"` // "path", "query", "header", or "cookie"
	Required    bool    `yaml:"required"`
	Description string  `yaml:"description"`
	Schema      *Schema `yaml:"schema"`
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
}

// Components holds reusable schema definitions referenced by $ref.
type Components struct {
	Schemas map[string]*Schema `yaml:"schemas"`
}

// Schema represents an OpenAPI/JSON Schema node. A non-empty Ref field indicates
// the schema is a $ref that should be resolved via ResolveSchema.
type Schema struct {
	Ref                  string             `yaml:"$ref"`
	Type                 string             `yaml:"type"`
	Format               string             `yaml:"format"`
	Description          string             `yaml:"description"`
	Nullable             bool               `yaml:"nullable"`
	Properties           map[string]*Schema `yaml:"properties"`
	Required             []string           `yaml:"required"`
	Items                *Schema            `yaml:"items"`
	AdditionalProperties *Schema            `yaml:"additionalProperties"`
	Enum                 []any              `yaml:"enum"`
	AllOf                []*Schema          `yaml:"allOf"`
	OneOf                []*Schema          `yaml:"oneOf"`
	AnyOf                []*Schema          `yaml:"anyOf"`
	Minimum              *float64           `yaml:"minimum"`
	Maximum              *float64           `yaml:"maximum"`
	MinLength            *int               `yaml:"minLength"`
	MaxLength            *int               `yaml:"maxLength"`
	Pattern              string             `yaml:"pattern"`
	Default              any                `yaml:"default"`
}
