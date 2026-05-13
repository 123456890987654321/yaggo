package gen_test

import (
	"bytes"
	"go/format"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/123456890987654321/yago/internal/gen"
	"github.com/123456890987654321/yago/internal/spec"
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

	petstoreYAML, err := filepath.Abs(filepath.Join("..", "..", "example", "petstore.yaml"))
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
		require.NoError(t, os.WriteFile(filepath.Join(dir, filename), formatted, 0o644))
	}
	emit("types.go", gen.GenerateTypes)
	emit("server.go", gen.GenerateServer)
	emit("client.go", gen.GenerateClient)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module petstore\n\ngo 1.21\n"), 0o644))

	smoke := `package petstore

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	c := NewClient("http://example.test",
		WithHTTPClient(&http.Client{Transport: transport}),
		WithUserAgent("yago-test/1.0"),
		WithHeader("X-Trace", "abc"),
		WithRequestEditor(func(_ context.Context, req *http.Request) error {
			req.Header.Set("X-Editor", "set")
			return nil
		}),
	)
	if err := c.DeletePet(context.Background(), 1); err != nil {
		t.Fatalf("DeletePet: %v", err)
	}
	if sawUA != "yago-test/1.0" {
		t.Errorf("user-agent = %q, want yago-test/1.0", sawUA)
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
	c := NewClient("http://example.test",
		WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("transport should not run when editor aborts")
			return nil, nil
		})}),
		WithRequestEditor(func(context.Context, *http.Request) error { return abort }),
	)
	err := c.DeletePet(context.Background(), 1)
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
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "options_test.go"), []byte(smoke), 0o644))

	for _, args := range [][]string{
		{"go", "mod", "tidy"},
		{"go", "test", "./..."},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		require.NoErrorf(t, cmd.Run(), "%v failed:\n%s", args, out.String())
	}
}
