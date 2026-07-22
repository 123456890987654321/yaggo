package gen

import (
	"bytes"
	"errors"
	"go/format"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateBodyTypesMixedInlineAndRefBodies(t *testing.T) {
	// One inline body triggers emission, one $ref body must be skipped silently.
	// The Other schema is registered as a component so the ref resolves —
	// validateSchemaRefs would otherwise reject the dangling target.
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Other": {Type: spec.SchemaType{"object"}},
		}},
		Paths: map[string]spec.PathItem{
			"/a": {Post: &spec.Operation{
				OperationID: "aPost",
				RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}}}},
				}},
			}},
			"/b": {Post: &spec.Operation{
				OperationID: "bPost",
				RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Other"}},
				}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateBodyTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)
	require.Contains(t, s, "type APostBody struct {")
	require.NotContains(t, s, "type BPostBody struct {")
}

func TestGenerateBodyTypesAllOfInlineBody(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Base": {Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"id": {Type: spec.SchemaType{"integer"}}}},
		}},
		Paths: map[string]spec.PathItem{
			"/x": {Post: &spec.Operation{
				OperationID: "doX",
				RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{AllOf: []*spec.Schema{
						{Ref: "#/components/schemas/Base"},
						{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}}},
					}}},
				}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateBodyTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "output:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "type DoXBody struct {")
	require.Contains(t, s, "Id")
	require.Contains(t, s, "Name")
}

func TestGenerateBodyTypesEmitsForInlineBodies(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doX",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{
					Type:     spec.SchemaType{"object"},
					Required: []string{"name"},
					Properties: map[string]*spec.Schema{
						"name": {Type: spec.SchemaType{"string"}},
					},
				}},
			}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateBodyTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generated body_types not valid Go:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "type DoXBody struct {")
	require.Contains(t, s, "func (v DoXBody) Validate() error")
}

func TestGenerateBodyTypesNoBodiesEmitsNothing(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Pet": {Type: spec.SchemaType{"object"}},
		}},
		Paths: map[string]spec.PathItem{
			"/refonly": {Post: &spec.Operation{
				OperationID: "refOnly",
				RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Pet"}},
				}},
			}},
			"/getonly": {Get: &spec.Operation{OperationID: "noBody"}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateBodyTypes(&buf, api, "p"))
	require.Empty(t, buf.Bytes(), "expected no output when all bodies are $refs")
}

func TestGenerateBodyTypesPropagatesWriterError(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doX",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"a": {Type: spec.SchemaType{"string"}}}}},
			}},
		}},
	}}
	err := GenerateBodyTypes(&failingWriter{err: errors.New("io fail")}, api, "p")
	require.Error(t, err)
}
