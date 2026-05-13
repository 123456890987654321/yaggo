package gen

import (
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestToGoName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"pet_name", "PetName"},
		{"pet-name", "PetName"},
		{"petName", "PetName"},
		{"PetName", "PetName"},
		{"ABC_def", "ABCDef"},
		{"x", "X"},
		{"snake_case_long", "SnakeCaseLong"},
		{"", ""},
		{"___", "___"}, // all-empty parts → falls back to original
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			require.Equal(t, tc.want, toGoName(tc.in))
		})
	}
}

func TestToGoFieldName(t *testing.T) {
	require.Equal(t, "PetId", toGoFieldName("pet_id"))
}

func TestOperationName(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		op     *spec.Operation
		want   string
	}{
		{"with operationId", "GET", "/pets", &spec.Operation{OperationID: "listPets"}, "ListPets"},
		{"GET list (collection)", "GET", "/pets", &spec.Operation{}, "ListPets"},
		{"GET single (path param)", "GET", "/pets/{petId}", &spec.Operation{}, "GetPetsByPetId"},
		{"POST", "POST", "/pets", &spec.Operation{}, "CreatePets"},
		{"PUT", "PUT", "/pets/{petId}", &spec.Operation{}, "UpdatePetsByPetId"},
		{"PATCH", "PATCH", "/pets/{petId}", &spec.Operation{}, "UpdatePetsByPetId"},
		{"DELETE", "DELETE", "/pets/{petId}", &spec.Operation{}, "DeletePetsByPetId"},
		{"unknown method", "HEAD", "/pets", &spec.Operation{}, "HeadPets"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, operationName(tc.method, tc.path, tc.op))
		})
	}
}

func TestSchemaToGoType(t *testing.T) {
	tests := []struct {
		name     string
		schema   *spec.Schema
		optional bool
		want     string
	}{
		{"nil schema", nil, false, "any"},
		{"ref required", &spec.Schema{Ref: "#/components/schemas/Pet"}, false, "Pet"},
		{"ref optional", &spec.Schema{Ref: "#/components/schemas/Pet"}, true, "*Pet"},
		{"string required", &spec.Schema{Type: "string"}, false, "string"},
		{"string optional", &spec.Schema{Type: "string"}, true, "*string"},
		{"integer default", &spec.Schema{Type: "integer"}, false, "int"},
		{"integer int32", &spec.Schema{Type: "integer", Format: "int32"}, false, "int32"},
		{"integer int64", &spec.Schema{Type: "integer", Format: "int64"}, false, "int64"},
		{"integer optional", &spec.Schema{Type: "integer", Format: "int64"}, true, "*int64"},
		{"number default", &spec.Schema{Type: "number"}, false, "float64"},
		{"number float", &spec.Schema{Type: "number", Format: "float"}, false, "float32"},
		{"number optional", &spec.Schema{Type: "number"}, true, "*float64"},
		{"boolean required", &spec.Schema{Type: "boolean"}, false, "bool"},
		{"boolean optional", &spec.Schema{Type: "boolean"}, true, "*bool"},
		{"array no items", &spec.Schema{Type: "array"}, false, "[]any"},
		{"array of string", &spec.Schema{Type: "array", Items: &spec.Schema{Type: "string"}}, false, "[]string"},
		{"object no addl", &spec.Schema{Type: "object"}, false, "map[string]any"},
		{"object with addl", &spec.Schema{Type: "object", AdditionalProperties: &spec.Schema{Type: "string"}}, false, "map[string]string"},
		{"unknown type", &spec.Schema{Type: "garbage"}, false, "any"},
		{"empty type", &spec.Schema{}, false, "any"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, schemaToGoType(tc.schema, tc.optional))
		})
	}
}

func TestSortedKeys(t *testing.T) {
	require.Empty(t, sortedKeys[int](nil))
	require.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]int{"c": 0, "a": 0, "b": 0}))
}

func TestPathItemOp(t *testing.T) {
	g := &spec.Operation{OperationID: "g"}
	po := &spec.Operation{OperationID: "po"}
	pu := &spec.Operation{OperationID: "pu"}
	pa := &spec.Operation{OperationID: "pa"}
	d := &spec.Operation{OperationID: "d"}
	item := &spec.PathItem{Get: g, Post: po, Put: pu, Patch: pa, Delete: d}
	tests := []struct {
		method string
		want   *spec.Operation
	}{
		{"GET", g},
		{"POST", po},
		{"PUT", pu},
		{"PATCH", pa},
		{"DELETE", d},
		{"OPTIONS", nil},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			require.Same(t, tc.want, pathItemOp(item, tc.method))
		})
	}
}

func TestCollectOps(t *testing.T) {
	api := &spec.OpenAPI{
		Paths: map[string]spec.PathItem{
			"/b": {Get: &spec.Operation{OperationID: "bGet"}},
			"/a": {
				Post: &spec.Operation{OperationID: "aPost"},
				Get:  &spec.Operation{OperationID: "aGet"},
			},
		},
	}
	ops := collectOps(api)
	require.Len(t, ops, 3)
	// Order: paths sorted; within a path, GET before POST.
	require.Equal(t, "/a", ops[0].Path)
	require.Equal(t, "GET", ops[0].Method)
	require.Equal(t, "/a", ops[1].Path)
	require.Equal(t, "POST", ops[1].Method)
	require.Equal(t, "/b", ops[2].Path)
}

func TestPathAndQueryParams(t *testing.T) {
	op := &spec.Operation{
		Parameters: []spec.Parameter{
			{Name: "id", In: "path"},
			{Name: "q", In: "query"},
			{Name: "x", In: "header"},
		},
	}
	pps := pathParams(op)
	require.Len(t, pps, 1)
	require.Equal(t, "id", pps[0].Name)
	qps := queryParams(op)
	require.Len(t, qps, 1)
	require.Equal(t, "q", qps[0].Name)
}

func TestRequestBodySchema(t *testing.T) {
	require.Nil(t, requestBodySchema(&spec.Operation{}))

	noJSON := &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
		"application/xml": {Schema: &spec.Schema{Type: "string"}},
	}}}
	require.Nil(t, requestBodySchema(noJSON))

	withJSON := &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
		"application/json": {Schema: &spec.Schema{Type: "object"}},
	}}}
	require.NotNil(t, requestBodySchema(withJSON))
}

func TestSuccessResponseSchema(t *testing.T) {
	tests := []struct {
		name    string
		op      *spec.Operation
		wantNil bool
	}{
		{
			name:    "no responses",
			op:      &spec.Operation{},
			wantNil: true,
		},
		{
			name: "only 404",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"404": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{}}}},
			}},
			wantNil: true,
		},
		{
			name: "200 with no JSON content",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"text/plain": {Schema: &spec.Schema{}}}},
			}},
			wantNil: true,
		},
		{
			name: "200 with JSON content",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: "object"}}}},
			}},
		},
		{
			name: "201 only",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"201": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: "object"}}}},
			}},
		},
		{
			name: "202 only",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"202": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: "object"}}}},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := successResponseSchema(tc.op)
			if tc.wantNil {
				require.Nil(t, got)
			} else {
				require.NotNil(t, got)
			}
		})
	}
}

func TestMergeAllOf(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Base": {
				Type:       "object",
				Required:   []string{"id"},
				Properties: map[string]*spec.Schema{"id": {Type: "integer"}},
			},
		}},
	}
	schemas := []*spec.Schema{
		{Ref: "#/components/schemas/Base"},
		{Type: "object", Required: []string{"name"}, Properties: map[string]*spec.Schema{"name": {Type: "string"}}},
		{Ref: "#/components/schemas/Missing"}, // unresolvable, skipped
	}
	merged := mergeAllOf(schemas, api)
	require.Equal(t, "object", merged.Type)
	require.Contains(t, merged.Properties, "id")
	require.Contains(t, merged.Properties, "name")
	require.ElementsMatch(t, []string{"id", "name"}, merged.Required)
}

func TestHttpMethodsConst(t *testing.T) {
	require.Equal(t, []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, httpMethods)
}
