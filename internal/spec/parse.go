package spec

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParseOption configures Parse. Compose multiple options at the call site;
// the zero set of options is the lenient default that mirrors the OpenAPI
// spec's "unknown fields are silently ignored" semantics.
type ParseOption func(*parseConfig)

// parseConfig is the resolved Parse configuration after all options apply.
type parseConfig struct {
	strict bool
}

// WithStrict makes the YAML decoder reject keys that don't match a struct
// field. Useful in CI to catch misspelled OpenAPI keywords (e.g. "responsess"
// instead of "responses") that would otherwise turn into silent no-ops.
func WithStrict() ParseOption {
	return func(c *parseConfig) { c.strict = true }
}

// Parse reads and validates an OpenAPI 3.x YAML file at filename.
//
// Errors are wrapped with the source filename so log lines remain
// interpretable when many specs are processed in one pipeline.
func Parse(filename string, opts ...ParseOption) (*OpenAPI, error) {
	var cfg parseConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	data, err := os.ReadFile(filename) // #nosec G304 -- CLI tool reads a user-supplied path by design
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filename, err)
	}
	var api OpenAPI
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(cfg.strict)
	if err := dec.Decode(&api); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}
	if err := validate(&api); err != nil {
		return nil, fmt.Errorf("validating %s: %w", filename, err)
	}
	return &api, nil
}

// supportedMajor is the OpenAPI major version family this generator targets.
// Both 3.0.x and 3.1.x are accepted; the parser handles the type-array and
// nullable-via-type-null differences between them.
const supportedMajor = "3."

func validate(api *OpenAPI) error {
	if !strings.HasPrefix(api.OpenAPI, supportedMajor) {
		return fmt.Errorf("unsupported OpenAPI version %q, only 3.x is supported (3.0.x and 3.1.x tested)", api.OpenAPI)
	}
	return nil
}

// ResolveSchema follows a $ref and returns the referenced schema.
func ResolveSchema(ref string, api *OpenAPI) (*Schema, string, error) {
	const prefix = "#/components/schemas/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, "", fmt.Errorf("unsupported $ref %q (only local component refs supported)", ref)
	}
	name := ref[len(prefix):]
	s, ok := api.Components.Schemas[name]
	if !ok {
		return nil, "", fmt.Errorf("$ref %q not found in components/schemas (have: %s)", ref, strings.Join(knownSchemaNames(api), ", "))
	}
	return s, name, nil
}

// knownSchemaNames lists the names defined under components/schemas, sorted for
// stable error messages. Empty slice → "none".
func knownSchemaNames(api *OpenAPI) []string {
	if len(api.Components.Schemas) == 0 {
		return []string{"none"}
	}
	names := make([]string, 0, len(api.Components.Schemas))
	for n := range api.Components.Schemas {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RefName extracts the schema name from a $ref string.
func RefName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

// ResolveParameter follows a $ref into components/parameters and returns the
// referenced Parameter. Only local component refs are supported (consistent
// with ResolveSchema). The returned pointer is the same one stored in
// Components.Parameters; callers must not mutate it.
func ResolveParameter(ref string, api *OpenAPI) (*Parameter, string, error) {
	const prefix = "#/components/parameters/"
	if !strings.HasPrefix(ref, prefix) {
		return nil, "", fmt.Errorf("unsupported parameter $ref %q (only local component refs supported)", ref)
	}
	name := ref[len(prefix):]
	p, ok := api.Components.Parameters[name]
	if !ok {
		return nil, "", fmt.Errorf("parameter $ref %q not found in components/parameters", ref)
	}
	return p, name, nil
}
