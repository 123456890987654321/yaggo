package gen

import (
	"bytes"
	"errors"
	"go/format"
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateClientPetstore(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, petstoreAPI(), "petstore"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err, "generated client not valid Go:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "package petstore")
	require.Contains(t, s, "func NewClient(baseURL string, opts ...ClientOption) *Client {")
	require.Contains(t, s, "func (c *Client) ListPets(ctx context.Context, limit *int32, status *PetStatus) ([]Pet, error)")
	require.Contains(t, s, "func (c *Client) CreatePet(ctx context.Context, body NewPet) (Pet, error)")
	require.Contains(t, s, "func (c *Client) DeletePet(ctx context.Context, petId int64) error")

	// Named-enum query param uses string() cast in q.Set.
	require.Contains(t, s, `q.Set("status", string(*status))`)
	require.Contains(t, s, `q.Set("limit", strconv.FormatInt(int64(*limit), 10))`)

	// Int path param uses %d formatter.
	require.Contains(t, s, `fmt.Sprintf("/pets/%d", petId)`)

	// Generic decoder is used for typed return.
	require.Contains(t, s, "return decodeResponse[Pet](resp)")
	require.Contains(t, s, "return decodeResponse[[]Pet](resp)")

	// any (not interface{}) in the do() signature.
	require.Contains(t, s, "body any, query url.Values")
}

func TestGenerateClientRequiredQueryDoesNotWrapInIfNotNil(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "getX",
			Parameters: []spec.Parameter{
				{Name: "tag", In: "query", Required: true, Schema: &spec.Schema{Type: "string"}},
				{Name: "n", In: "query", Required: true, Schema: &spec.Schema{Type: "integer"}},
			},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateClient(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoError(t, err)
	s := string(formatted)
	require.Contains(t, s, `q.Set("tag", tag)`)
	require.Contains(t, s, `q.Set("n", strconv.FormatInt(int64(n), 10))`)
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
	require.Contains(t, s, "requestEditors []RequestEditor")

	// NewClient applies options.
	require.Contains(t, s, "for _, opt := range opts {")
	require.Contains(t, s, "opt(c)")

	// do() runs request editors after default headers but before dispatch.
	require.Contains(t, s, "for _, ed := range c.requestEditors {")
	require.Contains(t, s, "if err := ed(ctx, req); err != nil {")
}
