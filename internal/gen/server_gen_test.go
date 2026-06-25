package gen

import (
	"bytes"
	"errors"
	"go/format"
	"strings"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
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
					{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32"}},
					{Name: "flag", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"boolean"}}},
					{Name: "ratio", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"number"}, Format: "float"}},
					{Name: "q", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
					{Name: "f", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"number"}}},
					{Name: "b", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"boolean"}}},
					{Name: "name", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
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
			Parameters:  []spec.Parameter{{Name: "name", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}}},
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
			"Status": {Type: spec.SchemaType{"string"}, Enum: []any{"a", "b"}},
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
				"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}}}},
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
			"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}},
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

func TestGenerateServerHeaderParam(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/things": {Get: &spec.Operation{
			OperationID: "listThings",
			Parameters: []spec.Parameter{
				{Name: "X-Trace-Id", In: "header", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "X-Page", In: "header", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
			},
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"array"}, Items: &spec.Schema{Type: spec.SchemaType{"string"}}}}}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// Headers struct emitted alongside params struct.
	require.Contains(t, s, "type ListThingsHeaders struct {")
	require.Regexp(t, `XTraceId\s+string\s+`+"`"+`header:"X-Trace-Id"`+"`", s)
	require.Regexp(t, `XPage\s+\*int\s+`+"`"+`header:"X-Page"`+"`", s)
	// ServerInterface signature gets a headers arg.
	require.Contains(t, s, "ListThings(w http.ResponseWriter, r *http.Request, headers ListThingsHeaders)")
	// Required header missing → 400.
	require.Contains(t, s, `missing required header %q`)
	require.Contains(t, s, `"X-Trace-Id"`)
	// Optional integer header is parsed via strconv.
	require.Contains(t, s, "strconv.ParseInt")
}

func TestGenerateServerHeaderInvalidIntReturns400(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "get",
			Parameters: []spec.Parameter{
				{Name: "X-Count", In: "header", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, `invalid header %q`)
	require.Contains(t, s, "http.StatusBadRequest")
}

func TestGenerateServerEmitsBodyDecodeHelperWith413(t *testing.T) {
	// Spec with an inline body so the generator emits a body-decode helper.
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/widgets": {Post: &spec.Operation{
			OperationID: "createWidget",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}},
			}},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "func decodeCreateWidgetBody(w http.ResponseWriter, r *http.Request, cfg *serverConfig)")
	require.Contains(t, s, "http.MaxBytesReader(w, r.Body, cfg.maxBodyBytes)")
	require.Contains(t, s, "var maxErr *http.MaxBytesError")
	require.Contains(t, s, "errors.As(err, &maxErr)")
	require.Contains(t, s, "http.StatusRequestEntityTooLarge")
	// maxBodyBytes == 0 means unlimited; the helper must short-circuit when 0.
	require.Contains(t, s, "if cfg.maxBodyBytes > 0 {")
}

func TestGenerateServerRequiredQueryMissing400(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/search": {Get: &spec.Operation{
			OperationID: "search",
			Parameters: []spec.Parameter{
				{Name: "q", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "limit", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// Required missing → 400.
	require.Contains(t, s, `missing required query parameter %q`)
	// Required parse error → 400.
	require.Contains(t, s, `invalid query parameter %q`)
	require.Contains(t, s, "http.StatusBadRequest")
}

func TestGenerateServerOptionalQueryParseErrorStillSilent(t *testing.T) {
	// Backward-compat behaviour: optional query params that fail to parse are
	// silently treated as "not supplied" rather than rejected.
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "list",
			Parameters: []spec.Parameter{
				{Name: "limit", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "if err == nil {")
	require.NotContains(t, s, `invalid query parameter "limit"`)
}

func TestGenerateServerArrayQueryParam(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/search": {Get: &spec.Operation{
			OperationID: "search",
			Parameters: []spec.Parameter{
				{Name: "ids", In: "query", Required: true, Schema: &spec.Schema{
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"},
				}},
				{Name: "tags", In: "query", Schema: &spec.Schema{
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"string"}},
				}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "Ids  []int64")
	require.Contains(t, s, "Tags []string")
	// Per-element parsing loop with ParseInt for required int array.
	require.Contains(t, s, "vs := query[\"ids\"]")
	require.Contains(t, s, "arr := make([]int64, 0, len(vs))")
	require.Contains(t, s, "strconv.ParseInt(v, 10, 64)")
	require.Contains(t, s, "missing required query parameter %q")
	// String array: no parsing, direct append.
	require.Contains(t, s, "vs := query[\"tags\"]")
	require.Contains(t, s, "arr := make([]string, 0, len(vs))")
}

func TestGenerateServerCachesQueryParse(t *testing.T) {
	// Operations with multiple query params must parse r.URL.Query() once.
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "list",
			Parameters: []spec.Parameter{
				{Name: "a", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "b", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	// One cached parse per handler; no inline r.URL.Query().Get(...) calls remain.
	require.Equal(t, 1, strings.Count(s, "query := r.URL.Query()"), "URL query must be parsed once and cached")
	require.NotContains(t, s, "r.URL.Query().Get(", "no inline-per-parameter re-parsing of the query string")
}

func TestGenerateServerWriteContentBuffersBeforeHeader(t *testing.T) {
	// Regression: WriteContent must encode into a buffer before WriteHeader so
	// that a marshal error can still report 500 with a correct status.
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)
	require.Contains(t, s, "var buf bytes.Buffer")
	require.Contains(t, s, "if err := json.NewEncoder(&buf).Encode(v); err != nil {")
	require.Contains(t, s, "http.Error(w, \"internal server error\", http.StatusInternalServerError)")
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
	require.Contains(t, s, "errorHandler: defaultErrorHandler,")
	require.Contains(t, s, "maxBodyBytes: DefaultMaxBodyBytes,")
	require.Contains(t, s, "wrap := chainMiddleware(cfg.middlewares)")
	require.Contains(t, s, "func WithMaxBodyBytes(n int64) ServerOption")
	require.Contains(t, s, "const DefaultMaxBodyBytes int64 = 1 << 20")

	// Error paths go through cfg.errorHandler, not WriteError.
	require.Contains(t, s, "cfg.errorHandler(w, r, fmt.Errorf(\"invalid path param 'petId': %w\", err), http.StatusBadRequest)")
	require.Contains(t, s, "cfg.errorHandler(w, r, fmt.Errorf(\"invalid request body: %w\", err), http.StatusBadRequest)")
	require.Contains(t, s, "cfg.errorHandler(w, r, err, http.StatusBadRequest)")
	// 413 path for oversize bodies.
	require.Contains(t, s, "http.StatusRequestEntityTooLarge")
	require.Contains(t, s, "http.MaxBytesReader")

	// WriteError is still emitted as a public helper for users.
	require.Contains(t, s, "func WriteError(w http.ResponseWriter, status int, msg string)")
}

// TestGenerateServer_ContentTypeMismatchReturns415: server must reject
// requests whose Content-Type doesn't match the spec-declared media type
// with HTTP 415, not 400 with a confusing JSON parse error.
func TestGenerateServer_ContentTypeMismatchReturns415(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{
				Required: true,
				Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{
						Type:       spec.SchemaType{"object"},
						Properties: map[string]*spec.Schema{"n": {Type: spec.SchemaType{"string"}}},
					}},
				},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "http.StatusUnsupportedMediaType")
	require.Contains(t, s, "unsupported Content-Type")
	require.Contains(t, s, "strings.EqualFold")
}

// TestGenerateServer_WildcardAndDefaultResponseDecoded: the 2XX wildcard
// and the "default" response code now feed into successResponseContent,
// so the client method gets a body-typed return.
func TestGenerateServer_WildcardAndDefaultResponseDecoded(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/wild": {Get: &spec.Operation{
			OperationID: "getWild",
			Responses: map[string]spec.Response{
				"2XX": {Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				}},
			},
		}},
		"/def": {Get: &spec.Operation{
			OperationID: "getDefault",
			Responses: map[string]spec.Response{
				"default": {Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "func (c *Client) GetWild(ctx context.Context) (string, error)")
	require.Contains(t, s, "func (c *Client) GetDefault(ctx context.Context) (string, error)")
}

// TestGenerateServer_ZeroOperationsCompiles: a spec with components only
// (no paths or paths-without-operations) must still produce a server.go
// that compiles — earlier the `wrap := chainMiddleware(...)` local was
// emitted unconditionally and Go rejected the unused variable.
func TestGenerateServer_ZeroOperationsCompiles(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"n": {Type: spec.SchemaType{"string"}}}},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// `wrap` must NOT be declared at all (no `wrap := chainMiddleware`).
	require.NotContains(t, s, "wrap := chainMiddleware")
	// The `_ = si` / `_ = cfg` no-op assignments keep unused-variable
	// checks happy without removing the public surface.
	require.Contains(t, s, "_ = si")
	require.Contains(t, s, "_ = cfg")
}

// TestGenerateServer_OptionalBodyAcceptsEmpty: when requestBody.required
// is false the decoder must short-circuit on io.EOF instead of returning
// 400 on every empty body. The check is wired through the BodyRequired
// flag in tmplOp.
func TestGenerateServer_OptionalBodyAcceptsEmpty(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{
				Required: false,
				Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{
						Type:       spec.SchemaType{"object"},
						Properties: map[string]*spec.Schema{"n": {Type: spec.SchemaType{"string"}}},
					}},
				},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "errors.Is(err, io.EOF)", "optional body must short-circuit on EOF")
	require.Contains(t, s, "\"io\"", "io must be imported when an optional body exists")
}

// TestGenerateServer_RequiredBodyStillFails: when requestBody.required is
// true (the default) an empty body must still flow through the regular
// error path. We verify the EOF short-circuit is absent.
func TestGenerateServer_RequiredBodyStillFails(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{
				Required: true,
				Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{
						Type:       spec.SchemaType{"object"},
						Properties: map[string]*spec.Schema{"n": {Type: spec.SchemaType{"string"}}},
					}},
				},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	require.NotContains(t, string(formatted), "errors.Is(err, io.EOF)")
}

// TestGenerateServer_ResolvesComponentParameterRefs: a parameter declared
// as $ref:'#/components/parameters/X' must resolve to the referenced
// definition so downstream code sees a concrete name/in/schema.
func TestGenerateServer_ResolvesComponentParameterRefs(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Parameters: map[string]*spec.Parameter{
			"Limit": {Name: "limit", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int32"}},
		}},
		Paths: map[string]spec.PathItem{
			"/x": {Get: &spec.Operation{
				OperationID: "doIt",
				Parameters:  []spec.Parameter{{Ref: "#/components/parameters/Limit"}},
				Responses:   map[string]spec.Response{"204": {}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// The resolved param shows up in the Params struct.
	require.Contains(t, s, "type DoItParams struct {")
	require.Contains(t, s, "Limit *int32")
}

// TestGenerateServer_UnresolvedParameterRefRejected: when the target
// components.parameters entry doesn't exist, the user must get a clear
// error pointing at the bad ref — not the legacy "empty name" cryptic
// message.
func TestGenerateServer_UnresolvedParameterRefRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "doIt",
			Parameters:  []spec.Parameter{{Ref: "#/components/parameters/Missing"}},
			Responses:   map[string]spec.Response{"204": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not be resolved")
	require.Contains(t, err.Error(), "Missing")
}

// TestGenerateServer_RegistersOptionsHeadTrace: spec authors who declare
// OPTIONS/HEAD/TRACE operations on a path must get registered handlers.
// Earlier the generator silently skipped them, leaving 405-via-chi as a
// surprise to anyone who used those verbs.
func TestGenerateServer_RegistersOptionsHeadTrace(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {
			Options: &spec.Operation{OperationID: "optionsX"},
			Head:    &spec.Operation{OperationID: "headX"},
			Trace:   &spec.Operation{OperationID: "traceX"},
		},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generated server.go must be valid Go:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, `r.Method("OPTIONS", "/x"`)
	require.Contains(t, s, `r.Method("HEAD", "/x"`)
	require.Contains(t, s, `r.Method("TRACE", "/x"`)
	// Each operation also surfaces in ServerInterface.
	require.Contains(t, s, "OptionsX(")
	require.Contains(t, s, "HeadX(")
	require.Contains(t, s, "TraceX(")
}

// TestGenerateServerDefaultErrorHandlerUsesStatusText: the default handler
// must NOT echo err.Error() into the response — that path leaks JSON decoder
// internals and the user's own request bytes. Custom handlers installed via
// WithErrorHandler still receive the full err and can opt back in.
func TestGenerateServerDefaultErrorHandlerUsesStatusText(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, `WriteJSON(w, status, map[string]string{"error": http.StatusText(status)})`)
	// The old leaky form must NOT survive.
	require.NotContains(t, s, `map[string]string{"error": err.Error()}`)
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

// TestGenerateServer_OptionalBodySkipsValidateOnEmptyBody: when requestBody.required
// is false and the body type has a Validate() method, an absent body must NOT
// trigger Validate() on the zero-value struct. Earlier the generated code called
// body.Validate() unconditionally, causing false 400 errors for clients that
// legitimately omit the optional body.
func TestGenerateServer_OptionalBodySkipsValidateOnEmptyBody(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{
				Required: false,
				Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{
						Type: spec.SchemaType{"object"},
						Required: []string{"name"},
						Properties: map[string]*spec.Schema{
							"name": {Type: spec.SchemaType{"string"}},
						},
					}},
				},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// The decode function must return a bool indicating body presence.
	require.Contains(t, s, "bool, error)", "decode func must return (T, bool, error) for optional body")
	// The call site must capture bodyPresent.
	require.Contains(t, s, "body, bodyPresent, err :=", "caller must capture bodyPresent")
	// Validate must be guarded by bodyPresent.
	require.Contains(t, s, "if bodyPresent {", "Validate must be skipped when body absent")
	// The EOF short-circuit must return bodyPresent=false.
	require.Contains(t, s, "return out, false, nil", "EOF path must return bodyPresent=false")
}
