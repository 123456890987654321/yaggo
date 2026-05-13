package gen

import (
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/123456890987654321/yago/internal/spec"
)

// toGoName converts any identifier (snake_case, kebab-case, camelCase) to PascalCase.
func toGoName(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	result := b.String()
	if result == "" {
		return s
	}
	// Ensure first rune is uppercase (handles camelCase input like "petId")
	runes := []rune(result)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// toGoFieldName returns an exported struct field name.
func toGoFieldName(name string) string {
	return toGoName(name)
}

// operationName returns a PascalCase function name for an operation.
func operationName(method, path string, op *spec.Operation) string {
	if op.OperationID != "" {
		return toGoName(op.OperationID)
	}
	method = strings.ToLower(method)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var out []string
	switch method {
	case "get":
		last := parts[len(parts)-1]
		if strings.HasPrefix(last, "{") {
			out = append(out, "Get")
		} else {
			out = append(out, "List")
		}
	case "post":
		out = append(out, "Create")
	case "put", "patch":
		out = append(out, "Update")
	case "delete":
		out = append(out, "Delete")
	default:
		out = append(out, toGoName(method))
	}
	for _, p := range parts {
		if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
			out = append(out, "By"+toGoName(p[1:len(p)-1]))
		} else {
			out = append(out, toGoName(p))
		}
	}
	return strings.Join(out, "")
}

// schemaToGoType returns the Go type string for an OpenAPI schema.
// optional=true wraps scalar types in a pointer.
func schemaToGoType(s *spec.Schema, optional bool) string {
	if s == nil {
		return "any"
	}
	if s.Ref != "" {
		t := toGoName(spec.RefName(s.Ref))
		if optional {
			return "*" + t
		}
		return t
	}
	switch s.Type {
	case "string":
		if optional {
			return "*string"
		}
		return "string"
	case "integer":
		var t string
		switch s.Format {
		case "int32":
			t = "int32"
		case "int64":
			t = "int64"
		default:
			t = "int"
		}
		if optional {
			return "*" + t
		}
		return t
	case "number":
		var t string
		if s.Format == "float" {
			t = "float32"
		} else {
			t = "float64"
		}
		if optional {
			return "*" + t
		}
		return t
	case "boolean":
		if optional {
			return "*bool"
		}
		return "bool"
	case "array":
		inner := "any"
		if s.Items != nil {
			inner = schemaToGoType(s.Items, false)
		}
		return "[]" + inner
	case "object":
		if s.AdditionalProperties != nil {
			return "map[string]" + schemaToGoType(s.AdditionalProperties, false)
		}
		return "map[string]any"
	default:
		return "any"
	}
}

// sortedKeys returns map keys in sorted order.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// httpMethods lists the HTTP methods in PathItem order.
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// MethodOp pairs an HTTP method and URL path with its resolved Operation.
// It is the unit of iteration used by the code generators.
type MethodOp struct {
	Method string
	Path   string
	Op     *spec.Operation
}

// collectOps returns all operations from the spec in a stable order.
func collectOps(api *spec.OpenAPI) []MethodOp {
	paths := sortedKeys(api.Paths)
	var ops []MethodOp
	for _, path := range paths {
		item := api.Paths[path]
		for _, m := range httpMethods {
			op := pathItemOp(&item, m)
			if op != nil {
				ops = append(ops, MethodOp{Method: m, Path: path, Op: op})
			}
		}
	}
	return ops
}

func pathItemOp(item *spec.PathItem, method string) *spec.Operation {
	switch method {
	case "GET":
		return item.Get
	case "POST":
		return item.Post
	case "PUT":
		return item.Put
	case "PATCH":
		return item.Patch
	case "DELETE":
		return item.Delete
	}
	return nil
}

// pathParams returns only the path parameters for an operation.
func pathParams(op *spec.Operation) []spec.Parameter {
	var out []spec.Parameter
	for _, p := range op.Parameters {
		if p.In == "path" {
			out = append(out, p)
		}
	}
	return out
}

// queryParams returns only the query parameters for an operation.
func queryParams(op *spec.Operation) []spec.Parameter {
	var out []spec.Parameter
	for _, p := range op.Parameters {
		if p.In == "query" {
			out = append(out, p)
		}
	}
	return out
}

// requestBodySchema extracts the JSON schema from a request body, if any.
func requestBodySchema(op *spec.Operation) *spec.Schema {
	if op.RequestBody == nil {
		return nil
	}
	if mt, ok := op.RequestBody.Content["application/json"]; ok {
		return mt.Schema
	}
	return nil
}

// successResponseSchema extracts the first 2xx response JSON schema, if any.
func successResponseSchema(op *spec.Operation) *spec.Schema {
	for _, code := range []string{"200", "201", "202"} {
		if resp, ok := op.Responses[code]; ok {
			if mt, ok := resp.Content["application/json"]; ok {
				return mt.Schema
			}
		}
	}
	return nil
}

// mergeAllOf merges allOf schemas into a single flat schema (for code gen purposes).
func mergeAllOf(schemas []*spec.Schema, api *spec.OpenAPI) *spec.Schema {
	merged := &spec.Schema{
		Type:       "object",
		Properties: make(map[string]*spec.Schema),
	}
	for _, s := range schemas {
		src := s
		if s.Ref != "" {
			resolved, _, err := spec.ResolveSchema(s.Ref, api)
			if err != nil {
				continue
			}
			src = resolved
		}
		maps.Copy(merged.Properties, src.Properties)
		merged.Required = append(merged.Required, src.Required...)
	}
	return merged
}
