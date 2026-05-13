package gen

import (
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

// petstoreAPI builds an in-memory spec equivalent to example/petstore.yaml.
// Tests share it to exercise the full code path through buildTmplData.
func petstoreAPI() *spec.OpenAPI {
	int64Schema := &spec.Schema{Type: "integer", Format: "int64"}
	int32Schema := &spec.Schema{Type: "integer", Format: "int32"}
	statusRef := &spec.Schema{Ref: "#/components/schemas/PetStatus"}
	return &spec.OpenAPI{
		OpenAPI: "3.0.3",
		Info:    spec.Info{Title: "Petstore", Version: "1.0"},
		Paths: map[string]spec.PathItem{
			"/pets": {
				Get: &spec.Operation{
					OperationID: "listPets",
					Summary:     "List all pets",
					Parameters: []spec.Parameter{
						{Name: "limit", In: "query", Schema: int32Schema},
						{Name: "status", In: "query", Schema: statusRef},
					},
					Responses: map[string]spec.Response{
						"200": {Content: map[string]spec.MediaType{
							"application/json": {Schema: &spec.Schema{Type: "array", Items: &spec.Schema{Ref: "#/components/schemas/Pet"}}},
						}},
					},
				},
				Post: &spec.Operation{
					OperationID: "createPet",
					Summary:     "Create a pet",
					RequestBody: &spec.RequestBody{Required: true, Content: map[string]spec.MediaType{
						"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/NewPet"}},
					}},
					Responses: map[string]spec.Response{
						"201": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Pet"}}}},
					},
				},
			},
			"/pets/{petId}": {
				Get: &spec.Operation{
					OperationID: "getPet",
					Parameters:  []spec.Parameter{{Name: "petId", In: "path", Required: true, Schema: int64Schema}},
					Responses: map[string]spec.Response{
						"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Pet"}}}},
					},
				},
				Delete: &spec.Operation{
					OperationID: "deletePet",
					Parameters:  []spec.Parameter{{Name: "petId", In: "path", Required: true, Schema: int64Schema}},
					Responses:   map[string]spec.Response{"204": {}},
				},
			},
		},
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"PetStatus": {Type: "string", Enum: []any{"available", "pending", "sold"}},
			"NewPet": {
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]*spec.Schema{
					"name":   {Type: "string"},
					"status": {Ref: "#/components/schemas/PetStatus"},
				},
			},
			"Pet": {
				AllOf: []*spec.Schema{
					{Ref: "#/components/schemas/NewPet"},
					{Type: "object", Required: []string{"id"}, Properties: map[string]*spec.Schema{"id": {Type: "integer", Format: "int64"}}},
				},
			},
		}},
	}
}

func TestBuildTmplData(t *testing.T) {
	data := buildTmplData(petstoreAPI(), "petstore")
	require.Equal(t, "petstore", data.Package)
	require.Len(t, data.Operations, 4)

	// listPets has both query params and a return type.
	listPets := data.Operations[0]
	require.Equal(t, "ListPets", listPets.Name)
	require.Equal(t, "GET", listPets.Method)
	require.True(t, listPets.HasQuery)
	require.False(t, listPets.HasBody)
	require.True(t, listPets.HasReturn)
	require.Equal(t, "[]Pet", listPets.ReturnType)
	require.Len(t, listPets.QueryParams, 2)

	limit := listPets.QueryParams[0]
	require.Equal(t, "limit", limit.JSONName)
	require.Equal(t, "int32", limit.GoType)
	require.Equal(t, "int32", limit.Kind)
	require.Equal(t, "*int32", limit.GoTypeOptional)
	require.False(t, limit.IsNamed)

	status := listPets.QueryParams[1]
	require.Equal(t, "PetStatus", status.GoType)
	require.Equal(t, "string", status.Kind)
	require.True(t, status.IsNamed, "PetStatus must be flagged as named")
	require.Equal(t, "string(status)", status.QuerySetExpr)
	require.Equal(t, "string(*status)", status.QuerySetExprDeref)

	// createPet has a body whose type has Validate (NewPet has required + enum field).
	createPet := data.Operations[1]
	require.Equal(t, "CreatePet", createPet.Name)
	require.True(t, createPet.HasBody)
	require.Equal(t, "NewPet", createPet.BodyType)
	require.True(t, createPet.HasBodyValidate)

	// getPet uses a path param.
	getPet := data.Operations[2]
	require.Len(t, getPet.PathParams, 1)
	require.Equal(t, "petId", getPet.PathParams[0].JSONName)
	require.Equal(t, "int64", getPet.PathParams[0].GoType)
	require.Contains(t, getPet.PathExpr, "/pets/%d")

	// deletePet has no return type.
	deletePet := data.Operations[3]
	require.False(t, deletePet.HasReturn)
}

func TestBuildParamFallbackNoSchema(t *testing.T) {
	param := buildParam(spec.Parameter{Name: "x", In: "query"}, &spec.OpenAPI{})
	require.Equal(t, "string", param.GoType)
	require.Equal(t, "*string", param.GoTypeOptional)
	require.Equal(t, "string", param.Kind)
	require.False(t, param.IsNamed)
}

func TestBuildParamRequiredHasNoPointer(t *testing.T) {
	param := buildParam(spec.Parameter{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: "integer"}}, &spec.OpenAPI{})
	require.Equal(t, "int", param.GoType)
	require.Equal(t, "int", param.GoTypeOptional)
}

func TestParamKind(t *testing.T) {
	tests := []struct {
		name     string
		schema   *spec.Schema
		wantKind string
		wantBits string
	}{
		{"nil", nil, "string", ""},
		{"string", &spec.Schema{Type: "string"}, "string", ""},
		{"integer no format", &spec.Schema{Type: "integer"}, "int", "64"},
		{"int32", &spec.Schema{Type: "integer", Format: "int32"}, "int32", "32"},
		{"int64", &spec.Schema{Type: "integer", Format: "int64"}, "int64", "64"},
		{"number default", &spec.Schema{Type: "number"}, "float64", "64"},
		{"number float", &spec.Schema{Type: "number", Format: "float"}, "float32", "32"},
		{"boolean", &spec.Schema{Type: "boolean"}, "bool", ""},
		{"empty type", &spec.Schema{}, "string", ""},
		{"unknown", &spec.Schema{Type: "garbage"}, "string", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, bits := paramKind(tc.schema, &spec.OpenAPI{})
			require.Equal(t, tc.wantKind, kind)
			require.Equal(t, tc.wantBits, bits)
		})
	}
}

func TestParamKindRef(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"S": {Type: "string"},
	}}}
	kind, bits := paramKind(&spec.Schema{Ref: "#/components/schemas/S"}, api)
	require.Equal(t, "string", kind)
	require.Equal(t, "", bits)

	// Unresolvable ref → fallback string/"".
	kind, bits = paramKind(&spec.Schema{Ref: "#/components/schemas/Missing"}, api)
	require.Equal(t, "string", kind)
	require.Equal(t, "", bits)

	// Bad ref prefix → also fallback.
	kind, bits = paramKind(&spec.Schema{Ref: "not/local"}, api)
	require.Equal(t, "string", kind)
	require.Equal(t, "", bits)
}

func TestQuerySetExpr(t *testing.T) {
	tests := []struct {
		name    string
		kind    string
		named   bool
		varExpr string
		want    string
	}{
		{"int", "int", false, "limit", "strconv.FormatInt(int64(limit), 10)"},
		{"int32 deref", "int32", false, "*limit", "strconv.FormatInt(int64(*limit), 10)"},
		{"float64", "float64", false, "x", "strconv.FormatFloat(float64(x), 'f', -1, 64)"},
		{"float32 deref", "float32", false, "*x", "strconv.FormatFloat(float64(*x), 'f', -1, 64)"},
		{"bool", "bool", false, "b", "strconv.FormatBool(b)"},
		{"named bool", "bool", true, "b", "strconv.FormatBool(bool(b))"},
		{"string", "string", false, "s", "s"},
		{"named string", "string", true, "*s", "string(*s)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, querySetExpr(tc.varExpr, tc.kind, tc.named, "Foo"))
		})
	}
}

func TestBodyHasValidate(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Validating":     {Type: "object", Required: []string{"name"}, Properties: map[string]*spec.Schema{"name": {Type: "string"}}},
		"NotValidating":  {Type: "object", Properties: map[string]*spec.Schema{"x": {Type: "string"}}},
	}}}
	require.False(t, bodyHasValidate(nil, api))
	require.True(t, bodyHasValidate(&spec.Schema{Ref: "#/components/schemas/Validating"}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Ref: "#/components/schemas/NotValidating"}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Ref: "bad/ref"}, api))
	require.True(t, bodyHasValidate(&spec.Schema{Type: "object", Required: []string{"x"}, Properties: map[string]*spec.Schema{"x": {Type: "string"}}}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Type: "object"}, api))
}

func TestBuildPathExpr(t *testing.T) {
	tests := []struct {
		name string
		path string
		op   *spec.Operation
		want string
	}{
		{
			name: "no params",
			path: "/pets",
			op:   &spec.Operation{},
			want: `"/pets"`,
		},
		{
			name: "string path param",
			path: "/users/{userId}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "userId", In: "path", Schema: &spec.Schema{Type: "string"}},
			}},
			want: `fmt.Sprintf("/users/%s", userId)`,
		},
		{
			name: "int path param",
			path: "/pets/{petId}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "petId", In: "path", Schema: &spec.Schema{Type: "integer", Format: "int64"}},
			}},
			want: `fmt.Sprintf("/pets/%d", petId)`,
		},
		{
			name: "path param without schema",
			path: "/x/{id}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "id", In: "path"},
			}},
			want: `fmt.Sprintf("/x/%s", id)`,
		},
		{
			name: "multiple path params",
			path: "/owners/{ownerId}/pets/{petId}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "ownerId", In: "path", Schema: &spec.Schema{Type: "integer"}},
				{Name: "petId", In: "path", Schema: &spec.Schema{Type: "string"}},
			}},
			want: `fmt.Sprintf("/owners/%d/pets/%s", ownerId, petId)`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, buildPathExpr(tc.path, tc.op))
		})
	}
}
