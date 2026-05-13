package gen

import (
	"embed"
	"text/template"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("").ParseFS(templateFS, "templates/*.tmpl"))
