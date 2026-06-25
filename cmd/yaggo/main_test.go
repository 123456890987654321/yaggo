package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

const minimalPetstoreYAML = `openapi: "3.0.3"
info:
  title: Tiny
  version: "1.0"
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  $ref: '#/components/schemas/Pet'
components:
  schemas:
    Pet:
      type: object
      required: [id]
      properties:
        id:
          type: integer
          format: int64
`

// runDefault is a test convenience that invokes run with the production task
// list, matching how main() calls it.
func runDefault(args []string, stdout, stderr io.Writer) int {
	return run(args, stdout, stderr, defaultTasks)
}

func TestRunHappyPath(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))

	out := filepath.Join(dir, "gen")
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", out, "-package", "tiny"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	// types.go, server.go, client.go, auth.go should exist; body_types.go is skipped (no inline bodies).
	for _, f := range []string{"types.go", "server.go", "client.go", "auth.go"} {
		path := filepath.Join(out, f)
		stat, err := os.Stat(path)
		require.NoErrorf(t, err, "expected %s to exist", f)
		require.NotZero(t, stat.Size())
	}
	_, err := os.Stat(filepath.Join(out, "body_types.go"))
	require.True(t, os.IsNotExist(err), "body_types.go should not be written for ref-only bodies")
	require.Contains(t, stdout.String(), "wrote ")
}

func TestRunMissingSpecFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDefault(nil, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "-spec is required")
}

func TestRunBadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-not-a-flag"}, &stdout, &stderr)
	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "flag provided but not defined")
}

func TestRunSpecParseError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", "/no/such/file.yaml"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "parsing spec")
}

func TestRunMkdirError(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))

	// Use an existing file as the requested -out → MkdirAll fails.
	blocker := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", blocker}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "creating output dir")
}

func TestRunGenerateError(t *testing.T) {
	tasks := []genTask{{
		filename: "types.go",
		fn: func(_ io.Writer, _ *spec.OpenAPI, _ string) error {
			return errors.New("gen fail")
		},
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen")}, &stdout, &stderr, tasks)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "generating types.go")
}

func TestRunFormatError(t *testing.T) {
	tasks := []genTask{{
		filename: "bad.go",
		fn: func(w io.Writer, _ *spec.OpenAPI, _ string) error {
			_, err := io.WriteString(w, "this is not valid Go {{")
			return err
		},
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	out := filepath.Join(dir, "gen")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out}, &stdout, &stderr, tasks)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "formatting bad.go")
	// Unformatted source is written for debugging.
	raw, err := os.ReadFile(filepath.Join(out, "bad.go")) // #nosec G304 -- reading known temp path in test
	require.NoError(t, err)
	require.Equal(t, "this is not valid Go {{", string(raw))
}

func TestRunWriteError(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))

	out := filepath.Join(dir, "gen")
	require.NoError(t, os.MkdirAll(out, 0o700))
	// Make types.go a directory so WriteFile fails on it.
	require.NoError(t, os.MkdirAll(filepath.Join(out, "types.go"), 0o700))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", out}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "writing")
}

func TestRunSkipsEmptyOutput(t *testing.T) {
	tasks := []genTask{{
		filename: "empty.go",
		fn:       func(_ io.Writer, _ *spec.OpenAPI, _ string) error { return nil },
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	out := filepath.Join(dir, "gen")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out}, &stdout, &stderr, tasks)
	require.Equal(t, 0, code)
	_, err := os.Stat(filepath.Join(out, "empty.go"))
	require.True(t, os.IsNotExist(err), "empty output should not be written")
	require.True(t, strings.HasSuffix(strings.TrimSpace(stderr.String()), "") || stderr.Len() == 0)
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-version"}, &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Contains(t, stdout.String(), "yaggo")
	require.Empty(t, stderr.String())
}

func TestRunVersionLdflagOverride(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })
	version = "1.2.3"
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-version"}, &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Equal(t, "yaggo 1.2.3\n", stdout.String())
	require.Empty(t, stderr.String())
}

func TestRunOnly(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	out := filepath.Join(dir, "gen")

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", out, "-only", "types,client"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	// types.go and client.go must exist; server.go and auth.go must not.
	for _, want := range []string{"types.go", "client.go"} {
		_, err := os.Stat(filepath.Join(out, want))
		require.NoErrorf(t, err, "expected %s", want)
	}
	for _, missing := range []string{"server.go", "auth.go", "body_types.go"} {
		_, err := os.Stat(filepath.Join(out, missing))
		require.True(t, os.IsNotExist(err), "%s should not exist when -only types,client", missing)
	}
}

func TestRunSkip(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	out := filepath.Join(dir, "gen")

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", out, "-skip", "server,auth"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	for _, missing := range []string{"server.go", "auth.go"} {
		_, err := os.Stat(filepath.Join(out, missing))
		require.True(t, os.IsNotExist(err), "%s should not exist when -skip", missing)
	}
	for _, want := range []string{"types.go", "client.go"} {
		_, err := os.Stat(filepath.Join(out, want))
		require.NoErrorf(t, err, "expected %s", want)
	}
}

func TestRunOnlyAndSkipMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-only", "types", "-skip", "server"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "mutually exclusive")
}

// TestRunRejectsMalformedPackageFlag: the -package value is interpolated
// directly as `package %s` in every emitted file. Without validation, a CI
// wrapper that derives the flag from upstream metadata could inject code at
// the package declaration line.
func TestRunRejectsMalformedPackageFlag(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	for _, bad := range []string{
		`api; func init(){}`,
		`api"; var x = "y`,
		`api with spaces`,
		`1starts-with-digit`,
		``,
	} {
		t.Run(bad, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runDefault([]string{"-spec", specFile, "-package", bad}, &stdout, &stderr)
			require.Equal(t, 1, code)
			require.Contains(t, stderr.String(), "-package")
		})
	}
}

func TestRunOnlyUnknownName(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-only", "typos"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "unknown file")
	require.Contains(t, stderr.String(), "typos")
}

func TestRunCheckDoesNotWriteFiles(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o600))
	out := filepath.Join(dir, "gen")

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", out, "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	// Output dir must NOT have been created; nothing on disk.
	_, err := os.Stat(out)
	require.True(t, os.IsNotExist(err), "-check must not create the output directory")
	require.Contains(t, stdout.String(), "ok types.go")
}

func TestRunStrictSurfacesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	bad := minimalPetstoreYAML + "typo_at_root: 1\n"
	require.NoError(t, os.WriteFile(specFile, []byte(bad), 0o600))

	// Without -strict, the unknown key is silently ignored.
	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "lenient"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "lenient should accept unknown keys; stderr=%s", stderr.String())

	// With -strict, the parse fails.
	stdout.Reset()
	stderr.Reset()
	code = runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "strict"), "-strict", "-check"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "typo_at_root")
}

func TestRunWarnsOnEmptyPaths(t *testing.T) {
	emptyPathsYAML := `openapi: "3.0.3"
info:
  title: Empty
  version: "1.0"
paths: {}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(emptyPathsYAML), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen")}, &stdout, &stderr)
	require.Equal(t, 0, code)
	require.Contains(t, stderr.String(), "no paths or webhooks")
}

// TestRunWarnsOnTraceOperations: TRACE handlers expose request data
// (XST attack surface); yaggo emits them when declared, but the user
// should see a warning so they can guard upstream.
func TestRunWarnsOnTraceOperations(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /x:
    trace:
      operationId: traceX
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "TRACE handler declared")
	require.Contains(t, stderr.String(), "XST")
}

// TestRunWarnsOnPathPlaceholderMismatch: when a path placeholder has no
// matching parameter (silent empty string at runtime) or a path parameter
// has no matching placeholder (silent 400 forever), yaggo must warn.
func TestRunWarnsOnPathPlaceholderMismatch(t *testing.T) {
	t.Run("placeholder without param", func(t *testing.T) {
		yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /pets/{petId}:
    get:
      operationId: getPet
      responses: {"204": {description: ok}}
`
		dir := t.TempDir()
		specFile := filepath.Join(dir, "spec.yaml")
		require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))
		var stdout, stderr bytes.Buffer
		code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
		require.Equal(t, 0, code, "stderr=%s", stderr.String())
		require.Contains(t, stderr.String(), "no matching parameter")
	})
	t.Run("param without placeholder", func(t *testing.T) {
		yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /pets:
    get:
      operationId: listPets
      parameters:
        - {name: petId, in: path, required: true, schema: {type: integer}}
      responses: {"204": {description: ok}}
`
		dir := t.TempDir()
		specFile := filepath.Join(dir, "spec.yaml")
		require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))
		var stdout, stderr bytes.Buffer
		code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
		require.Equal(t, 0, code, "stderr=%s", stderr.String())
		require.Contains(t, stderr.String(), "no {petId} placeholder")
	})
}

// TestRunWarnsOnAuthorizationParam: a spec header parameter named
// "Authorization" is silently overridden by yaggo's auth helpers (their
// RequestEditors run last). Warn so spec authors don't lose hours.
func TestRunWarnsOnAuthorizationParam(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /x:
    get:
      operationId: doIt
      parameters:
        - {name: Authorization, in: header, required: true, schema: {type: string}}
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), `header parameter "Authorization"`)
	require.Contains(t, stderr.String(), "overwrite")
}

// TestRunWarnsOnMultiTypeField: 3.1 `type: [string, integer]` collapses
// to first non-null type; warn so spec author refactors to oneOf or
// picks a dominant type.
func TestRunWarnsOnMultiTypeField(t *testing.T) {
	yaml := `openapi: "3.1.1"
info: {title: T, version: "1.0"}
paths: {}
components:
  schemas:
    Multi:
      type: object
      properties:
        v:
          type: [string, integer]
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "declares multiple types")
}

// TestRunWarnsOnNonJSONBody: yaggo doesn't decode multipart/form-encoded
// /xml bodies. Spec author must be told the body argument will be missing.
func TestRunWarnsOnNonJSONBody(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema: {type: object}
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "only non-JSON content types")
	require.Contains(t, stderr.String(), "multipart/form-data")
}

// TestRunWarnsOnEmptyBodyContent: a requestBody with `content: {}` is a
// spec defect (no media types). The warning text must be distinct from
// the non-JSON case so the user knows it's a different issue.
func TestRunWarnsOnEmptyBodyContent(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /x:
    post:
      operationId: doIt
      requestBody:
        required: true
        content: {}
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "no content media types declared")
	require.NotContains(t, stderr.String(), "only non-JSON content types ()")
}

// TestRunWarnsOnNestedArrayParam: array-of-array params can't round-trip
// over URL query/header encoding. yaggo emits []any so the file compiles,
// but the user should know it's a degradation.
func TestRunWarnsOnNestedArrayParam(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /x:
    get:
      operationId: doIt
      parameters:
        - name: matrix
          in: query
          schema:
            type: array
            items:
              type: array
              items: {type: integer}
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "array-of-array")
}

// TestRunWarnsOnCookieParam: cookie parameters aren't wired into the
// generated server/client; the user must be told so they know to read
// the cookie manually.
func TestRunWarnsOnCookieParam(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
paths:
  /x:
    get:
      operationId: doIt
      parameters:
        - {name: session, in: cookie, schema: {type: string}}
      responses: {"204": {description: ok}}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "cookie parameter")
	require.Contains(t, stderr.String(), "session")
}

// TestRunWarnsOnUnwiredSecurity: per-operation and root-level "security"
// requirements aren't enforced by generated code; the user must install
// middleware. Both warnings should appear.
func TestRunWarnsOnUnwiredSecurity(t *testing.T) {
	yaml := `openapi: "3.0.3"
info: {title: T, version: "1.0"}
security: [{rootScheme: []}]
paths:
  /x:
    get:
      operationId: doIt
      security: [{opScheme: []}]
      responses: {"204": {description: ok}}
components:
  securitySchemes:
    rootScheme: {type: http, scheme: bearer}
    opScheme: {type: http, scheme: bearer}
`
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(yaml), 0o600))

	var stdout, stderr bytes.Buffer
	code := runDefault([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen"), "-check"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	require.Contains(t, stderr.String(), "GET /x declares 'security'")
	require.Contains(t, stderr.String(), "root-level 'security'")
}

// TestSelectTasks_RoundTrip is a focused unit test for the helper since the
// behavioural tests above don't probe ordering when neither flag is set.
func TestSelectTasks_RoundTrip(t *testing.T) {
	got, err := selectTasks(defaultTasks, "", "")
	require.NoError(t, err)
	require.Equal(t, defaultTasks, got)

	got, err = selectTasks(defaultTasks, "client", "")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "client.go", got[0].filename)

	got, err = selectTasks(defaultTasks, "", "types,server,client,body_types,auth")
	require.NoError(t, err)
	require.Empty(t, got, "skipping every task yields an empty list")
}
