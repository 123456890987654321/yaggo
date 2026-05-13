package gen

import (
	"bytes"
	"errors"
	"go/format"
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateServerPetstore(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, petstoreAPI(), "petstore"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generated server is not valid Go:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "package petstore")
	require.Contains(t, s, "type ListPetsParams struct {")
	require.Contains(t, s, "type ServerInterface interface {")
	require.Contains(t, s, "ListPets(w http.ResponseWriter, r *http.Request, params ListPetsParams)")
	require.Contains(t, s, "CreatePet(w http.ResponseWriter, r *http.Request, body NewPet)")
	require.Contains(t, s, "GetPet(w http.ResponseWriter, r *http.Request, petId int64)")
	require.Contains(t, s, `r.Method("GET", "/pets", wrap(http.HandlerFunc(`)
	require.Contains(t, s, `r.Method("DELETE", "/pets/{petId}", wrap(http.HandlerFunc(`)

	// Named-enum query param uses a cast, not a raw string assignment.
	require.Contains(t, s, "tmp := PetStatus(v)")
	require.Contains(t, s, "params.Status = &tmp")
	require.NotContains(t, s, "params.Status = &v")

	// Body with a Validate method must be exercised.
	require.Contains(t, s, "if err := body.Validate(); err != nil {")

	require.Contains(t, s, "func WriteJSON(w http.ResponseWriter, status int, v any)")
}

func TestGenerateServerHandlesAllScalarKinds(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/things/{id}/{flag}/{ratio}": {
			Get: &spec.Operation{
				OperationID: "getThing",
				Parameters: []spec.Parameter{
					{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: "integer", Format: "int32"}},
					{Name: "flag", In: "path", Required: true, Schema: &spec.Schema{Type: "boolean"}},
					{Name: "ratio", In: "path", Required: true, Schema: &spec.Schema{Type: "number", Format: "float"}},
					{Name: "q", In: "query", Schema: &spec.Schema{Type: "integer"}},
					{Name: "f", In: "query", Schema: &spec.Schema{Type: "number"}},
					{Name: "b", In: "query", Required: true, Schema: &spec.Schema{Type: "boolean"}},
					{Name: "name", In: "query", Required: true, Schema: &spec.Schema{Type: "string"}},
				},
			},
		},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generator output not valid Go:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "strconv.ParseInt(idRaw, 10, 32)")
	require.Contains(t, s, "strconv.ParseBool(flagRaw)")
	require.Contains(t, s, "strconv.ParseFloat(ratioRaw, 32)")
	require.Contains(t, s, "strconv.ParseInt(v, 10, 64)")
	require.Contains(t, s, "strconv.ParseFloat(v, 64)")
	require.Contains(t, s, "params.B = b")
	require.Contains(t, s, "params.Name = v")
}

func TestGenerateServerStringPathParamHasNoCast(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/items/{name}": {Get: &spec.Operation{
			OperationID: "getItem",
			Parameters:  []spec.Parameter{{Name: "name", In: "path", Required: true, Schema: &spec.Schema{Type: "string"}}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	require.Contains(t, string(formatted), "name := nameRaw")
}

func TestGenerateServerNamedStringPathParamCastsExplicitly(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Status": {Type: "string", Enum: []any{"a", "b"}},
		}},
		Paths: map[string]spec.PathItem{
			"/x/{status}": {Get: &spec.Operation{
				OperationID: "getX",
				Parameters: []spec.Parameter{
					{Name: "status", In: "path", Required: true, Schema: &spec.Schema{Ref: "#/components/schemas/Status"}},
				},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	require.Contains(t, string(formatted), "status := Status(statusRaw)")
}

func TestGenerateServerBodyWithoutValidateSkipsCall(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doX",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{Type: "object", Properties: map[string]*spec.Schema{"name": {Type: "string"}}}},
			}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	require.NotContains(t, buf.String(), "body.Validate()")
}

func TestBodyTypeName(t *testing.T) {
	tests := []struct {
		name string
		op   *spec.Operation
		want string
	}{
		{"no body", &spec.Operation{}, "any"},
		{"ref body", &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
			"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Pet"}},
		}}}, "Pet"},
		{"inline body", &spec.Operation{RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
			"application/json": {Schema: &spec.Schema{Type: "object"}},
		}}}, "CreatePetBody"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := bodyTypeName("CreatePet", MethodOp{Op: tc.op}, &spec.OpenAPI{})
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLowerFirst(t *testing.T) {
	require.Equal(t, "", lowerFirst(""))
	require.Equal(t, "petId", lowerFirst("PetId"))
	require.Equal(t, "x", lowerFirst("x"))
}

func TestGenerateServerPropagatesWriterError(t *testing.T) {
	err := GenerateServer(&failingWriter{err: errors.New("io fail")}, petstoreAPI(), "p")
	require.Error(t, err)
}

func TestGenerateServerEmitsOptionPattern(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, "type Middleware = func(http.Handler) http.Handler")
	require.Contains(t, s, "type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error, status int)")
	require.Contains(t, s, "type ServerOption func(*serverConfig)")
	require.Contains(t, s, "func WithMiddleware(mw Middleware) ServerOption")
	require.Contains(t, s, "func WithErrorHandler(h ErrorHandler) ServerOption")
	require.Contains(t, s, "func RegisterHandlers(r chi.Router, si ServerInterface, opts ...ServerOption)")
	require.Contains(t, s, "cfg := &serverConfig{errorHandler: defaultErrorHandler}")
	require.Contains(t, s, "wrap := chainMiddleware(cfg.middlewares)")

	// Error paths go through cfg.errorHandler, not WriteError.
	require.Contains(t, s, "cfg.errorHandler(w, r, fmt.Errorf(\"invalid path param 'petId': %w\", err), http.StatusBadRequest)")
	require.Contains(t, s, "cfg.errorHandler(w, r, fmt.Errorf(\"invalid request body: %w\", err), http.StatusBadRequest)")
	require.Contains(t, s, "cfg.errorHandler(w, r, err, http.StatusBadRequest)")

	// WriteError is still emitted as a public helper for users.
	require.Contains(t, s, "func WriteError(w http.ResponseWriter, status int, msg string)")
}

func TestGenerateServerMiddlewareChainOrder(t *testing.T) {
	// chainMiddleware should iterate from last to first so the *first* registered
	// middleware ends up outermost.
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)
	require.Contains(t, s, "for i := len(mws) - 1; i >= 0; i--")
}
