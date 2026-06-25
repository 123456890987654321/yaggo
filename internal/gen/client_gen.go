package gen

import (
	"io"

	"github.com/123456890987654321/yaggo/internal/spec"
)

// GenerateClient writes the HTTP client file.
func GenerateClient(w io.Writer, api *spec.OpenAPI, pkg string) error {
	if err := validateSpecIdentifiers(api); err != nil {
		return err
	}
	if err := validateSpecPatterns(api); err != nil {
		return err
	}
	return templates.ExecuteTemplate(w, "client.go.tmpl", buildTmplData(api, pkg))
}
