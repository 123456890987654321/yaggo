package spec_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/123456890987654321/yaggo/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestParse_OAS31_FullCoverage parses a representative OpenAPI 3.1.1 document
// that exercises the keywords whose semantics differ from 3.0:
//   - openapi: 3.1.1
//   - info.summary, info.license.identifier
//   - components.schemas with type: [..., "null"]
//   - exclusiveMinimum as a number
//   - const, examples (array), readOnly
//   - webhooks
//   - paths.PathItem with $ref + summary
//   - root-level servers, security, tags, externalDocs, jsonSchemaDialect
func TestParse_OAS31_FullCoverage(t *testing.T) {
	const oas31 = `openapi: "3.1.1"
jsonSchemaDialect: https://spec.openapis.org/oas/3.1/dialect/base
info:
  title: Demo
  summary: A short summary.
  description: A longer description.
  version: "1.0"
  license:
    name: Apache 2.0
    identifier: Apache-2.0
servers:
  - url: https://api.example.com/v1
    description: production
security:
  - BearerAuth: []
tags:
  - name: pets
    description: pet operations
externalDocs:
  url: https://example.com/docs
paths:
  /pets:
    summary: pet collection
    description: collection endpoint
    get:
      operationId: list
      deprecated: true
      security:
        - BearerAuth: []
      responses:
        "200":
          description: ok
webhooks:
  petUpdated:
    post:
      operationId: petUpdatedHook
      responses:
        "204":
          description: acknowledged
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
  schemas:
    NullableName:
      # 3.1: type-array nullability
      type: [string, "null"]
      examples:
        - alice
        - null
    NumericLimits:
      type: integer
      # 3.1: exclusiveMinimum is a NUMBER (was bool in 3.0)
      exclusiveMinimum: 0
      maximum: 100
    StringConst:
      const: "v1"
    ReadOnlyID:
      type: integer
      readOnly: true
    Pet:
      type: object
      required: [id, name]
      properties:
        id:
          type: integer
          readOnly: true
        name:
          type: string
        nickname:
          type: [string, "null"]
`

	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(oas31), 0o600))
	api, err := spec.Parse(path)
	require.NoError(t, err)

	// Top-level / info.
	require.Equal(t, "3.1.1", api.OpenAPI)
	require.Equal(t, "https://spec.openapis.org/oas/3.1/dialect/base", api.JSONSchemaDialect)
	require.Equal(t, "A short summary.", api.Info.Summary)
	require.NotNil(t, api.Info.License)
	require.Equal(t, "Apache-2.0", api.Info.License.Identifier)

	// Servers / security / tags / externalDocs.
	require.Len(t, api.Servers, 1)
	require.Equal(t, "https://api.example.com/v1", api.Servers[0].URL)
	require.Len(t, api.Security, 1)
	require.Contains(t, api.Security[0], "BearerAuth")
	require.Len(t, api.Tags, 1)
	require.Equal(t, "pets", api.Tags[0].Name)
	require.NotNil(t, api.ExternalDocs)
	require.Equal(t, "https://example.com/docs", api.ExternalDocs.URL)

	// PathItem summary, Operation deprecated, per-op security.
	pi, ok := api.Paths["/pets"]
	require.True(t, ok)
	require.Equal(t, "pet collection", pi.Summary)
	require.NotNil(t, pi.Get)
	require.True(t, pi.Get.Deprecated)
	require.Len(t, pi.Get.Security, 1)

	// Webhooks (3.1-specific).
	wh, ok := api.Webhooks["petUpdated"]
	require.True(t, ok)
	require.NotNil(t, wh.Post)

	// type: [string, "null"] is detected by both Primary() and Nullable().
	nullable := api.Components.Schemas["NullableName"]
	require.NotNil(t, nullable)
	require.Equal(t, "string", nullable.Type.Primary())
	require.True(t, nullable.Type.Nullable())
	require.True(t, nullable.IsNullable())
	require.Len(t, nullable.Examples, 2)

	// exclusiveMinimum as number (3.1).
	limits := api.Components.Schemas["NumericLimits"]
	require.NotNil(t, limits)
	require.True(t, limits.ExclusiveMinimum.Set)
	require.NotNil(t, limits.ExclusiveMinimum.Value)
	require.Equal(t, 0.0, *limits.ExclusiveMinimum.Value)

	// const keyword.
	require.Equal(t, "v1", api.Components.Schemas["StringConst"].Const)

	// readOnly.
	require.True(t, api.Components.Schemas["ReadOnlyID"].ReadOnly)

	// Pet nullable-via-array property is detected.
	pet := api.Components.Schemas["Pet"]
	require.True(t, pet.Properties["nickname"].IsNullable())
	require.False(t, pet.Properties["name"].IsNullable())
}

// TestParse_OAS30_Compat verifies that the 3.0 "nullable: true" form still
// works after the type field was switched to SchemaType.
func TestParse_OAS30_Compat(t *testing.T) {
	const oas30 = `openapi: "3.0.3"
info:
  title: Demo
  version: "1.0"
paths: {}
components:
  schemas:
    Old:
      type: string
      nullable: true
`
	path := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(path, []byte(oas30), 0o600))
	api, err := spec.Parse(path)
	require.NoError(t, err)

	old := api.Components.Schemas["Old"]
	require.Equal(t, "string", old.Type.Primary())
	require.False(t, old.Type.Nullable()) // 3.0 doesn't put "null" in type list
	require.True(t, old.Nullable)         // ...it sets the dedicated field instead
	require.True(t, old.IsNullable())     // ...and IsNullable() bridges the two
}

// TestExclusiveBound_3xCompat ensures both the 3.0 boolean form and the 3.1
// numeric form of exclusiveMinimum unmarshal cleanly.
func TestExclusiveBound_3xCompat(t *testing.T) {
	t.Run("3.0 boolean form", func(t *testing.T) {
		const y = `openapi: "3.0.3"
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: integer
      minimum: 5
      exclusiveMinimum: true
`
		path := filepath.Join(t.TempDir(), "spec.yaml")
		require.NoError(t, os.WriteFile(path, []byte(y), 0o600))
		api, err := spec.Parse(path)
		require.NoError(t, err)
		em := api.Components.Schemas["S"].ExclusiveMinimum
		require.True(t, em.Set)
		require.True(t, em.Bool)
		require.Nil(t, em.Value)
	})

	t.Run("3.1 numeric form", func(t *testing.T) {
		const y = `openapi: "3.1.1"
info: {title: t, version: "1"}
paths: {}
components:
  schemas:
    S:
      type: integer
      exclusiveMinimum: 5
`
		path := filepath.Join(t.TempDir(), "spec.yaml")
		require.NoError(t, os.WriteFile(path, []byte(y), 0o600))
		api, err := spec.Parse(path)
		require.NoError(t, err)
		em := api.Components.Schemas["S"].ExclusiveMinimum
		require.True(t, em.Set)
		require.False(t, em.Bool)
		require.NotNil(t, em.Value)
		require.Equal(t, 5.0, *em.Value)
	})
}

// TestSchemaType_Unmarshal covers the custom SchemaType unmarshaller directly.
func TestSchemaType_Unmarshal(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		want     spec.SchemaType
		primary  string
		nullable bool
	}{
		{"scalar", "type: string", spec.SchemaType{"string"}, "string", false},
		{"empty scalar", `type: ""`, nil, "", false},
		{"sequence single", `type: [string]`, spec.SchemaType{"string"}, "string", false},
		{"sequence with null", `type: [string, "null"]`, spec.SchemaType{"string", "null"}, "string", true},
		{"sequence null first", `type: ["null", integer]`, spec.SchemaType{"null", "integer"}, "integer", true},
		{"only null", `type: ["null"]`, spec.SchemaType{"null"}, "", true},
		{"absent", "format: int64", nil, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "spec.yaml")
			doc := "openapi: \"3.1.1\"\ninfo: {title: t, version: \"1\"}\npaths: {}\ncomponents:\n  schemas:\n    X:\n      " + tc.yaml + "\n"
			require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))
			api, err := spec.Parse(path)
			require.NoError(t, err)
			x := api.Components.Schemas["X"]
			require.Equal(t, tc.want, x.Type)
			require.Equal(t, tc.primary, x.Type.Primary())
			require.Equal(t, tc.nullable, x.Type.Nullable())
		})
	}
}
