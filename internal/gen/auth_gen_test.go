package gen

import (
	"bytes"
	"go/format"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateAuth(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, GenerateAuth(&buf, nil, "petstore"))

	src := buf.Bytes()
	formatted, err := format.Source(src)
	require.NoErrorf(t, err, "GenerateAuth did not gofmt:\n%s", src)

	out := string(formatted)
	require.Contains(t, out, "package petstore")

	// Public surface: every documented helper must be present.
	for _, want := range []string{
		"type SecretString string",
		"func (SecretString) String() string",
		"func (SecretString) GoString() string",
		"func (SecretString) MarshalJSON()",
		"func (SecretString) MarshalText()",
		"func (SecretString) LogValue() slog.Value",
		"func (s SecretString) Reveal() string",
		"type AuthOption func(*authConfig)",
		"func AllowInsecure() AuthOption",
		"var ErrInsecureScheme = errors.New",
		"func BasicAuth(username string, password SecretString, opts ...AuthOption) RequestEditor",
		"func BearerToken(token SecretString, opts ...AuthOption) RequestEditor",
		"func BearerTokenSource(fn func(ctx context.Context) (SecretString, error), opts ...AuthOption) RequestEditor",
		"type APIKeyLocation int",
		"APIKeyHeader APIKeyLocation = iota",
		"APIKeyQuery",
		"func APIKey(name string, key SecretString, location APIKeyLocation, opts ...AuthOption) RequestEditor",
	} {
		require.Containsf(t, out, want, "expected %q in generated auth.go", want)
	}

	// Defense in depth: the literal "[REDACTED]" placeholder must appear,
	// and the body must never reveal the raw secret via fmt verbs.
	require.Contains(t, out, `"[REDACTED]"`)
	require.NotContains(t, out, "password.Reveal()+")
	require.NotContains(t, out, "%s\", token)")
}

func TestGenerateAuth_SpecDrivenSchemes(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{
			SecuritySchemes: map[string]*spec.SecurityScheme{
				"BearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT", Description: "JWT issued by auth service."},
				"BasicAuth":  {Type: "http", Scheme: "basic"},
				"HeaderKey":  {Type: "apiKey", In: "header", Name: "X-API-Key"},
				"QueryKey":   {Type: "apiKey", In: "query", Name: "api_key"},
				"OAuth":      {Type: "oauth2"},
				"WeirdHTTP":  {Type: "http", Scheme: "digest"},
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, GenerateAuth(&buf, api, "petstore"))

	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	out := string(formatted)

	// Wrappers for the supported scheme kinds.
	require.Contains(t, out, "func NewBearerAuth(token SecretString, opts ...AuthOption) RequestEditor")
	require.Contains(t, out, "return BearerToken(token, opts...)")
	require.Contains(t, out, "Token format (informational): JWT")
	require.Contains(t, out, "JWT issued by auth service.")

	require.Contains(t, out, "func NewBasicAuth(username string, password SecretString, opts ...AuthOption) RequestEditor")
	require.Contains(t, out, "return BasicAuth(username, password, opts...)")

	require.Contains(t, out, "func NewHeaderKey(key SecretString, opts ...AuthOption) RequestEditor")
	require.Contains(t, out, `return APIKey("X-API-Key", key, APIKeyHeader, opts...)`)

	require.Contains(t, out, "func NewQueryKey(key SecretString, opts ...AuthOption) RequestEditor")
	require.Contains(t, out, `return APIKey("api_key", key, APIKeyQuery, opts...)`)
	// The query-form leak warning must be present for the apiKey-query helper.
	require.Contains(t, out, "leak through access logs")

	// Unsupported kinds must NOT produce a function but should leave a documented skip.
	require.NotContains(t, out, "func NewOAuth(")
	require.Contains(t, out, "OAuth: no helper generated. oauth2")
	require.NotContains(t, out, "func NewWeirdHTTP(")
	require.Contains(t, out, `WeirdHTTP: no helper generated. http scheme "digest" not supported`)
}

// TestGenerateAuth_OpenIDConnectURLSanitized: the openIdConnectUrl field is
// interpolated into a Go comment via the unsupported-scheme Reason path. A
// crafted URL containing newlines would otherwise escape the comment and
// inject arbitrary top-level Go into auth.go. After sanitization the entire
// payload — newlines and all — collapses onto a single comment line.
func TestGenerateAuth_OpenIDConnectURLSanitized(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{
		SecuritySchemes: map[string]*spec.SecurityScheme{
			"OIDC": {
				Type:             "openIdConnect",
				OpenIDConnectURL: "https://issuer.test/.well-known\nimport \"os/exec\"\nvar _ = exec.Command(\"rm\")",
			},
		},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateAuth(&buf, api, "p"))
	out := buf.String()

	// Safety: the dangerous lines must NOT appear as standalone source —
	// they must remain inside the single comment line.
	require.NotContains(t, out, "\nimport \"os/exec\"")
	require.NotContains(t, out, "\nvar _ = exec.Command")
	// The URL host should still survive (newlines became spaces).
	require.Contains(t, out, "issuer.test")
	// And the whole thing must still gofmt cleanly — no comment escape.
	_, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", out)
}
