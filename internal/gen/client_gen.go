package gen

import (
	"io"

	"github.com/123456890987654321/yago/internal/spec"
)

// GenerateClient writes the HTTP client file.
func GenerateClient(w io.Writer, api *spec.OpenAPI, pkg string) error {
	return templates.ExecuteTemplate(w, "client.go.tmpl", buildTmplData(api, pkg))
}
