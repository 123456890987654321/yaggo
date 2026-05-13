package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
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

func TestRunHappyPath(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))

	out := filepath.Join(dir, "gen")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out, "-package", "tiny"}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	// types.go, server.go, client.go should exist; body_types.go is skipped (no inline bodies).
	for _, f := range []string{"types.go", "server.go", "client.go"} {
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
	code := run(nil, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "-spec is required")
}

func TestRunBadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-not-a-flag"}, &stdout, &stderr)
	require.Equal(t, 2, code)
	require.Contains(t, stderr.String(), "flag provided but not defined")
}

func TestRunSpecParseError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", "/no/such/file.yaml"}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "parsing spec")
}

func TestRunMkdirError(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))

	// Use an existing file as the requested -out → MkdirAll fails.
	blocker := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", blocker}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "creating output dir")
}

func TestRunGenerateError(t *testing.T) {
	// Swap one task with a stub that returns an error; restore after.
	orig := defaultTasks
	t.Cleanup(func() { defaultTasks = orig })
	defaultTasks = []genTask{{
		filename: "types.go",
		fn: func(_ io.Writer, _ *spec.OpenAPI, _ string) error {
			return errors.New("gen fail")
		},
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", filepath.Join(dir, "gen")}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "generating types.go")
}

func TestRunFormatError(t *testing.T) {
	orig := defaultTasks
	t.Cleanup(func() { defaultTasks = orig })
	defaultTasks = []genTask{{
		filename: "bad.go",
		fn: func(w io.Writer, _ *spec.OpenAPI, _ string) error {
			_, err := io.WriteString(w, "this is not valid Go {{")
			return err
		},
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))
	out := filepath.Join(dir, "gen")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "formatting bad.go")
	// Unformatted source is written for debugging.
	raw, err := os.ReadFile(filepath.Join(out, "bad.go"))
	require.NoError(t, err)
	require.Equal(t, "this is not valid Go {{", string(raw))
}

func TestRunWriteError(t *testing.T) {
	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))

	out := filepath.Join(dir, "gen")
	require.NoError(t, os.MkdirAll(out, 0o755))
	// Make types.go a directory so WriteFile fails on it.
	require.NoError(t, os.MkdirAll(filepath.Join(out, "types.go"), 0o755))

	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out}, &stdout, &stderr)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), "writing")
}

func TestRunSkipsEmptyOutput(t *testing.T) {
	orig := defaultTasks
	t.Cleanup(func() { defaultTasks = orig })
	defaultTasks = []genTask{{
		filename: "empty.go",
		fn:       func(_ io.Writer, _ *spec.OpenAPI, _ string) error { return nil },
	}}

	dir := t.TempDir()
	specFile := filepath.Join(dir, "spec.yaml")
	require.NoError(t, os.WriteFile(specFile, []byte(minimalPetstoreYAML), 0o644))
	out := filepath.Join(dir, "gen")

	var stdout, stderr bytes.Buffer
	code := run([]string{"-spec", specFile, "-out", out}, &stdout, &stderr)
	require.Equal(t, 0, code)
	_, err := os.Stat(filepath.Join(out, "empty.go"))
	require.True(t, os.IsNotExist(err), "empty output should not be written")
	require.True(t, strings.HasSuffix(strings.TrimSpace(stderr.String()), "") || stderr.Len() == 0)
}
