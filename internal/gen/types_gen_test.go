package gen

import (
	"bytes"
	"errors"
	"go/format"
	"strings"
	"testing"

	"github.com/123456890987654321/yago/internal/spec"
	"github.com/stretchr/testify/require"
)

// assertGofmtsAndContains formats the generated code (proving syntactic validity)
// and asserts each substring is present somewhere in the output.
func assertGofmtsAndContains(t *testing.T, src []byte, wants []string) []byte {
	t.Helper()
	formatted, err := format.Source(src)
	require.NoError(t, err, "generated code is not valid Go:\n%s", src)
	for _, w := range wants {
		require.Contains(t, string(formatted), w)
	}
	return formatted
}

func TestGenerateTypesEnum(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"PetStatus": {Type: "string", Enum: []any{"available", "pending", "sold"}},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "petstore"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		"package petstore",
		"type PetStatus string",
		"func (v PetStatus) Validate() error",
		"case PetStatusAvailable, PetStatusPending, PetStatusSold:",
	}))
	require.Regexp(t, `PetStatusAvailable\s+PetStatus\s+=\s+"available"`, out)
	require.Regexp(t, `PetStatusPending\s+PetStatus\s+=\s+"pending"`, out)
	require.Regexp(t, `PetStatusSold\s+PetStatus\s+=\s+"sold"`, out)
}

func TestGenerateTypesStruct(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"PetStatus": {Type: "string", Enum: []any{"available", "sold"}},
		"NewPet": {
			Type:        "object",
			Description: "an unsaved pet",
			Required:    []string{"name", "status"},
			Properties: map[string]*spec.Schema{
				"name":   {Type: "string", Description: "the pet's name"},
				"tag":    {Type: "string"},
				"status": {Ref: "#/components/schemas/PetStatus"},
				"tags":   {Type: "array", Items: &spec.Schema{Type: "string"}},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		"// NewPet an unsaved pet",
		"type NewPet struct {",
		"// the pet's name",
		"func (v NewPet) Validate() error",
		`if v.Name == ""`,
		"if err := v.Status.Validate(); err != nil",
	}))
	require.Regexp(t, `Name\s+string\s+`+"`"+`json:"name"`+"`", out)
	// Required ref field has no pointer wrap.
	require.Regexp(t, `Status\s+PetStatus\s+`+"`"+`json:"status"`+"`", out)
	// Tags is an optional slice — should NOT emit a nil check (not required).
	require.NotContains(t, out, `field 'tags' is required`)
}

func TestGenerateTypesStructRequiredArray(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Bag": {
			Type:     "object",
			Required: []string{"items", "lookup"},
			Properties: map[string]*spec.Schema{
				"items":  {Type: "array", Items: &spec.Schema{Type: "string"}},
				"lookup": {Type: "object", AdditionalProperties: &spec.Schema{Type: "integer"}},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	assertGofmtsAndContains(t, buf.Bytes(), []string{
		"if v.Items == nil",
		"field 'items' is required",
		"if v.Lookup == nil",
		"field 'lookup' is required",
	})
}

func TestGenerateTypesAllOf(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"NewPet": {Type: "object", Required: []string{"name"}, Properties: map[string]*spec.Schema{
			"name": {Type: "string"},
		}},
		"Pet": {AllOf: []*spec.Schema{
			{Ref: "#/components/schemas/NewPet"},
			{Type: "object", Required: []string{"id"}, Properties: map[string]*spec.Schema{
				"id": {Type: "integer", Format: "int64"},
			}},
		}},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	assertGofmtsAndContains(t, buf.Bytes(), []string{
		"type Pet struct {",
		"Id   int64",
		"Name string",
		"func (v Pet) Validate() error",
	})
}

func TestGenerateTypesAlias(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"ID": {Type: "integer", Format: "int64"},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	assertGofmtsAndContains(t, buf.Bytes(), []string{"type ID = int64"})
}

func TestGenerateTypesEmpty(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: nil}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	require.Contains(t, buf.String(), "package p")
}

func TestHasValidation(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Enum":     {Type: "string", Enum: []any{"a"}},
		"Plain":    {Type: "string"},
	}}}
	tests := []struct {
		name string
		s    *spec.Schema
		want bool
	}{
		{"required field", &spec.Schema{Required: []string{"x"}}, true},
		{"enum ref field", &spec.Schema{Properties: map[string]*spec.Schema{"x": {Ref: "#/components/schemas/Enum"}}}, true},
		{"plain ref field", &spec.Schema{Properties: map[string]*spec.Schema{"x": {Ref: "#/components/schemas/Plain"}}}, false},
		{"bad ref field", &spec.Schema{Properties: map[string]*spec.Schema{"x": {Ref: "bad/ref"}}}, false},
		{"nothing", &spec.Schema{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, hasValidation(tc.s, api))
		})
	}
}

func TestEffectiveSchema(t *testing.T) {
	api := &spec.OpenAPI{}
	plain := &spec.Schema{Type: "object", Properties: map[string]*spec.Schema{"x": {Type: "string"}}}
	require.Same(t, plain, effectiveSchema(plain, api))

	merged := effectiveSchema(&spec.Schema{AllOf: []*spec.Schema{
		{Type: "object", Properties: map[string]*spec.Schema{"a": {Type: "string"}}},
		{Type: "object", Properties: map[string]*spec.Schema{"b": {Type: "string"}}},
	}}, api)
	require.Contains(t, merged.Properties, "a")
	require.Contains(t, merged.Properties, "b")
}

// failingWriter returns the given error on every Write call. Used to exercise
// the printer's error-latch path.
type failingWriter struct{ err error }

func (w *failingWriter) Write(_ []byte) (int, error) { return 0, w.err }

func TestPrinterLatchesError(t *testing.T) {
	want := errors.New("boom")
	p := &printer{w: &failingWriter{err: want}}
	p.line("first")
	require.ErrorIs(t, p.err, want)

	// Subsequent calls must be no-ops and not overwrite the error.
	p.line("second")
	p.linef("ignored %d", 1)
	require.ErrorIs(t, p.err, want)
}

func TestGenerateTypesPropagatesWriterError(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Pet": {Type: "object", Properties: map[string]*spec.Schema{"name": {Type: "string"}}},
	}}}
	err := GenerateTypes(&failingWriter{err: errors.New("io fail")}, api, "p")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "io fail"))
}
