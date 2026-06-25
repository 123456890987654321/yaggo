package gen

import (
	"bytes"
	"errors"
	"go/format"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateClientPetstore(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "petstore"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generated client not valid Go:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "package petstore")
	require.Contains(t, s, "func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {")
	require.Contains(t, s, "func (c *Client) ListPets(ctx context.Context, limit *int32, status *PetStatus) ([]Pet, error)")
	require.Contains(t, s, "func (c *Client) CreatePet(ctx context.Context, body NewPet) (Pet, error)")
	require.Contains(t, s, "func (c *Client) DeletePet(ctx context.Context, petId int64) error")

	// Named-enum query param uses string() cast in qry.Set.
	require.Contains(t, s, `qry.Set("status", string(*status))`)
	require.Contains(t, s, `qry.Set("limit", strconv.FormatInt(int64(*limit), 10))`)

	// Int path param uses %d formatter (no escaping needed).
	require.Contains(t, s, `fmt.Sprintf("/pets/%d", petId)`)

	// Generic decoder is used for typed return; response size limit is threaded through.
	require.Contains(t, s, "return decodeResponse[Pet](httpResp, c.maxResponseBytes)")
	require.Contains(t, s, "return decodeResponse[[]Pet](httpResp, c.maxResponseBytes)")

	// any (not interface{}) in the do() signature; Content-Type, Accept, and
	// operation-level headers are per-call arguments threaded through from the spec.
	require.Contains(t, s, "body any, contentType, accept string, extraHeaders http.Header, query url.Values")
	require.Contains(t, s, `"application/json"`)
}

func TestGenerateClientRequiredQueryDoesNotWrapInIfNotNil(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "getX",
			Parameters: []spec.Parameter{
				{Name: "tag", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "n", In: "query", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)
	require.Contains(t, s, `qry.Set("tag", tag)`)
	require.Contains(t, s, `qry.Set("n", strconv.FormatInt(int64(n), 10))`)
	require.NotContains(t, s, "if tag != nil")
}

func TestGenerateClientPropagatesWriterError(t *testing.T) {
	err := GenerateClient(&failingWriter{err: errors.New("io fail")}, petstoreAPI(), "p")
	require.Error(t, err)
}

func TestGenerateClientEmitsOptionPattern(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, "type ClientOption func(*Client)")
	require.Contains(t, s, "type RequestEditor func(ctx context.Context, req *http.Request) error")
	require.Contains(t, s, "func WithHTTPClient(hc *http.Client) ClientOption")
	require.Contains(t, s, "func WithHeader(key, value string) ClientOption")
	require.Contains(t, s, "func WithUserAgent(ua string) ClientOption")
	require.Contains(t, s, "func WithRequestEditor(fn RequestEditor) ClientOption")
	require.Contains(t, s, "func WithMaxResponseBytes(n int64) ClientOption")
	require.Regexp(t, `requestEditors\s+\[\]RequestEditor`, s)

	// NewClient applies options.
	require.Contains(t, s, "for _, opt := range opts {")
	require.Contains(t, s, "opt(c)")

	// do() runs request editors after default headers but before dispatch.
	require.Contains(t, s, "for _, ed := range c.requestEditors {")
	require.Contains(t, s, "if err := ed(ctx, req); err != nil {")
}

// TestGenerateClientDefaultMaxResponseBytes: the response cap must be on by
// default. A misbehaving (or hostile) upstream sending gigabytes of JSON
// should not be able to exhaust client memory just because the caller didn't
// opt in to WithMaxResponseBytes.
func TestGenerateClientDefaultMaxResponseBytes(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	// The exported constant must exist so callers can reference it.
	require.Contains(t, s, "DefaultMaxResponseBytes int64 = 8 << 20")
	// NewClient must seed the field with the default — otherwise the
	// zero-value (disabled) wins and the cap silently doesn't apply.
	require.Contains(t, s, "maxResponseBytes: DefaultMaxResponseBytes")
}

// TestGenerateClient_DrainIsBounded: drainAndClose must cap the bytes it
// reads from resp.Body — otherwise WithMaxResponseBytes only protects
// memory, not bandwidth/time. The drain should run io.CopyN against a
// constant limit so a hostile upstream can't force the client to read an
// unbounded byte stream just to reuse the connection.
func TestGenerateClient_DrainIsBounded(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, "const drainLimit = 64 << 10")
	require.Contains(t, s, "io.CopyN(io.Discard, body, drainLimit)")
	// The unbounded form must NOT survive.
	require.NotContains(t, s, "io.Copy(io.Discard, body)")
}

// TestGenerateClient_DoDrainOnErrorPath: when httpClient.Do returns (resp,
// err) with both non-nil — e.g. CheckRedirect rejection — the generated
// client must drain+close before returning, and return nil resp so the
// caller's `if err != nil { return ... }` branch is leak-free.
func TestGenerateClient_DoDrainOnErrorPath(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, "resp, err := c.httpClient.Do(req)")
	require.Contains(t, s, "if resp != nil && resp.Body != nil {")
	require.Contains(t, s, "drainAndClose(resp.Body)")
	require.Contains(t, s, "return nil, err")
}

// TestGenerateClient_NoStrconvForIntPathParamOnly: client formats int
// path params via fmt.Sprintf("%d", v), NOT via strconv. A spec whose
// only non-string param is a path int must not pull strconv into the
// client output — earlier it did, producing `imported and not used`.
func TestGenerateClient_NoStrconvForIntPathParamOnly(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x/{id}": {Get: &spec.Operation{
			OperationID: "doIt",
			Parameters: []spec.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"integer"}, Format: "int64"}},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.NotContains(t, s, `"strconv"`, "client must not import strconv when only int path params exist")
	require.Contains(t, s, `fmt.Sprintf("/x/%d", id)`)
}

// TestGenerateClient_RejectsBaseURLWithQuery: NewClient(...) must reject
// baseURL containing a query string or fragment — concatenating raw with
// per-op paths produces a corrupted URL.
func TestGenerateClient_RejectsBaseURLWithQuery(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	require.Contains(t, s, `u.RawQuery != "" || u.Fragment != ""`)
	require.Contains(t, s, "must not contain a query string or fragment")
}

// TestGenerateClient_NoSyncPool: the request-body buffer must not be
// recycled through sync.Pool. http.NewRequestWithContext for *bytes.Buffer
// captures v.Bytes() into req.GetBody; pooling the buffer back lets a
// concurrent request stomp the array while a redirect retry is reading it.
func TestGenerateClient_NoSyncPool(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)

	// No sync import means no Pool can be referenced.
	require.NotContains(t, s, "\"sync\"")
	require.NotContains(t, s, "reqBufPool")
	// Per-request allocation must remain in place.
	require.Contains(t, s, "var buf bytes.Buffer")
	require.Contains(t, s, "json.NewEncoder(&buf).Encode(body)")
}

// TestGenerateClient_VendorJSONContentType verifies that a spec declaring a
// non-default JSON variant (application/vnd.api+json, application/hal+json,
// problem+json, etc.) routes that exact media type into both the Content-Type
// and Accept headers of the generated client — not "application/json".
func TestGenerateClient_VendorJSONContentType(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/widgets": {Post: &spec.Operation{
			OperationID: "createWidget",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/vnd.api+json": {Schema: &spec.Schema{Ref: "#/components/schemas/Widget"}},
			}},
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{
					"application/hal+json": {Schema: &spec.Schema{Ref: "#/components/schemas/Widget"}},
				}},
			},
		}},
	}, Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Widget": {Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{
			"name": {Type: spec.SchemaType{"string"}},
		}},
	}}}

	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// The CreateWidget call passes the spec-declared content type and accept.
	require.Contains(t, s, `"application/vnd.api+json", "application/hal+json"`)
	// And it MUST NOT fall back to "application/json" anywhere on the operation line.
	require.NotContains(t, s, `body, "application/json"`)
}

// TestGenerateClient_NoStrconvNeeded covers the import-trimming logic: a spec
// with only string path/query params should NOT emit "strconv" or the
// "var _ = strconv.Itoa" keep-import line. Verifies the absence directly.
func TestGenerateClient_NoStrconvNeeded(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/items/{name}": {Get: &spec.Operation{
			OperationID: "getItem",
			Parameters: []spec.Parameter{
				{Name: "name", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "tag", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"object"}}}}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt — would indicate the import block is malformed:\n%s", buf.String())
	s := string(formatted)
	require.NotContains(t, s, `"strconv"`, "strconv must not be imported when no non-string params exist")
	require.NotContains(t, s, "var _ = strconv.Itoa", "keep-import placeholder must be gone")
}

func TestGenerateClientHeaderParam(t *testing.T) {
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
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// Method signature includes both header params after query params.
	require.Contains(t, s, "func (c *Client) ListThings(ctx context.Context, xTraceId string, xPage *int)")
	// Required header set unconditionally.
	require.Contains(t, s, `hdrs.Set("X-Trace-Id", xTraceId)`)
	// Optional header guarded.
	require.Contains(t, s, "if xPage != nil {")
	require.Contains(t, s, `hdrs.Set("X-Page", strconv.FormatInt(int64(*xPage), 10))`)
}

// TestGenerateServer_NoStrconvNorFmt covers the analogous trimming on the
// server side: a path with only string params and no body uses neither fmt
// nor strconv.
func TestGenerateServer_NoStrconvNorFmt(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/echo/{msg}": {Get: &spec.Operation{
			OperationID: "echo",
			Parameters: []spec.Parameter{
				{Name: "msg", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{"application/json": {Schema: &spec.Schema{Type: spec.SchemaType{"string"}}}}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.NotContains(t, s, `"strconv"`, "strconv must not be imported")
	require.NotContains(t, s, `"fmt"`, "fmt must not be imported when no error wrapping is needed")
	require.NotContains(t, s, "var _ = fmt.Sprintf")
	require.NotContains(t, s, "var _ = strconv.Itoa")
}
