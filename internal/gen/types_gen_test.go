package gen

import (
	"bytes"
	"errors"
	"go/format"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/123456890987654321/yaggo/internal/spec"
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
		"PetStatus": {Type: spec.SchemaType{"string"}, Enum: []any{"available", "pending", "sold"}},
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
		"PetStatus": {Type: spec.SchemaType{"string"}, Enum: []any{"available", "sold"}},
		"NewPet": {
			Type:        spec.SchemaType{"object"},
			Description: "an unsaved pet",
			Required:    []string{"name", "status"},
			Properties: map[string]*spec.Schema{
				"name":   {Type: spec.SchemaType{"string"}, Description: "the pet's name"},
				"tag":    {Type: spec.SchemaType{"string"}},
				"status": {Ref: "#/components/schemas/PetStatus"},
				"tags":   {Type: spec.SchemaType{"array"}, Items: &spec.Schema{Type: spec.SchemaType{"string"}}},
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
			Type:     spec.SchemaType{"object"},
			Required: []string{"items", "lookup"},
			Properties: map[string]*spec.Schema{
				"items":  {Type: spec.SchemaType{"array"}, Items: &spec.Schema{Type: spec.SchemaType{"string"}}},
				"lookup": {Type: spec.SchemaType{"object"}, AdditionalProperties: spec.AdditionalProperties{Schema: &spec.Schema{Type: spec.SchemaType{"integer"}}, Set: true, Allowed: true}},
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
		"NewPet": {Type: spec.SchemaType{"object"}, Required: []string{"name"}, Properties: map[string]*spec.Schema{
			"name": {Type: spec.SchemaType{"string"}},
		}},
		"Pet": {AllOf: []*spec.Schema{
			{Ref: "#/components/schemas/NewPet"},
			{Type: spec.SchemaType{"object"}, Required: []string{"id"}, Properties: map[string]*spec.Schema{
				"id": {Type: spec.SchemaType{"integer"}, Format: "int64"},
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
		"ID": {Type: spec.SchemaType{"integer"}, Format: "int64"},
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
		"Enum":  {Type: spec.SchemaType{"string"}, Enum: []any{"a"}},
		"Plain": {Type: spec.SchemaType{"string"}},
	}}}
	tests := []struct {
		name string
		s    *spec.Schema
		want bool
	}{
		// "required" alone now triggers Validate only when at least one
		// required field is string/slice/map (the kinds we can zero-check).
		// A required field that doesn't exist in Properties (or whose type
		// is int/bool/number) yields no emittable check, so hasValidation
		// reports false — Validate() would otherwise be an empty stub.
		{"required string field present", &spec.Schema{
			Required:   []string{"x"},
			Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}},
		}, true},
		{"required only int (no enforceable check)", &spec.Schema{
			Required:   []string{"x"},
			Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"integer"}}},
		}, false},
		{"required field missing from properties", &spec.Schema{Required: []string{"x"}}, false},
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
	plain := &spec.Schema{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}}}
	require.Same(t, plain, effectiveSchema(plain, api))

	merged := effectiveSchema(&spec.Schema{AllOf: []*spec.Schema{
		{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"a": {Type: spec.SchemaType{"string"}}}},
		{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"b": {Type: spec.SchemaType{"string"}}}},
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

func TestGenerateTypesXGoType(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Timestamp": {
			Type:      spec.SchemaType{"string"},
			Format:    "date-time",
			XGoType:   "time.Time",
			XGoImport: "time",
		},
		"UserID": {
			// x-go-type without an import (already in scope or stdlib type).
			XGoType: "string",
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		"package p",
		`import "time"`,
		"type Timestamp = time.Time",
		"type UserID = string",
	}))
	// No struct body, no constants, no Validate() emitted for the override.
	require.NotContains(t, out, "type Timestamp struct")
	require.NotContains(t, out, "func (v Timestamp)")
}

func TestGenerateTypesXGoTypeWithExternalImport(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"PetID": {
			XGoType:   "uuid.UUID",
			XGoImport: "github.com/google/uuid",
		},
		"Name": {Type: spec.SchemaType{"string"}},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	assertGofmtsAndContains(t, buf.Bytes(), []string{
		`"github.com/google/uuid"`,
		"type PetID = uuid.UUID",
		"type Name = string",
	})
}

func TestGenerateTypesStringMinMaxPattern(t *testing.T) {
	min3, max10 := 3, 10
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"username"},
			Properties: map[string]*spec.Schema{
				"username": {
					Type:      spec.SchemaType{"string"},
					MinLength: &min3,
					MaxLength: &max10,
					Pattern:   `^[a-z][a-z0-9_]*$`,
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		`"regexp"`,
		`"unicode/utf8"`,
		"pattern_User_Username = regexp.MustCompile(",
		"utf8.RuneCountInString(v.Username)",
		"must be at least 3 characters",
		"must be at most 10 characters",
		"does not match required pattern",
	}))
	// Required string: no pointer-guard around the constraint block.
	require.NotContains(t, out, "if v.Username != nil {")
}

func TestGenerateTypesOptionalStringWithLength(t *testing.T) {
	min1 := 1
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"nickname": {Type: spec.SchemaType{"string"}, MinLength: &min1},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		"if v.Nickname != nil {",
		"utf8.RuneCountInString((*v.Nickname))",
	}))
	require.Contains(t, out, "Nickname *string")
}

func TestGenerateTypesNumericBounds(t *testing.T) {
	min := 0.0
	max := 100.0
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Score": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"value"},
			Properties: map[string]*spec.Schema{
				"value":  {Type: spec.SchemaType{"integer"}, Minimum: &min, Maximum: &max},
				"weight": {Type: spec.SchemaType{"number"}, Minimum: &min, Maximum: &max},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	out := string(assertGofmtsAndContains(t, buf.Bytes(), []string{
		"if v.Value < 0 {",
		"if v.Value > 100 {",
		"must be >= 0",
		"must be <= 100",
	}))
	// Number bounds are float literals.
	require.Contains(t, out, "0.0")
	require.Contains(t, out, "100.0")
}

func TestGenerateTypesArrayItemBounds(t *testing.T) {
	min, max := 1, 5
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Tags": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"items"},
			Properties: map[string]*spec.Schema{
				"items": {
					Type:     spec.SchemaType{"array"},
					Items:    &spec.Schema{Type: spec.SchemaType{"string"}},
					MinItems: &min,
					MaxItems: &max,
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	assertGofmtsAndContains(t, buf.Bytes(), []string{
		"if l := len(v.Items); l < 1 {",
		"if l := len(v.Items); l > 5 {",
		"must have at least 1 items",
		"must have at most 5 items",
	})
}

func TestFormatBound(t *testing.T) {
	require.Equal(t, "0", formatBound(0, "integer"))
	require.Equal(t, "-3", formatBound(-3, "integer"))
	require.Equal(t, "1.5", formatBound(1.5, "number"))
	require.Equal(t, "0.0", formatBound(0, "number"))
}

func TestImportSet_BlockEmission(t *testing.T) {
	type tc struct {
		name  string
		paths []string
		want  []string
	}
	for _, c := range []tc{
		{"empty", nil, []string{}},
		{"single stdlib", []string{"fmt"}, []string{`import "fmt"`}},
		{"single external", []string{"github.com/x/y"}, []string{`import "github.com/x/y"`}},
		{"multi mixed", []string{"fmt", "regexp", "github.com/x/y"}, []string{"import (", `"fmt"`, `"regexp"`, `"github.com/x/y"`, ")"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			imports := newImportSet()
			for _, p := range c.paths {
				imports.add(p)
			}
			var buf bytes.Buffer
			imports.writeBlock(&printer{w: &buf})
			for _, w := range c.want {
				require.Contains(t, buf.String(), w)
			}
		})
	}
}

func TestHasValidation_CycleSafe(t *testing.T) {
	// A → B (ref) → A: cyclic schemas must not blow the stack.
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"A": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"b": {Ref: "#/components/schemas/B"},
			},
		},
		"B": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"a": {Ref: "#/components/schemas/A"},
			},
		},
	}}}
	// hasValidation must terminate; the exact answer doesn't matter — what we
	// care about is that the call returns instead of recursing forever.
	done := make(chan bool, 1)
	go func() { _ = hasValidation(api.Components.Schemas["A"], api); done <- true }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("hasValidation did not terminate on a cyclic spec")
	}

	// Generation should also succeed end-to-end.
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	_, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "cyclic spec produced invalid Go:\n%s", buf.String())
}

func TestMergeAllOf_CycleSafe(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"A": {AllOf: []*spec.Schema{
			{Ref: "#/components/schemas/B"},
			{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"x": {Type: spec.SchemaType{"string"}}}},
		}},
		"B": {AllOf: []*spec.Schema{
			{Ref: "#/components/schemas/A"},
			{Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"y": {Type: spec.SchemaType{"string"}}}},
		}},
	}}}
	done := make(chan bool, 1)
	go func() {
		_ = mergeAllOf(api.Components.Schemas["A"].AllOf, api)
		done <- true
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mergeAllOf did not terminate on a cyclic allOf chain")
	}
}

// Security: spec values must never reach generated source as raw bytes that
// could break out of context. Each test below feeds a deliberately
// adversarial spec value and asserts GenerateTypes refuses with a clear error.

func TestSecurity_SchemaNameInjectionRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		// The schema name is a YAML map key — yaml.v3 will accept anything.
		// Without identifier validation this would be interpolated as
		//   type X; func init() { /* … */ } struct {…}
		// and go/format would write the unformatted bytes to disk for debugging.
		`X; func init() { _ = "pwned" }`: {Type: spec.SchemaType{"object"}},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

func TestSecurity_PropertyNameInjectionRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Pet": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				`name"; var pwned = "x`: {Type: spec.SchemaType{"string"}},
			},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

func TestSecurity_EnumValueInjectionRejected(t *testing.T) {
	// Enum values become the SUFFIX of typed constants (e.g. "PetStatusAvailable").
	// A malicious value that yields a non-identifier suffix must be rejected.
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"PetStatus": {
			Type: spec.SchemaType{"string"},
			Enum: []any{"available", `bad"; func init() { _ = "x" }; var z = "`},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

func TestSecurity_XGoTypeInjectionRejected(t *testing.T) {
	// x-go-type is emitted unquoted as the RHS of `type Foo = ...`. Without
	// validation the spec author could inject arbitrary top-level Go: a
	// terminating semicolon followed by func init() {} runs at import time.
	tests := []struct {
		name    string
		xGoType string
	}{
		{"semicolon-then-init", `string; func init() { _ = "pwned" }`},
		{"open-brace", `struct{ X int }`},
		{"function-call", `pkg.New()`},
		{"backtick", "string`evil`"},
		{"quotes", `"string"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
				"Foo": {XGoType: tc.xGoType},
			}}}
			err := GenerateTypes(&bytes.Buffer{}, api, "p")
			require.Error(t, err, "x-go-type %q should be rejected", tc.xGoType)
			require.Contains(t, err.Error(), "x-go-type")
		})
	}
}

func TestSecurity_XGoTypeLegitimateAccepted(t *testing.T) {
	// Real-world plain type expressions must still pass the validator.
	tests := []string{
		"time.Time",
		"*uuid.UUID",
		"[]string",
		"map[string]int",
		"Foo[Bar]",
		"[]*pkg.Type",
	}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
				"Foo": {XGoType: expr, XGoImport: "example.com/pkg"},
			}}}
			err := GenerateTypes(&bytes.Buffer{}, api, "p")
			require.NoErrorf(t, err, "x-go-type %q is a plain type expression and must be accepted", expr)
		})
	}
}

func TestSecurity_XGoImportInjectionRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Foo": {XGoType: "time.Time", XGoImport: `time"; import _ "evil`},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "x-go-import")
}

func TestSecurity_InvalidRegexRejected(t *testing.T) {
	// A pattern that uses a PCRE-only feature (lookahead) would compile in
	// Perl but panic in Go's RE2-based regexp.MustCompile at the consumer's
	// package init — denying service to anyone who imports the generated code.
	// yaggo must reject it at generation time.
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"name"},
			Properties: map[string]*spec.Schema{
				"name": {Type: spec.SchemaType{"string"}, Pattern: `^(?=foo)bar$`}, // lookahead
			},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a valid Go (RE2) regex")
}

func TestSecurity_OperationIDInjectionRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: `getX; func init() { /* pwned */ }`,
			Responses:   map[string]spec.Response{"200": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

func TestSecurity_ParameterNameInjectionRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "list",
			Parameters: []spec.Parameter{
				{Name: `q"; os.Exit(0); v := "`, In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestSecurity_DanglingRefInPropertyRejected: a $ref to a non-existent
// component schema whose name would inject Go was previously slipping past
// validation. schemaToGoType uses toGoName(RefName(ref)) as a raw identifier
// in struct field type position, so the unvalidated ref name landed in source.
func TestSecurity_DanglingRefInPropertyRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Good": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"bad": {Ref: `#/components/schemas/X; var pwned = "x"`},
			},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestCollision_HeaderCaseInsensitiveDuplicateRejected: HTTP headers are
// case-insensitive on the wire (RFC 9110); two spec entries differing
// only in case both compile but get the same r.Header.Get value at
// runtime. Almost certainly a spec typo; reject early.
func TestCollision_HeaderCaseInsensitiveDuplicateRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "doIt",
			Parameters: []spec.Parameter{
				{Name: "X-Trace-ID", In: "header", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "x-trace-id", In: "header", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "case")
}

// TestCollision_ParamGoKeywordRejected: a parameter whose Go local form
// matches a Go keyword (func / type / map / range / …) can never be a
// valid identifier in a signature. yaggo must reject up front rather
// than letting the user discover a syntax error at `go build` time.
func TestCollision_ParamGoKeywordRejected(t *testing.T) {
	for _, kw := range []string{"func", "type", "map", "range", "chan", "interface", "select"} {
		t.Run(kw, func(t *testing.T) {
			api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
				"/x": {Get: &spec.Operation{
					OperationID: "doIt",
					Parameters: []spec.Parameter{
						{Name: kw, In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
					},
					Responses: map[string]spec.Response{"204": {}},
				}},
			}}
			err := GenerateServer(&bytes.Buffer{}, api, "p")
			require.Error(t, err)
			require.Contains(t, err.Error(), "Go keyword")
		})
	}
}

// TestCollision_PropertyNameAfterToGoNameRejected: two properties whose
// PascalCase forms collide (Name + name → both "Name") would produce a
// struct with duplicate fields. Catch at generation time.
func TestCollision_PropertyNameAfterToGoNameRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"Name": {Type: spec.SchemaType{"string"}},
				"name": {Type: spec.SchemaType{"string"}},
			},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "both produce Go field")
}

// TestCollision_OperationIDDuplicateRejected: two operations with the same
// operationId across paths would emit duplicate methods.
func TestCollision_OperationIDDuplicateRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/a": {Get: &spec.Operation{OperationID: "doIt", Responses: map[string]spec.Response{"204": {}}}},
		"/b": {Get: &spec.Operation{OperationID: "doIt", Responses: map[string]spec.Response{"204": {}}}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "operation name \"DoIt\" is produced by both")
}

// TestCollision_ParamNameReservedRejected: a path/query/header parameter
// whose local name lands on a reserved handler/client identifier (w, r,
// ctx, body, params, headers) must be rejected with a clear message.
func TestCollision_ParamNameReservedRejected(t *testing.T) {
	for _, name := range []string{"r", "w", "body", "params", "headers"} {
		t.Run(name, func(t *testing.T) {
			api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
				"/x/{" + name + "}": {Get: &spec.Operation{
					OperationID: "doIt",
					Parameters: []spec.Parameter{
						{Name: name, In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
					},
					Responses: map[string]spec.Response{"204": {}},
				}},
			}}
			err := GenerateServer(&bytes.Buffer{}, api, "p")
			require.Error(t, err)
			require.Contains(t, err.Error(), "reserved")
		})
	}
}

// TestCollision_DuplicateParamLocalRejected: two parameters whose Go
// locals would collide (e.g. one in path, one in query) should be rejected.
func TestCollision_DuplicateParamLocalRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x/{id}": {Get: &spec.Operation{
			OperationID: "doIt",
			Parameters: []spec.Parameter{
				{Name: "id", In: "path", Required: true, Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
				{Name: "id", In: "query", Schema: &spec.Schema{Type: spec.SchemaType{"string"}}},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "both resolve to local")
}

// TestSecurity_DanglingRefInOperationParamRejected: a $ref inside an
// operation parameter schema must be validated. The path doesn't go through
// components.schemas iteration so an earlier audit missed it.
func TestSecurity_DanglingRefInOperationParamRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "list",
			Parameters: []spec.Parameter{
				{Name: "q", In: "query", Schema: &spec.Schema{Ref: `#/components/schemas/Bad; var pwned = "x"`}},
			},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestSecurity_DanglingRefInRequestBodyRejected: bodyTypeName extracts the
// type name from the request body's $ref. A malformed target leaks straight
// into the function signature of the generated handler.
func TestSecurity_DanglingRefInRequestBodyRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "create",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{Ref: `#/components/schemas/Bad type{}; func init(){}`}},
			}},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	err := GenerateServer(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestSecurity_DanglingRefInResponseRejected: success-response schemas feed
// ReturnType through the same RefName → identifier path as request bodies.
func TestSecurity_DanglingRefInResponseRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "get",
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Ref: `#/components/schemas/Bad; func init(){}`}},
				}},
			},
		}},
	}}
	err := GenerateClient(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestSecurity_RefInArrayItemsRejected: array Items is one of the recursive
// schema positions schemaToGoType descends through; a malformed ref nested
// inside an array must still be caught.
func TestSecurity_RefInArrayItemsRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Wrap": {
			Type: spec.SchemaType{"array"},
			Items: &spec.Schema{
				Ref: `#/components/schemas/X"; var z = "y`,
			},
		},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not produce a valid Go identifier")
}

// TestSecurity_DanglingValidRefRejected: a $ref whose target name is a
// syntactically valid Go identifier but doesn't exist in components/schemas
// must fail at generation time. Earlier the generator wrote a file that
// referenced an undefined type — surfacing the error here turns it into a
// clear yaggo error instead of a confusing `go build` failure.
func TestSecurity_DanglingValidRefRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Get: &spec.Operation{
			OperationID: "getIt",
			Responses: map[string]spec.Response{
				"200": {Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/MissingType"}},
				}},
			},
		}},
	}}
	err := GenerateClient(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "MissingType")
	require.Contains(t, err.Error(), "not found")
}

// TestSecurity_ValidRefsStillAccepted: the new ref-walker must not regress
// the happy path — a normal ref to a real component must still pass.
func TestSecurity_ValidRefsStillAccepted(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Pet": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"name": {Type: spec.SchemaType{"string"}},
			},
		},
		"Wrap": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"pet": {Ref: "#/components/schemas/Pet"},
			},
		},
	}}}
	require.NoError(t, GenerateTypes(&bytes.Buffer{}, api, "p"))
}

// TestSecurity_InvalidRegexInInlineBodyRejected: emitBodyPatternVars used to
// emit regexp.MustCompile() for inline request body patterns without
// validating them. A malformed pattern survived until the consumer's package
// init time, then panicked.
func TestSecurity_InvalidRegexInInlineBodyRejected(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "create",
			RequestBody: &spec.RequestBody{Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{
					Type: spec.SchemaType{"object"},
					Properties: map[string]*spec.Schema{
						"name": {Type: spec.SchemaType{"string"}, Pattern: `^(?=foo)bar$`}, // PCRE lookahead → invalid RE2
					},
				}},
			}},
			Responses: map[string]spec.Response{"200": {}},
		}},
	}}
	err := GenerateBodyTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a valid Go (RE2) regex")
}

// TestGenerateTypes_PureRefAlias: a top-level component whose body is just a
// $ref to another component must emit a Go type alias. Earlier the alias
// branch only fired when Type was non-empty, so a ref-only schema produced
// no output and downstream references to the name failed to compile.
func TestGenerateTypes_PureRefAlias(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"UserID": {Type: spec.SchemaType{"integer"}, Format: "int64"},
		"PetID":  {Ref: "#/components/schemas/UserID"},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "type UserID = int64")
	require.Contains(t, s, "type PetID = UserID")
}

// TestGenerateTypes_AllOfMergesSiblingProperties: a schema declaring BOTH
// allOf and direct properties must yield a struct with all fields. Earlier
// writeStruct honoured allOf only and silently dropped the parent's own
// properties — JSON Schema semantics require the intersection.
func TestGenerateTypes_AllOfMergesSiblingProperties(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Base": {
			Type:       spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{"id": {Type: spec.SchemaType{"integer"}, Format: "int64"}},
		},
		"Pet": {
			AllOf:      []*spec.Schema{{Ref: "#/components/schemas/Base"}},
			Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "type Pet struct {")
	require.Contains(t, s, "Id ")   // inherited from Base via allOf
	require.Contains(t, s, "Name ") // sibling property must survive
}

// TestGenerateTypes_InlineEnumValidation: an inline string enum on a
// property must emit a switch-based validation check. Earlier the inline
// enum was silently dropped because the validation pipeline only handled
// enums reached via $ref to a named component.
func TestGenerateTypes_InlineEnumValidation(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"status"},
			Properties: map[string]*spec.Schema{
				"status": {Type: spec.SchemaType{"string"}, Enum: []any{"active", "inactive", "banned"}},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "func (v User) Validate() error")
	require.Contains(t, s, "switch v.Status {")
	require.Contains(t, s, `case "active", "inactive", "banned":`)
	require.Contains(t, s, `'status' must be one of [active, inactive, banned]`)
}

// TestGenerateTypes_OptionalInlineEnumGuarded: optional fields are
// pointers; the inline-enum check must guard with a nil check and
// dereference before comparing.
func TestGenerateTypes_OptionalInlineEnumGuarded(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"status": {Type: spec.SchemaType{"string"}, Enum: []any{"a", "b"}},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "if v.Status != nil {")
	require.Contains(t, s, "switch *v.Status {")
}

// TestGenerateTypes_RequiredOnlyIntsSkipValidate: when the only validation
// trigger is a required int/bool/number field, no Validate() should be
// emitted — yaggo cannot detect "missing" for non-pointer scalars, so the
// empty stub is misleading.
func TestGenerateTypes_RequiredOnlyIntsSkipValidate(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Base": {
			Type:       spec.SchemaType{"object"},
			Required:   []string{"id"},
			Properties: map[string]*spec.Schema{"id": {Type: spec.SchemaType{"integer"}, Format: "int64"}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.NotContains(t, s, "func (v Base) Validate()")
}

// TestGenerateTypes_NaNBoundEmitsMathNaN: minimum/maximum: .nan / ±inf
// must NOT produce the broken `NaN.0` literal. formatBound now returns
// math.NaN() / math.Inf(±1) and collectValidationImports adds the math
// import. Round-trip gofmt confirms validity.
func TestGenerateTypes_NaNBoundEmitsMathNaN(t *testing.T) {
	nan := math.NaN()
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Score": {
			Type:       spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{"v": {Type: spec.SchemaType{"number"}, Minimum: &nan}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "math.NaN()")
	require.Contains(t, s, `"math"`)
	require.NotContains(t, s, "NaN.0")
}

// TestGenerateTypes_InfBoundEmitsMathInf: +Inf / -Inf get the matching
// math.Inf(±1) helper.
func TestGenerateTypes_InfBoundEmitsMathInf(t *testing.T) {
	pos := math.Inf(1)
	neg := math.Inf(-1)
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Range": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"hi": {Type: spec.SchemaType{"number"}, Maximum: &pos},
				"lo": {Type: spec.SchemaType{"number"}, Minimum: &neg},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "math.Inf(1)")
	require.Contains(t, s, "math.Inf(-1)")
}

// TestGenerateTypes_NumericEnumNamedType: an integer enum must emit a
// named type + const block + Validate, mirroring the existing string-enum
// codegen. Earlier integer enums silently collapsed to a plain type alias.
func TestGenerateTypes_NumericEnumNamedType(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Priority": {
			Type: spec.SchemaType{"integer"},
			Enum: []any{1, 2, 3},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "type Priority int")
	require.Contains(t, s, "Priority1 Priority = 1")
	require.Contains(t, s, "Priority2 Priority = 2")
	require.Contains(t, s, "Priority3 Priority = 3")
	require.Contains(t, s, "func (v Priority) Validate() error")
	require.Contains(t, s, "case Priority1, Priority2, Priority3:")
	require.Contains(t, s, "invalid Priority value:")
}

// TestGenerateTypes_BooleanEnumNamedType: bool enum produces True/False
// suffixed constants and a switch over them.
func TestGenerateTypes_BooleanEnumNamedType(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Flag": {Type: spec.SchemaType{"boolean"}, Enum: []any{true, false}},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "type Flag bool")
	// gofmt pads the const block; match on the unpadded substrings.
	require.Regexp(t, `FlagTrue\s+Flag = true`, s)
	require.Regexp(t, `FlagFalse\s+Flag = false`, s)
}

// TestGenerateTypes_ExclusiveAndMultipleOfChecks: exclusiveMinimum,
// exclusiveMaximum, multipleOf — all three keywords were previously
// dropped; verify the generated Validate covers them.
func TestGenerateTypes_ExclusiveAndMultipleOfChecks(t *testing.T) {
	zero := 0.0
	one := 1.0
	step := 0.25
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Score": {
			Type:     spec.SchemaType{"object"},
			Required: []string{"v"},
			Properties: map[string]*spec.Schema{
				"v": {
					Type:             spec.SchemaType{"number"},
					ExclusiveMinimum: spec.ExclusiveBound{Value: &zero, Set: true},
					ExclusiveMaximum: spec.ExclusiveBound{Value: &one, Set: true},
					MultipleOf:       &step,
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "must be > 0.0")
	require.Contains(t, s, "must be < 1.0")
	require.Contains(t, s, "math.Mod(float64(v.V), float64(0.25))")
	require.Contains(t, s, "must be a multiple of 0.25")
}

// TestGenerateTypes_UniqueItemsCheck: uniqueItems on an array property
// emits an O(n²) seen-map check; the field is rejected as soon as any
// duplicate appears.
func TestGenerateTypes_UniqueItemsCheck(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Bag": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"tags": {
					Type:        spec.SchemaType{"array"},
					UniqueItems: true,
					Items:       &spec.Schema{Type: spec.SchemaType{"string"}},
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "seen := make(map[any]struct{}, len(v.Tags))")
	require.Contains(t, s, "must have unique items")
}

// TestGenerateTypes_MapPropertiesSizeChecks: minProperties/maxProperties
// on a map-typed property emit len() comparisons.
func TestGenerateTypes_MapPropertiesSizeChecks(t *testing.T) {
	min, max := 1, 5
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Attrs": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"data": {
					Type:                 spec.SchemaType{"object"},
					AdditionalProperties: spec.AdditionalProperties{Schema: &spec.Schema{Type: spec.SchemaType{"string"}}, Set: true, Allowed: true},
					MinProperties:        &min,
					MaxProperties:        &max,
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "must have at least 1 entries")
	require.Contains(t, s, "must have at most 5 entries")
}

// TestGenerateTypes_AdditionalPropertiesBoolForms: yaml input can use
// `additionalProperties: true` or `additionalProperties: false`. The
// custom AdditionalProperties unmarshaler must accept both and the
// strict-mode flag must propagate into a DisallowUnknownFields call on
// the JSON decoder generated for a request body.
func TestGenerateTypes_AdditionalPropertiesBoolForms(t *testing.T) {
	// Boolean true: schema parses, no map alias is emitted, no strict mode.
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Open": {
			Type:                 spec.SchemaType{"object"},
			AdditionalProperties: spec.AdditionalProperties{Set: true, Allowed: true},
			Properties: map[string]*spec.Schema{
				"x": {Type: spec.SchemaType{"string"}},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	require.Contains(t, buf.String(), "type Open struct {")
}

// TestGenerateTypes_AdditionalPropertiesFalseStrictBody: a request body
// whose schema declares `additionalProperties: false` enables strict
// decoding on the server side (DisallowUnknownFields).
func TestGenerateTypes_AdditionalPropertiesFalseStrictBody(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{
				Required: true,
				Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{
						Type:                 spec.SchemaType{"object"},
						AdditionalProperties: spec.AdditionalProperties{Set: true, Allowed: false},
						Properties:           map[string]*spec.Schema{"n": {Type: spec.SchemaType{"string"}}},
					}},
				},
			},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	require.Contains(t, string(formatted), "dec.DisallowUnknownFields()")
}

// TestGenerateTypes_MapAliasForAdditionalOnlyObject: a top-level object
// schema that only declares additionalProperties (no Properties) must
// emit a map alias — emitting an empty struct silently drops every
// inbound JSON entry.
func TestGenerateTypes_MapAliasForAdditionalOnlyObject(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"M": {
			Type:                 spec.SchemaType{"object"},
			AdditionalProperties: spec.AdditionalProperties{Schema: &spec.Schema{Type: spec.SchemaType{"string"}}, Set: true, Allowed: true},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)
	require.Contains(t, s, "type M = map[string]string")
	require.NotContains(t, s, "type M struct {")
}

// TestGenerateTypes_PrimitiveAliasWithConstraintsHasValidate: a top-level
// primitive type with keyword constraints (pattern, minLength, …) must
// emit a NAMED type with a Validate method so references via $ref carry
// the constraints. Earlier the constraints silently vanished through the
// `type X = string` alias path.
func TestGenerateTypes_PrimitiveAliasWithConstraintsHasValidate(t *testing.T) {
	min := 2
	max := 20
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Tag": {
			Type: spec.SchemaType{"string"}, MinLength: &min, MaxLength: &max, Pattern: "^[a-z]+$",
		},
		"Tagged": {
			Type:       spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{"tag": {Ref: "#/components/schemas/Tag"}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// Named type (not `=` alias) so a method can attach.
	require.Contains(t, s, "type Tag string")
	require.NotContains(t, s, "type Tag = string")
	require.Contains(t, s, "func (v Tag) Validate() error")
	require.Contains(t, s, "patternTagSelf")
	require.Contains(t, s, "Tag value must be at least 2 characters")
	require.Contains(t, s, "Tag value must be at most 20 characters")
	require.Contains(t, s, "Tag value does not match required pattern")

	// Tagged.Validate must delegate to Tag.Validate.
	require.Contains(t, s, "func (v Tagged) Validate() error")
	require.Contains(t, s, "v.Tag.Validate()")
}

// TestGenerateTypes_DescriptionEmittedAcrossBranches: every shape yaggo
// emits at the top level (alias, $ref alias, enum, map alias, named
// primitive, struct, x-go-type) must carry the spec's description as a
// godoc comment. Earlier only the struct branch surfaced it.
func TestGenerateTypes_DescriptionEmittedAcrossBranches(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"UserID":   {Description: "Unique user id", Type: spec.SchemaType{"integer"}, Format: "int64"},
		"PetID":    {Description: "Pet id alias", Ref: "#/components/schemas/UserID"},
		"Username": {Description: "Constrained username", Type: spec.SchemaType{"string"}, MinLength: ptr(3), Pattern: "^[a-z]+$"},
		"Vendor":   {Description: "Custom Go type", XGoType: "time.Time", XGoImport: "time"},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "// UserID Unique user id")
	require.Contains(t, s, "// PetID Pet id alias")
	require.Contains(t, s, "// Username Constrained username")
	require.Contains(t, s, "// Vendor Custom Go type")
}

// ptr returns a pointer to v — small helper for test-data constants.
func ptr[T any](v T) *T { return &v }

// TestGenerateTypes_PatternVarNoCollision_ItemSibling: two pattern vars
// in one struct — one from a property named `tagsItem`, one from an
// array property `tags` with items.pattern — must NOT collide. Earlier
// the item suffix "Item" matched the property's toGoFieldName so both
// vars rendered as `patternUserTagsItem` (duplicate declaration).
func TestGenerateTypes_PatternVarNoCollision_ItemSibling(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"tagsItem": {Type: spec.SchemaType{"string"}, Pattern: "^A"},
				"tags": {
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"string"}, Pattern: "^B"},
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "pattern_User_TagsItem")
	require.Contains(t, s, "pattern_User_Tags_item")
}

// TestGenerateTypes_RequiredRefToStringEnforced: a struct field that
// references a top-level plain `type X = string` alias as required must
// still emit `if v.X == ""` zero rejection. Earlier the switch on the
// literal "string" missed the alias.
func TestGenerateTypes_RequiredRefToStringEnforced(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Name": {Type: spec.SchemaType{"string"}},
		"User": {
			Type:       spec.SchemaType{"object"},
			Required:   []string{"n"},
			Properties: map[string]*spec.Schema{"n": {Ref: "#/components/schemas/Name"}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "func (v User) Validate() error")
	require.Contains(t, s, `if v.N == ""`)
	require.Contains(t, s, "field 'n' is required")
}

// TestGenerateTypes_AliasChainDelegatesValidate: a struct field whose
// $ref reaches a constrained primitive through an intermediate alias
// must call .Validate() — earlier hasValidationVisited didn't follow
// top-level Ref, so the constraint chain was silently dropped.
func TestGenerateTypes_AliasChainDelegatesValidate(t *testing.T) {
	min := 2
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Tag":      {Type: spec.SchemaType{"string"}, MinLength: &min, Pattern: "^[a-z]+$"},
		"SuperTag": {Ref: "#/components/schemas/Tag"},
		"Holder": {
			Type:       spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{"s": {Ref: "#/components/schemas/SuperTag"}},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "func (v Holder) Validate() error")
	require.Contains(t, s, "v.S.Validate()")
}

// TestGenerateTypes_EnumBodyHandlerCallsValidate: a request body whose
// schema is a $ref to an enum component must trigger body.Validate() in
// the generated handler. Earlier hasValidation skipped enum-only refs.
func TestGenerateTypes_EnumBodyHandlerCallsValidate(t *testing.T) {
	api := &spec.OpenAPI{
		Components: spec.Components{Schemas: map[string]*spec.Schema{
			"Status": {Type: spec.SchemaType{"string"}, Enum: []any{"a", "b"}},
		}},
		Paths: map[string]spec.PathItem{
			"/x": {Post: &spec.Operation{
				OperationID: "doIt",
				RequestBody: &spec.RequestBody{Required: true, Content: map[string]spec.MediaType{
					"application/json": {Schema: &spec.Schema{Ref: "#/components/schemas/Status"}},
				}},
				Responses: map[string]spec.Response{"204": {}},
			}},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, GenerateServer(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	require.Contains(t, string(formatted), "body.Validate()")
}

// TestGenerateTypes_ValidatorUsesEnumConstSuffix: spec with enum entries
// that toGoName would mishandle (numeric, negative) but enumConstSuffix
// handles correctly must NOT be rejected up front. The earlier divergence
// blocked specs that the emitter would happily generate.
func TestGenerateTypes_ValidatorUsesEnumConstSuffix(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Code": {Type: spec.SchemaType{"integer"}, Enum: []any{-1, 0, 1, 100}},
	}}}
	require.NoError(t, GenerateTypes(&bytes.Buffer{}, api, "p"))
}

// TestGenerateTypes_NaNInfEnumRejected: NaN / ±Inf can't appear in a Go
// `const` initialiser (math.NaN() / math.Inf() aren't constants). The
// rejection mirrors JSON itself, which has no encoding for these values.
func TestGenerateTypes_NaNInfEnumRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Bad": {Type: spec.SchemaType{"number"}, Enum: []any{math.NaN(), 1.5}},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot be represented as a Go identifier suffix")
}

// TestGenerateTypes_CyclicAliasRejected: A → B → A would compile to
// `type A = B; type B = A` which Go rejects. Catch in validation.
func TestGenerateTypes_CyclicAliasRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"A": {Ref: "#/components/schemas/B"},
		"B": {Ref: "#/components/schemas/A"},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "cycle")
}

// TestGenerateTypes_EmptyEnumRejected: enum: [] semantically means "no
// value is valid". Almost never what spec author intended — fail loudly.
func TestGenerateTypes_EmptyEnumRejected(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Empty": {Type: spec.SchemaType{"string"}, Enum: []any{}},
	}}}
	err := GenerateTypes(&bytes.Buffer{}, api, "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty enum")
}

// TestGenerateTypes_EnumPlusPatternBothChecked: JSON Schema says enum
// AND pattern apply together (both must hold). The generated Validate
// must check the pattern as well as enum membership.
func TestGenerateTypes_EnumPlusPatternBothChecked(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Strict": {
			Type:    spec.SchemaType{"string"},
			Pattern: "^[a-z]+$",
			Enum:    []any{"foo", "bar"},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "patternStrictSelf.MatchString")
	require.Contains(t, s, "does not match required pattern")
	require.Contains(t, s, "case StrictFoo, StrictBar")
}

// TestGenerateTypes_InlineArrayBodyAlias: an inline non-object request
// body (e.g. JSON array) must emit a type alias rather than an empty
// struct — otherwise the decoder rejects the array shape entirely.
func TestGenerateTypes_InlineArrayBodyAlias(t *testing.T) {
	api := &spec.OpenAPI{Paths: map[string]spec.PathItem{
		"/x": {Post: &spec.Operation{
			OperationID: "doIt",
			RequestBody: &spec.RequestBody{Required: true, Content: map[string]spec.MediaType{
				"application/json": {Schema: &spec.Schema{
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"string"}},
				}},
			}},
			Responses: map[string]spec.Response{"204": {}},
		}},
	}}
	var buf bytes.Buffer
	require.NoError(t, GenerateBodyTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "type DoItBody = []string")
	require.NotContains(t, s, "type DoItBody struct {")
}

// TestGenerateTypes_ItemsPatternEmitsPerItemCheck: arrays whose items
// declare a pattern must produce per-element validation in the struct's
// Validate(). Earlier the items.pattern was silently dropped, so callers
// silently lost validation they had every reason to expect.
func TestGenerateTypes_ItemsPatternEmitsPerItemCheck(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"tags": {
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"string"}, Pattern: `^[a-z]+$`},
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	// Validate() must be emitted (items.pattern alone now triggers it).
	require.Contains(t, s, "func (v User) Validate() error")
	// Distinct pattern var, distinct from a field-level pattern.
	require.Contains(t, s, "pattern_User_Tags_item")
	require.Contains(t, s, `regexp.MustCompile("^[a-z]+$")`)
	// Per-element loop, indexed error message.
	require.Contains(t, s, "for i, item := range v.Tags")
	require.Contains(t, s, "pattern_User_Tags_item.MatchString(item)")
	require.Contains(t, s, "'tags'[%d] does not match required pattern")
}

// TestGenerateTypes_ItemsMinMaxLengthEmitsPerItemCheck: same gap closed for
// string-item length constraints.
func TestGenerateTypes_ItemsMinMaxLengthEmitsPerItemCheck(t *testing.T) {
	min, max := 2, 8
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"User": {
			Type: spec.SchemaType{"object"},
			Properties: map[string]*spec.Schema{
				"codes": {
					Type:  spec.SchemaType{"array"},
					Items: &spec.Schema{Type: spec.SchemaType{"string"}, MinLength: &min, MaxLength: &max},
				},
			},
		},
	}}}
	var buf bytes.Buffer
	require.NoError(t, GenerateTypes(&buf, api, "p"))
	formatted, err := format.Source(buf.Bytes())
	require.NoErrorf(t, err, "did not gofmt:\n%s", buf.String())
	s := string(formatted)

	require.Contains(t, s, "for i, item := range v.Codes")
	require.Contains(t, s, "utf8.RuneCountInString(item)")
	require.Contains(t, s, "'codes'[%d] must be at least 2 characters")
	require.Contains(t, s, "'codes'[%d] must be at most 8 characters")
}

func TestGenerateTypesPropagatesWriterError(t *testing.T) {
	api := &spec.OpenAPI{Components: spec.Components{Schemas: map[string]*spec.Schema{
		"Pet": {Type: spec.SchemaType{"object"}, Properties: map[string]*spec.Schema{"name": {Type: spec.SchemaType{"string"}}}},
	}}}
	err := GenerateTypes(&failingWriter{err: errors.New("io fail")}, api, "p")
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "io fail"))
}
