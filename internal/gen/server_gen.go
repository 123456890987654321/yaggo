package gen

import (
	"io"
	"strings"

	"github.com/123456890987654321/yaggo/internal/spec"
)

// GenerateServer writes the server interface + chi handler registration file.
func GenerateServer(w io.Writer, api *spec.OpenAPI, pkg string) error {
	if err := validateSpecIdentifiers(api); err != nil {
		return err
	}
	if err := validateSpecPatterns(api); err != nil {
		return err
	}
	return templates.ExecuteTemplate(w, "server.go.tmpl", buildTmplData(api, pkg))
}

// bodyTypeName returns the Go type name for a request body. When the body is a $ref
// to a named component schema, that name is used; otherwise an inline "<OpName>Body"
// type is emitted by GenerateBodyTypes.
func bodyTypeName(opName string, mo MethodOp, _ *spec.OpenAPI) string {
	_, bs := requestBodyContent(mo.Op)
	if bs == nil {
		return "any"
	}
	if bs.Ref != "" {
		return toGoName(spec.RefName(bs.Ref))
	}
	return opName + "Body"
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}
