package gen

import (
	"testing"
	"time"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

// petstoreAPI builds an in-memory spec equivalent to example/petstore.yaml.
// Tests share it to exercise the full code path through buildTmplData.
func petstoreAPI() *spec.OpenAPI {
	int64Schema := &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}
	int32Schema := &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32"}
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
							"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"array"}, Items: &spec.Schema{Ref: "#/components/schemas/Pet"}}},
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
			"PetStatus": {Type: spec.SchemaType{"string"}, Enum: []any{"available", "pending", "sold"}},
			"NewPet": {
				Type:     spec.SchemaType{"object"},
				Required: []string{"name"},
				Properties: map[string]*spec.Schema{
					"name":   {Type: spec.SchemaType{"string"}},
					"status": {Ref: "#/components/schemas/PetStatus"},
				},
			},
			"Pet": {
				AllOf: []*spec.Schema{
					{Ref: "#/components/schemas/NewPet"},
					{Type: spec.SchemaType{"object"}, Required: []string{"id"}, Properties: map[string]*spec.Schema{"id": {Type: spec.SchemaType{"integer"}, Format: "int64"}}},
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
	param := buildParam(spec.Parameter{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}}, &spec.OpenAPI{})
	require.Equal(t, "int", param.GoType)
	require.Equal(t, "int", param.GoTypeOptional)
}

// TestBuildParam_RequiredNullableScalarIgnoresNullable: nullable on a
// path/query/header parameter has no wire encoding, so the resolved GoType
// must remain the value type — otherwise the server-side cast emitted by
// the template (`params.X = GoType(n)`) becomes `*int32(n)` and the file
// won't compile.
func TestBuildParam_RequiredNullableScalarIgnoresNullable(t *testing.T) {
	p := buildParam(spec.Parameter{
		Name:     "needed",
		In:       "query",
		Required: true,
		Schema:   &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32", Nullable: true},
	}, &spec.OpenAPI{})
	require.Equal(t, "int32", p.GoType, "required nullable scalar must be value type, not *int32")
	require.Equal(t, "int32", p.GoTypeOptional)
	require.False(t, p.IsNamed, "int32 is not a named alias of int32 — IsNamed must be false to avoid the broken cast branch")
}

// TestBuildParam_OptionalNullableScalarIsSinglePointer: optional+nullable
// must produce a single pointer, not **int32. Earlier code wrapped a
// nullable-derived "*int32" in another pointer for the optional field.
func TestBuildParam_OptionalNullableScalarIsSinglePointer(t *testing.T) {
	p := buildParam(spec.Parameter{
		Name:   "maybe",
		In:     "query",
		Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32", Nullable: true},
	}, &spec.OpenAPI{})
	require.Equal(t, "int32", p.GoType)
	require.Equal(t, "*int32", p.GoTypeOptional)
}

// TestBuildParam_ArrayWithNullableItems: nullable item must not bleed into
// the slice element type — the server template strips "[]" and casts the
// remainder as a type, so "[]*string" produces an invalid "*string(v)".
func TestBuildParam_ArrayWithNullableItems(t *testing.T) {
	p := buildParam(spec.Parameter{
		Name: "tags",
		In:   "query",
		Schema: &spec.Schema{
			Type:  spec.SchemaType{"array"},
			Items: &spec.Schema{Type: spec.SchemaType{"string"}, Nullable: true},
		},
	}, &spec.OpenAPI{})
	require.True(t, p.IsArray)
	require.Equal(t, "[]string", p.GoType, "nullable items must collapse to value-typed elements")
	require.Equal(t, "[]string", p.GoTypeOptional)
}

// TestBuildParam_RefToArraySchema: a parameter whose schema is just a $ref
// to a top-level array component must still be detected as an array param,
// not treated as a scalar. Earlier the array check fired only when
// Type.Primary()=="array" — refs have empty Type, so this silently emitted
// broken slice<->string casts.
func TestBuildParam_RefToArraySchema(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"IDList": {Type: spec.SchemaType{"array"}, Items: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}},
	}}}
	p := buildParam(spec.Parameter{
		Name:   "ids",
		In:     "query",
		Schema: &spec.Schema{Ref: "#/components/schemas/IDList"},
	}, api)
	require.True(t, p.IsArray, "ref to array schema must produce an array param")
	require.Equal(t, "IDList", p.GoType, "the GoType keeps the named alias")
	require.Equal(t, "int64", p.Kind, "element kind comes from the resolved items schema")
	require.Equal(t, "int64", p.ElemGoType)
}

// TestBuildPathExpr_DuplicatePlaceholders: paths like /x/{id}/sub/{id}
// must produce a fmt.Sprintf with N verbs AND N args. Earlier the loop
// over PathParams emitted one arg per param, so duplicate placeholders
// drew an %!d(MISSING) at runtime.
func TestBuildPathExpr_DuplicatePlaceholders(t *testing.T) {
	op := &spec.Operation{Parameters: []spec.Parameter{
		{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}},
	}}
	got := buildPathExpr("/x/{id}/sub/{id}", op)
	require.Equal(t, `fmt.Sprintf("/x/%d/sub/%d", id, id)`, got)
}

func TestParamKind(t *testing.T) {
	tests := []struct {
		name     string
		schema   *spec.Schema
		wantKind string
		wantBits string
	}{
		{"nil", nil, "string", ""},
		{"string", &spec.Schema{Type: spec.SchemaType{"string"}}, "string", ""},
		{"integer no format", &spec.Schema{Type: spec.SchemaType{"integer"}}, "int", "64"},
		{"int32", &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32"}, "int32", "32"},
		{"int64", &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}, "int64", "64"},
		{"number default", &spec.Schema{Type: spec.SchemaType{"number"}}, "float64", "64"},
		{"number float", &spec.Schema{Type: spec.SchemaType{"number"}, Format: "float"}, "float32", "32"},
		{"boolean", &spec.Schema{Type: spec.SchemaType{"boolean"}}, "bool", ""},
		{"empty type", &spec.Schema{}, "string", ""},
		{"unknown", &spec.Schema{Type: spec.SchemaType{"garbage"}}, "string", ""},
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
		"S": {Type: spec.SchemaType{"string"}},
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
		"Validating":    {Type: spec.SchemaType{"object"}, Required: []string{"name"}, Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}}},
		"NotValidating": {Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}}},
	}}}
	require.False(t, bodyHasValidate(nil, api))
	require.True(t, bodyHasValidate(&spec.Schema{Ref: "#/components/schemas/Validating"}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Ref: "#/components/schemas/NotValidating"}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Ref: "bad/ref"}, api))
	require.True(t, bodyHasValidate(&spec.Schema{Type: spec.SchemaType{"object"}, Required: []string{"x"}, Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}}}, api))
	require.False(t, bodyHasValidate(&spec.Schema{Type: spec.SchemaType{"object"}}, api))
}

func TestBuildOp_MergesPathItemLevelParameters(t *testing.T) {
	// Path-item-level params apply to every operation on that path; operation-
	// level params with the same (name, in) override.
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/pets/{id}": {
			Parameters: []spec.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}},
				{Name: "trace", In: "header", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Get: &spec.Operation{
				OperationID: "getPet",
				// Operation also declares "trace" — should win over path-item version.
				Parameters: []spec.Parameter{
					{Name: "trace", In: "header", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				},
				Responses: map[string]spec.Response{"200": {}},
			},
			Delete: &spec.Operation{
				OperationID: "deletePet",
				Responses:   map[string]spec.Response{"204": {}},
			},
		},
	}}
	data := buildTmplData(api, "p")
	require.Len(t, data.Operations, 2)

	for _, op := range data.Operations {
		switch op.Name {
		case "GetPet":
			// Inherits "id" from path-item; "trace" comes from op (Required=true).
			require.Len(t, op.PathParams, 1, "GetPet should inherit path-item path param 'id'")
			require.Equal(t, "id", op.PathParams[0].JSONName)
			require.Len(t, op.HeaderParams, 1)
			require.True(t, op.HeaderParams[0].Required, "operation-level 'trace' (Required=true) must override path-item 'trace'")
		case "DeletePet":
			require.Len(t, op.PathParams, 1, "DeletePet should inherit path-item path param 'id'")
			require.Len(t, op.HeaderParams, 1, "DeletePet should inherit path-item header 'trace'")
			require.False(t, op.HeaderParams[0].Required, "inherited 'trace' carries its declared optionality")
		}
	}
}

func TestParamKind_CycleSafe(t *testing.T) {
	// A → ref(B) → ref(A): paramKind must terminate.
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"A": {Ref: "#/components/schemas/B"},
		"B": {Ref: "#/components/schemas/A"},
	}}}
	done := make(chan bool, 1)
	go func() {
		_, _ = paramKind(&spec.Schema{Ref: "#/components/schemas/A"}, api)
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("paramKind did not terminate on cyclic ref chain")
	}
}

func TestBuildParam_ArrayQuery(t *testing.T) {
	p := spec.Parameter{
		Name: "ids", In: "query",
		Schema: &spec.Schema{
			Type:  spec.SchemaType{"array"},
			Items: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"},
		},
	}
	tp := buildParam(p, &spec.OpenAPI{})
	require.True(t, tp.IsArray)
	require.Equal(t, "[]int64", tp.GoType)
	// Optional arrays don't get a pointer wrap; nil-slice signals "absent".
	require.Equal(t, "[]int64", tp.GoTypeOptional)
	require.Equal(t, "int64", tp.Kind)
	require.NotEmpty(t, tp.ItemSetExpr, "array params must populate ItemSetExpr for per-element stringification")
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
				{Name: "userId", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			}},
			want: `fmt.Sprintf("/users/%s", url.PathEscape(userId))`,
		},
		{
			name: "int path param",
			path: "/pets/{petId}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "petId", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}},
			}},
			want: `fmt.Sprintf("/pets/%d", petId)`,
		},
		{
			name: "bool path param",
			path: "/flags/{on}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "on", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"boolean"}}},
			}},
			want: `fmt.Sprintf("/flags/%t", on)`,
		},
		{
			name: "float path param",
			path: "/scores/{ratio}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "ratio", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"number"}}},
			}},
			want: `fmt.Sprintf("/scores/%v", ratio)`,
		},
		{
			name: "path param without schema",
			path: "/x/{id}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "id", In: "path"},
			}},
			want: `fmt.Sprintf("/x/%s", url.PathEscape(id))`,
		},
		{
			name: "multiple path params",
			path: "/owners/{ownerId}/pets/{petId}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "ownerId", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
				{Name: "petId", In: "path", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			}},
			want: `fmt.Sprintf("/owners/%d/pets/%s", ownerId, url.PathEscape(petId))`,
		},
		{
			name: "named string path param casts before escaping",
			path: "/by-status/{status}",
			op: &spec.Operation{Parameters: []spec.Parameter{
				{Name: "status", In: "path", Schema: &spec.Schema{Ref: "#/components/schemas/PetStatus"}},
			}},
			want: `fmt.Sprintf("/by-status/%s", url.PathEscape(string(status)))`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, buildPathExpr(tc.path, tc.op))
		})
	}
}
