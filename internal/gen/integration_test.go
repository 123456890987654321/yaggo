package gen_test

import (
	"bytes"
	"go/format"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/123456890987654321/yaggo/internal/gen"
	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedPetstoreCompilesAndOptionsWork is an end-to-end check: it
// generates types/server/client into a temp module, drops in a unit test that
// exercises every ClientOption and ServerOption, then runs `go test` inside
// that module. If anything in the templates is type-broken, this catches it —
// unlike substring tests, which only check syntax.
//
// Requirements: `go` on PATH and module-proxy access for github.com/go-chi/chi/v5.
// `go test -short` skips it.
func TestGeneratedPetstoreCompilesAndOptionsWork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go binary not on PATH")
	}
	conn, err := net.DialTimeout("tcp", "proxy.golang.org:443", 3*time.Second)
	if err != nil {
		t.Skip("skipping: module proxy not reachable:", err)
	}
	_ = conn.Close()

	petstoreYAML, err := filepath.Abs(filepath.Join("..", "..", "examples", "petstore.yaml"))
	require.NoError(t, err)
	api, err := spec.Parse(petstoreYAML)
	require.NoError(t, err)

	dir := t.TempDir()
	pkg := "petstore"

	type genFn func(io.Writer, *spec.OpenAPI, string) error
	emit := func(filename string, fn genFn) {
		var buf bytes.Buffer
		require.NoError(t, fn(&buf, api, pkg))
		formatted, err := format.Source(buf.Bytes())
		require.NoErrorf(t, err, "%s did not gofmt:\n%s", filename, buf.String())
		require.NoError(t, os.WriteFile(filepath.Join(dir, filename), formatted, 0o600))
	}
	emit("types.go", gen.GenerateTypes)
	emit("server.go", gen.GenerateServer)
	emit("client.go", gen.GenerateClient)
	emit("auth.go", gen.GenerateAuth)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module petstore\n\ngo 1.21\n"), 0o600))

	smoke := `package petstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// stubServer satisfies ServerInterface; every method returns 204.
type stubServer struct{}

func (stubServer) ListPets(w http.ResponseWriter, _ *http.Request, _ ListPetsParams) {
	w.WriteHeader(http.StatusNoContent)
}
func (stubServer) CreatePet(w http.ResponseWriter, _ *http.Request, _ NewPet) {
	w.WriteHeader(http.StatusNoContent)
}
func (stubServer) GetPet(w http.ResponseWriter, _ *http.Request, _ int64) {
	w.WriteHeader(http.StatusNoContent)
}
func (stubServer) UpdatePet(w http.ResponseWriter, _ *http.Request, _ int64, _ NewPet) {
	w.WriteHeader(http.StatusNoContent)
}
func (stubServer) DeletePet(w http.ResponseWriter, _ *http.Request, _ int64) {
	w.WriteHeader(http.StatusNoContent)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestClientOptions(t *testing.T) {
	var sawUA, sawHeader, sawEditor string
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		sawUA = req.Header.Get("User-Agent")
		sawHeader = req.Header.Get("X-Trace")
		sawEditor = req.Header.Get("X-Editor")
		return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
	})
	c, err := NewClient("http://example.test",
		WithHTTPClient(&http.Client{Transport: transport}),
		WithUserAgent("yaggo-test/1.0"),
		WithHeader("X-Trace", "abc"),
		WithRequestEditor(func(_ context.Context, req *http.Request) error {
			req.Header.Set("X-Editor", "set")
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if sawUA != "yaggo-test/1.0" {
		t.Errorf("user-agent = %q, want yaggo-test/1.0", sawUA)
	}
	if sawHeader != "abc" {
		t.Errorf("X-Trace = %q, want abc", sawHeader)
	}
	if sawEditor != "set" {
		t.Errorf("X-Editor = %q, want set", sawEditor)
	}
}

func TestClientRequestEditorAborts(t *testing.T) {
	abort := errors.New("denied")
	c, err := NewClient("http://example.test",
		WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport should not run when editor aborts")
			return nil, nil
		})}),
		WithRequestEditor(func(context.Context, *http.Request) error { return abort }),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.DeletePet(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Errorf("err = %v, want wrapping %v", err, abort)
	}
}

func TestServerMiddleware(t *testing.T) {
	var order []string
	r := chi.NewRouter()
	RegisterHandlers(r, stubServer{},
		WithMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				order = append(order, "outer-in")
				next.ServeHTTP(w, req)
				order = append(order, "outer-out")
			})
		}),
		WithMiddleware(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				order = append(order, "inner-in")
				next.ServeHTTP(w, req)
				order = append(order, "inner-out")
			})
		}),
	)
	req := httptest.NewRequest("DELETE", "/pets/1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	want := "outer-in,inner-in,inner-out,outer-out"
	if strings.Join(order, ",") != want {
		t.Errorf("order = %v, want %s", order, want)
	}
}

func TestServerErrorHandlerCustom(t *testing.T) {
	var called bool
	r := chi.NewRouter()
	RegisterHandlers(r, stubServer{},
		WithErrorHandler(func(w http.ResponseWriter, _ *http.Request, err error, status int) {
			called = true
			http.Error(w, "custom: "+err.Error(), status)
		}),
	)
	req := httptest.NewRequest("GET", "/pets/not-a-number", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !called {
		t.Fatal("custom error handler not invoked")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), "custom: invalid path param 'petId':") {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestServerRequestBodySizeLimit(t *testing.T) {
	r := chi.NewRouter()
	RegisterHandlers(r, stubServer{}, WithMaxBodyBytes(64))

	// A body larger than 64 bytes must be rejected with 413.
	oversized := strings.Repeat("x", 500)
	payload := ` + "`" + `{"name":"` + "`" + ` + oversized + ` + "`" + `"}` + "`" + `
	req := httptest.NewRequest("POST", "/pets", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413; body=%s", rec.Code, rec.Body.String())
	}
	// The default error handler emits http.StatusText(413) only — no echoed
	// internals — so the body is the canonical "Request Entity Too Large".
	if !strings.Contains(rec.Body.String(), "Request Entity Too Large") {
		t.Errorf("body = %q, want canonical 413 status text", rec.Body.String())
	}
}

func TestServerRequestBodySizeLimitDefault(t *testing.T) {
	// With no WithMaxBodyBytes the default kicks in. A small body still passes.
	r := chi.NewRouter()
	RegisterHandlers(r, stubServer{})
	req := httptest.NewRequest("POST", "/pets", strings.NewReader(` + "`" + `{"name":"Buddy"}` + "`" + `))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerErrorHandlerDefault(t *testing.T) {
	r := chi.NewRouter()
	RegisterHandlers(r, stubServer{})
	req := httptest.NewRequest("GET", "/pets/not-a-number", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "\"error\"") {
		t.Errorf("body = %q, want JSON {error:...}", rec.Body.String())
	}
}

func TestNewClientValidation(t *testing.T) {
	if _, err := NewClient(""); err == nil {
		t.Error("empty URL: want error")
	}
	if _, err := NewClient("ftp://example.com"); err == nil {
		t.Error("ftp scheme: want error")
	}
	if _, err := NewClient("not-a-url"); err == nil {
		t.Error("no scheme: want error")
	}
	c, err := NewClient("http://example.com/api/v1/")
	if err != nil {
		t.Fatalf("valid URL: %v", err)
	}
	if c == nil {
		t.Fatal("want non-nil client")
	}
}

func TestSecretStringRedaction(t *testing.T) {
	s := SecretString("super-secret")

	// Every format verb that fmt routes through Stringer must redact.
	for verb, want := range map[string]string{
		"%v":  "[REDACTED]",
		"%s":  "[REDACTED]",
		"%q":  "\"[REDACTED]\"",
		"%#v": "[REDACTED]",
	} {
		if got := fmt.Sprintf(verb, s); got != want {
			t.Errorf("fmt.Sprintf(%q, secret) = %q, want %q", verb, got, want)
		}
	}

	// JSON must redact.
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if string(b) != "\"[REDACTED]\"" {
		t.Errorf("json = %s, want \"[REDACTED]\"", b)
	}

	// Sanity: the raw secret must never appear in any formatted form.
	mixed := fmt.Sprintf("%v %s %#v %q", s, s, s, s)
	if strings.Contains(mixed, "super-secret") {
		t.Errorf("secret leaked via fmt: %s", mixed)
	}

	// Reveal returns the underlying value.
	if got := s.Reveal(); got != "super-secret" {
		t.Errorf("Reveal() = %q, want super-secret", got)
	}

	// Explicit string conversion is the only sanctioned bypass.
	if got := string(s); got != "super-secret" {
		t.Errorf("string(secret) = %q, want super-secret", got)
	}
}

// authCapture is a roundtripper that records the request headers/URL for
// inspection. It always returns 200 No Body.
type authCapture struct {
	auth        string
	apikey      string
	contentType string
	accept      string
	url         *url.URL
}

func (a *authCapture) RoundTrip(req *http.Request) (*http.Response, error) {
	a.auth = req.Header.Get("Authorization")
	a.apikey = req.Header.Get("X-API-Key")
	a.contentType = req.Header.Get("Content-Type")
	a.accept = req.Header.Get("Accept")
	a.url = req.URL
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: http.Header{}}, nil
}

// trackedBody records whether it was fully drained (read to EOF) and closed.
// It also tracks how many Close calls happened so a double-close would be
// visible to the test.
type trackedBody struct {
	r       *strings.Reader
	closes  int
	readEOF bool
}

func newTrackedBody(s string) *trackedBody { return &trackedBody{r: strings.NewReader(s)} }
func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if errors.Is(err, io.EOF) {
		b.readEOF = true
	}
	return n, err
}
func (b *trackedBody) Close() error {
	b.closes++
	return nil
}

// TestResponseBody_DrainedBeforeClose verifies the generated client reads the
// response body to EOF before closing it, so HTTP/1.1 keep-alive connections
// can be reused. A successful decode normally leaves a trailing newline
// (json.Encoder.Encode emits one), and our test body adds extra bytes to make
// sure they're consumed too.
func TestResponseBody_DrainedBeforeClose(t *testing.T) {
	body := newTrackedBody("{\"id\":1,\"name\":\"x\"}\n   trailing garbage")
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
	})
	c, err := NewClient("https://example.test", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetPet(context.Background(), 1); err != nil {
		t.Fatalf("GetPet: %v", err)
	}
	if !body.readEOF {
		t.Error("response body was closed without being drained to EOF (breaks keep-alive)")
	}
	if body.closes != 1 {
		t.Errorf("response body closed %d times, want exactly 1", body.closes)
	}
}

// TestResponseBody_DrainedOnNoReturn covers the no-return-value path (e.g.
// DELETE) where the generated client doesn't call decodeResponse but must
// still drain.
func TestResponseBody_DrainedOnNoReturn(t *testing.T) {
	body := newTrackedBody("some response payload the server sent anyway")
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 204, Body: body, Header: http.Header{}}, nil
	})
	c, err := NewClient("https://example.test", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if !body.readEOF {
		t.Error("response body not drained on no-return path")
	}
	if body.closes != 1 {
		t.Errorf("response body closed %d times, want exactly 1", body.closes)
	}
}

// TestResponseBody_DrainedOnDecodeError verifies the body is drained even
// when JSON decoding fails partway through, so a malformed-response error
// doesn't leak a connection.
func TestResponseBody_DrainedOnDecodeError(t *testing.T) {
	body := newTrackedBody("{this is not valid json at all but is still bytes that need draining}")
	transport := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: http.Header{}}, nil
	})
	c, err := NewClient("https://example.test", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.GetPet(context.Background(), 1); err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !body.readEOF {
		t.Error("response body not drained after decode error")
	}
	if body.closes != 1 {
		t.Errorf("response body closed %d times, want exactly 1", body.closes)
	}
}

// The petstore spec declares all bodies and responses as application/json,
// so the generated client must set Content-Type only when there is a body,
// and Accept on every request.
func TestRequestHeaders_ContentTypeAndAccept(t *testing.T) {
	var sawCT, sawAccept, sawMethod string
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		sawMethod = req.Method
		sawCT = req.Header.Get("Content-Type")
		sawAccept = req.Header.Get("Accept")
		return &http.Response{StatusCode: 204, Body: http.NoBody, Header: http.Header{}}, nil
	})
	c, err := NewClient("https://example.test", WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if sawMethod != "DELETE" {
		t.Errorf("method = %q, want DELETE", sawMethod)
	}
	if sawCT != "" {
		t.Errorf("DELETE Content-Type = %q, want empty (no body)", sawCT)
	}
	if sawAccept != "application/json" {
		t.Errorf("DELETE Accept = %q, want application/json", sawAccept)
	}
}

func newAuthClient(t *testing.T, base string, editor RequestEditor) (*Client, *authCapture) {
	t.Helper()
	cap := &authCapture{}
	c, err := NewClient(base,
		WithHTTPClient(&http.Client{Transport: cap}),
		WithRequestEditor(editor),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, cap
}

func TestBasicAuth(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test", BasicAuth("alice", SecretString("p4ss")))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	// base64("alice:p4ss") = YWxpY2U6cDRzcw==
	if want := "Basic YWxpY2U6cDRzcw=="; cap.auth != want {
		t.Errorf("Authorization = %q, want %q", cap.auth, want)
	}
}

func TestBasicAuthRejectsHTTP(t *testing.T) {
	c, _ := newAuthClient(t, "http://example.test", BasicAuth("alice", SecretString("p4ss")))
	err := c.DeletePet(context.Background(), 1)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !errors.Is(err, ErrInsecureScheme) {
		t.Errorf("err = %v, want wrapping ErrInsecureScheme", err)
	}
	// The error must not embed the secret.
	if strings.Contains(err.Error(), "p4ss") {
		t.Errorf("error leaked password: %v", err)
	}
}

func TestBasicAuthAllowInsecure(t *testing.T) {
	c, cap := newAuthClient(t, "http://example.test", BasicAuth("alice", SecretString("p4ss"), AllowInsecure()))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if cap.auth == "" {
		t.Error("Authorization header not set with AllowInsecure")
	}
}

func TestBearerToken(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test", BearerToken(SecretString("tok-abc")))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if want := "Bearer tok-abc"; cap.auth != want {
		t.Errorf("Authorization = %q, want %q", cap.auth, want)
	}
}

func TestBearerTokenRejectsHTTP(t *testing.T) {
	c, _ := newAuthClient(t, "http://example.test", BearerToken(SecretString("tok-abc")))
	err := c.DeletePet(context.Background(), 1)
	if !errors.Is(err, ErrInsecureScheme) {
		t.Errorf("err = %v, want wrapping ErrInsecureScheme", err)
	}
	if strings.Contains(err.Error(), "tok-abc") {
		t.Errorf("error leaked token: %v", err)
	}
}

func TestBearerTokenSource(t *testing.T) {
	var calls int
	src := func(_ context.Context) (SecretString, error) {
		calls++
		return SecretString(fmt.Sprintf("tok-%d", calls)), nil
	}
	c, cap := newAuthClient(t, "https://example.test", BearerTokenSource(src))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if want := "Bearer tok-1"; cap.auth != want {
		t.Errorf("first call Authorization = %q, want %q", cap.auth, want)
	}
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet 2: %v", err)
	}
	if want := "Bearer tok-2"; cap.auth != want {
		t.Errorf("second call Authorization = %q, want %q (token must rotate)", cap.auth, want)
	}
}

func TestBearerTokenSourceNilFn(t *testing.T) {
	c, _ := newAuthClient(t, "https://example.test", BearerTokenSource(nil))
	err := c.DeletePet(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "token function is nil") {
		t.Errorf("err = %v, want nil-fn error", err)
	}
}

func TestBearerTokenSourcePropagatesError(t *testing.T) {
	boom := errors.New("vault unavailable")
	src := func(context.Context) (SecretString, error) { return "", boom }
	c, _ := newAuthClient(t, "https://example.test", BearerTokenSource(src))
	err := c.DeletePet(context.Background(), 1)
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want wrapping %v", err, boom)
	}
}

func TestAPIKeyHeader(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test",
		APIKey("X-API-Key", SecretString("k-123"), APIKeyHeader))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if want := "k-123"; cap.apikey != want {
		t.Errorf("X-API-Key = %q, want %q", cap.apikey, want)
	}
}

func TestAPIKeyQuery(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test",
		APIKey("api_key", SecretString("k-123"), APIKeyQuery))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if got := cap.url.Query().Get("api_key"); got != "k-123" {
		t.Errorf("query api_key = %q, want k-123", got)
	}
}

func TestAPIKeyRejectsHTTP(t *testing.T) {
	c, _ := newAuthClient(t, "http://example.test",
		APIKey("X-API-Key", SecretString("k-123"), APIKeyHeader))
	err := c.DeletePet(context.Background(), 1)
	if !errors.Is(err, ErrInsecureScheme) {
		t.Errorf("err = %v, want wrapping ErrInsecureScheme", err)
	}
	if strings.Contains(err.Error(), "k-123") {
		t.Errorf("error leaked key: %v", err)
	}
}

func TestAPIKeyUnknownLocation(t *testing.T) {
	c, _ := newAuthClient(t, "https://example.test",
		APIKey("X", SecretString("k"), APIKeyLocation(99)))
	err := c.DeletePet(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "unknown location") {
		t.Errorf("err = %v, want unknown-location error", err)
	}
}

// The petstore spec declares BearerAuth / BasicAuth / ApiKey under
// components.securitySchemes; the generator emits typed wrappers for each.
// These exercise that the wrappers route to the generic helpers correctly.

func TestSpecScheme_BearerAuth(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test", NewBearerAuth(SecretString("jwt-xyz")))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if want := "Bearer jwt-xyz"; cap.auth != want {
		t.Errorf("Authorization = %q, want %q", cap.auth, want)
	}
}

func TestSpecScheme_BasicAuth(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test", NewBasicAuth("alice", SecretString("p4ss")))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if want := "Basic YWxpY2U6cDRzcw=="; cap.auth != want {
		t.Errorf("Authorization = %q, want %q", cap.auth, want)
	}
}

func TestSpecScheme_ApiKey(t *testing.T) {
	c, cap := newAuthClient(t, "https://example.test", NewApiKey(SecretString("k-123")))
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if cap.apikey != "k-123" {
		t.Errorf("X-API-Key = %q, want k-123", cap.apikey)
	}
}

func TestSafeRedirectPolicy_DropsAuthOnSchemeDowngrade(t *testing.T) {
	initial, _ := http.NewRequest("GET", "https://example.com/start", nil)
	redirectReq, _ := http.NewRequest("GET", "http://example.com/redirect", nil)
	redirectReq.Header.Set("Authorization", "Bearer secret")

	if err := safeRedirectPolicy(redirectReq, []*http.Request{initial}); err != nil {
		t.Fatalf("safeRedirectPolicy: %v", err)
	}
	if got := redirectReq.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization after HTTPS→HTTP redirect = %q, want stripped", got)
	}
}

func TestSafeRedirectPolicy_KeepsAuthOnHTTPS(t *testing.T) {
	initial, _ := http.NewRequest("GET", "https://example.com/start", nil)
	redirectReq, _ := http.NewRequest("GET", "https://example.com/redirect", nil)
	redirectReq.Header.Set("Authorization", "Bearer secret")

	if err := safeRedirectPolicy(redirectReq, []*http.Request{initial}); err != nil {
		t.Fatalf("safeRedirectPolicy: %v", err)
	}
	if got := redirectReq.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization after HTTPS→HTTPS redirect = %q, want preserved", got)
	}
}

func TestSafeRedirectPolicy_KeepsAuthHTTPtoHTTP(t *testing.T) {
	initial, _ := http.NewRequest("GET", "http://example.com/start", nil)
	redirectReq, _ := http.NewRequest("GET", "http://example.com/redirect", nil)
	redirectReq.Header.Set("Authorization", "Bearer secret")

	if err := safeRedirectPolicy(redirectReq, []*http.Request{initial}); err != nil {
		t.Fatalf("safeRedirectPolicy: %v", err)
	}
	if got := redirectReq.Header.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization after HTTP→HTTP redirect = %q, want preserved", got)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "options_test.go"), []byte(smoke), 0o600))

	for _, args := range [][]string{
		{"go", "mod", "tidy"},
		{"go", "test", "./..."},
	} {
		cmd := exec.Command(args[0], args[1:]...) // #nosec G204 -- args are test-controlled literals
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		require.NoErrorf(t, cmd.Run(), "%v failed:\n%s", args, out.String())
	}
}
