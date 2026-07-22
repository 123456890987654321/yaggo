package spec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
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
			wantErr:  "parsing",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.contents), 0o600))
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
	require.ErrorContains(t, err, "reading")
}

func TestParseStrictRejectsUnknownKeys(t *testing.T) {
	contents := `openapi: "3.0.3"
info:
  title: x
  version: "1"
paths: {}
typo_at_root: 1
`
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	// Lenient (default) accepts.
	_, err := spec.Parse(path)
	require.NoError(t, err)

	// Strict surfaces the unknown key.
	_, err = spec.Parse(path, spec.WithStrict())
	require.ErrorContains(t, err, "typo_at_root")
}

func TestResolveSchemaErrorListsKnown(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Pet":  {Type: spec.SchemaType{"object"}},
		"User": {Type: spec.SchemaType{"object"}},
	}}}
	_, _, err := spec.ResolveSchema("#/components/schemas/Missing", api)
	require.ErrorContains(t, err, "not found")
	require.ErrorContains(t, err, "Pet")
	require.ErrorContains(t, err, "User")
}

// TestParse_AdditionalPropertiesBoolForms: JSON Schema lets
// `additionalProperties` be either a schema or a bool. Earlier yaggo's
// Schema unmarshaler accepted only the schema form, so common specs
// using `false` (strict) or `true` (default-explicit) hard-errored at
// parse time.
func TestParse_AdditionalPropertiesBoolForms(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name        string
		body        string
		wantAllowed bool
		wantSet     bool
		wantSchema  bool
	}{
		{
			name:        "true",
			body:        "openapi: \"3.0.3\"\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    O:\n      type: object\n      additionalProperties: true\n",
			wantAllowed: true,
			wantSet:     true,
		},
		{
			name:        "false",
			body:        "openapi: \"3.0.3\"\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    S:\n      type: object\n      additionalProperties: false\n",
			wantAllowed: false,
			wantSet:     true,
		},
		{
			name:        "schema",
			body:        "openapi: \"3.0.3\"\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    M:\n      type: object\n      additionalProperties:\n        type: string\n",
			wantAllowed: true,
			wantSet:     true,
			wantSchema:  true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o600))
			api, err := spec.Parse(path)
			require.NoError(t, err)

			var found *spec.Schema
			for _, s := range api.Components.Schemas {
				found = s
			}
			require.NotNil(t, found)
			require.Equal(t, tc.wantSet, found.AdditionalProperties.Set)
			require.Equal(t, tc.wantAllowed, found.AdditionalProperties.Allowed)
			if tc.wantSchema {
				require.NotNil(t, found.AdditionalProperties.Schema)
			} else {
				require.Nil(t, found.AdditionalProperties.Schema)
			}
			// AdditionalPropertiesForbidden only true for `false`.
			require.Equal(t, tc.name == "false", found.AdditionalPropertiesForbidden())
		})
	}
}

func TestResolveSchema(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{
			Schemas: map[string]*spec.Schema{
				"Pet": {Type: spec.SchemaType{"object"}},
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
