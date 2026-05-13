package spec

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parse reads and validates an OpenAPI 3.x YAML file at filename.
func Parse(filename string) (*OpenAPI, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}
	var api OpenAPI
	if err := yaml.Unmarshal(data, &api); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if err := validate(&api); err != nil {
		return nil, err
	}
	return &api, nil
}

func validate(api *OpenAPI) error {
	if !strings.HasPrefix(api.OpenAPI, "3.") {
		return fmt.Errorf("unsupported OpenAPI version %q, only 3.x is supported", api.OpenAPI)
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
		return nil, "", fmt.Errorf("$ref %q not found in components/schemas", ref)
	}
	return s, name, nil
}

// RefName extracts the schema name from a $ref string.
func RefName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}
