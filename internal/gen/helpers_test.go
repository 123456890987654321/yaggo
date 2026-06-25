package gen

import (
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
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
		{"string required", &spec.Schema{Type: spec.SchemaType{"string"}}, false, "string"},
		{"string optional", &spec.Schema{Type: spec.SchemaType{"string"}}, true, "*string"},
		{"integer default", &spec.Schema{Type: spec.SchemaType{"integer"}}, false, "int"},
		{"integer int32", &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32"}, false, "int32"},
		{"integer int64", &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}, false, "int64"},
		{"integer optional", &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}, true, "*int64"},
		{"number default", &spec.Schema{Type: spec.SchemaType{"number"}}, false, "float64"},
		{"number float", &spec.Schema{Type: spec.SchemaType{"number"}, Format: "float"}, false, "float32"},
		{"number optional", &spec.Schema{Type: spec.SchemaType{"number"}}, true, "*float64"},
		{"boolean required", &spec.Schema{Type: spec.SchemaType{"boolean"}}, false, "bool"},
		{"boolean optional", &spec.Schema{Type: spec.SchemaType{"boolean"}}, true, "*bool"},
		{"array no items", &spec.Schema{Type: spec.SchemaType{"array"}}, false, "[]any"},
		{"array of string", &spec.Schema{Type: spec.SchemaType{"array"}, Items: &spec.Schema{Type: spec.SchemaType{"string"}}}, false, "[]string"},
		{"object no addl", &spec.Schema{Type: spec.SchemaType{"object"}}, false, "map[string]any"},
		{"object with addl", &spec.Schema{Type: spec.SchemaType{"object"}, AdditionalProperties: spec.AdditionalProperties{Schema: &spec.Schema{Type: spec.SchemaType{"string"}}, Set: true, Allowed: true}}, false, "map[string]string"},
		{"unknown type", &spec.Schema{Type: spec.SchemaType{"garbage"}}, false, "any"},
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

func TestParamsByLocation(t *testing.T) {
	op := &spec.Operation{
		Parameters: []spec.Parameter{
			{Name: "id", In: "path"},
			{Name: "q", In: "query"},
			{Name: "x", In: "header"},
		},
	}
	pps := paramsByLocation(op, paramInPath)
	require.Len(t, pps, 1)
	require.Equal(t, "id", pps[0].Name)

	qps := paramsByLocation(op, paramInQuery)
	require.Len(t, qps, 1)
	require.Equal(t, "q", qps[0].Name)

	hps := paramsByLocation(op, paramInHeader)
	require.Len(t, hps, 1)
	require.Equal(t, "x", hps[0].Name)

	// Unknown locations (e.g. "cookie") yield an empty slice, not an error.
	require.Empty(t, paramsByLocation(op, "cookie"))
}

func TestIsJSONCompatible(t *testing.T) {
	tests := map[string]bool{
		"application/json":                        true,
		"application/vnd.api+json":                true,
		"application/hal+json":                    true,
		"application/problem+json":                true,
		"application/json; charset=utf-8":         true, // params after ";" are ignored
		"application/vnd.foo+json; charset=utf-8": true,
		"application/xml":                         false,
		"text/plain":                              false,
		"multipart/form-data":                     false,
		"application/x-www-form-urlencoded":       false,
		"":                                        false,
	}
	for ct, want := range tests {
		require.Equalf(t, want, isJSONCompatible(ct), "isJSONCompatible(%q)", ct)
	}
}

func TestRequestBodyContent(t *testing.T) {
	tests := []struct {
		name    string
		op      *spec.Operation
		wantCT  string
		wantNil bool
	}{
		{
			name:    "no request body",
			op:      &spec.Operation{},
			wantNil: true,
		},
		{
			name: "non-JSON content type only",
			op: &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/xml": {Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			}}},
			wantNil: true,
		},
		{
			name: "plain application/json",
			op: &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}},
			}}},
			wantCT: "application/json",
		},
		{
			name: "vnd.api+json fallback when no plain json",
			op: &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/vnd.api+json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}},
			}}},
			wantCT: "application/vnd.api+json",
		},
		{
			name: "exact application/json wins over +json variants",
			op: &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/vnd.api+json": {Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				"application/json":         {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}},
			}}},
			wantCT: "application/json",
		},
		{
			name: "two +json variants -> alphabetic first",
			op: &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/vnd.api+json": {Schema: &spec.Schema{}},
				"application/hal+json":     {Schema: &spec.Schema{}},
			}}},
			wantCT: "application/hal+json", // alphabetic in sortedKeys
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct, bs := requestBodyContent(tc.op)
			require.Equal(t, tc.wantCT, ct)
			if tc.wantNil {
				require.Nil(t, bs)
			} else {
				require.NotNil(t, bs)
			}
		})
	}
}

func TestSuccessResponseContent(t *testing.T) {
	tests := []struct {
		name    string
		op      *spec.Operation
		wantCT  string
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
			name: "200 with non-JSON content",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"text/plain": {Schema: &spec.Schema{}}}},
			}},
			wantNil: true,
		},
		{
			name: "200 application/json",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}}}},
			}},
			wantCT: "application/json",
		},
		{
			name: "200 problem+json (RFC 7807)",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/problem+json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}}}},
			}},
			wantCT: "application/problem+json",
		},
		{
			name: "200 missing; 201 has JSON",
			op: &spec.Operation{Responses: map[string]spec.Response{
				"201": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}}}},
			}},
			wantCT: "application/json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct, bs := successResponseContent(tc.op)
			require.Equal(t, tc.wantCT, ct)
			if tc.wantNil {
				require.Nil(t, bs)
			} else {
				require.NotNil(t, bs)
			}
		})
	}
}

func TestMergeAllOf(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Base": {
				Type:       spec.SchemaType{"object"},
				Required:   []string{"id"},
				Properties: map[string]*spec.Schema{"id": {Type: spec.SchemaType{"integer"}}},
			},
		}},
	}
	schemas := []*spec.Schema{
		{Ref: "#/components/schemas/Base"},
		{Type: spec.SchemaType{"object"}, Required: []string{"name"}, Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}}},
		{Ref: "#/components/schemas/Missing"}, // unresolvable, skipped
	}
	merged := mergeAllOf(schemas, api)
	require.Equal(t, "object", merged.Type.Primary())
	require.Contains(t, merged.Properties, "id")
	require.Contains(t, merged.Properties, "name")
	require.ElementsMatch(t, []string{"id", "name"}, merged.Required)
}

func TestHttpMethodsConst(t *testing.T) {
	require.Equal(t, []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE"}, httpMethods)
}

// TestSchemaToGoType_OAS31Nullable verifies that 3.1's type:["X","null"] form
// produces pointer types (matching 3.0's nullable:true behaviour) regardless
// of whether the field is marked required.
func TestSchemaToGoType_OAS31Nullable(t *testing.T) {
	tests := []struct {
		name     string
		schema   *spec.Schema
		optional bool
		want     string
	}{
		{
			name:     "type array with null forces pointer (required)",
			schema:   &spec.Schema{Type: spec.SchemaType{"string", "null"}},
			optional: false,
			want:     "*string",
		},
		{
			name:     "type array with null still pointer (optional)",
			schema:   &spec.Schema{Type: spec.SchemaType{"integer", "null"}, Format: "int64"},
			optional: true,
			want:     "*int64",
		},
		{
			name:     "3.0 nullable:true forces pointer",
			schema:   &spec.Schema{Type: spec.SchemaType{"boolean"}, Nullable: true},
			optional: false,
			want:     "*bool",
		},
		{
			name:     "non-nullable required scalar stays value type",
			schema:   &spec.Schema{Type: spec.SchemaType{"number"}},
			optional: false,
			want:     "float64",
		},
		{
			name:     "ref + nullable type array produces pointer",
			schema:   &spec.Schema{Ref: "#/components/schemas/Pet", Type: spec.SchemaType{"null"}},
			optional: false,
			want:     "*Pet",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, schemaToGoType(tc.schema, tc.optional))
		})
	}
}
