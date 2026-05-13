package spec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		contents string
		wantErr  string
	}{
		{
			name: "happy minimal 3.0.3",
			contents: `openapi: "3.0.3"
info:
  title: x
  version: "1"
paths: {}
`,
		},
		{
			name: "happy 3.1",
			contents: `openapi: "3.1.0"
info:
  title: x
  version: "1"
`,
		},
		{
			name:     "unsupported swagger 2",
			contents: "openapi: \"2.0\"\n",
			wantErr:  "unsupported OpenAPI version",
		},
		{
			name:     "missing openapi field",
			contents: "info: {title: x}\n",
			wantErr:  "unsupported OpenAPI version",
		},
		{
			name:     "malformed yaml",
			contents: "openapi: \"3.0\"\ninfo: {[bad",
			wantErr:  "parsing yaml",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.contents), 0o644))
			api, err := spec.Parse(path)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, api)
		})
	}
}

func TestParseFileMissing(t *testing.T) {
	_, err := spec.Parse(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.ErrorContains(t, err, "reading spec")
}

func TestResolveSchema(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{
			Schemas: map[string]*spec.Schema{
				"Pet": {Type: "object"},
			},
		},
	}
	tests := []struct {
		name     string
		ref      string
		wantName string
		wantErr  string
	}{
		{name: "happy", ref: "#/components/schemas/Pet", wantName: "Pet"},
		{name: "bad prefix", ref: "not/a/local/ref", wantErr: "unsupported $ref"},
		{name: "external ref", ref: "https://example.com/schema.json#/Pet", wantErr: "unsupported $ref"},
		{name: "missing schema", ref: "#/components/schemas/Missing", wantErr: "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, name, err := spec.ResolveSchema(tc.ref, api)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tc.wantName, name)
		})
	}
}

func TestRefName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"#/components/schemas/Pet", "Pet"},
		{"#/components/schemas/Nested/Pet", "Pet"},
		{"Pet", "Pet"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			require.Equal(t, tc.want, spec.RefName(tc.in))
		})
	}
}
