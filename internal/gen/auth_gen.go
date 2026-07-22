package gen

import (
	"fmt"
	"io"

	"github.com/123456890987654321/yaggo/internal/spec"
)

// authData drives the auth.go template.
type authData struct {
	Package string
	Schemes []authScheme
}

// authScheme is one OpenAPI security scheme rendered into form the template
// can consume without inspecting OpenAPI semantics directly.
type authScheme struct {
	GoName       string // exported function suffix, e.g. "BearerAuth"
	OriginalName string // spec key, used in doc comments
	Kind         string // "bearer" | "basic" | "apiKeyHeader" | "apiKeyQuery" | "unsupported"
	KeyName      string // header/query parameter name for apiKey schemes
	Description  string // single-line, comment-safe
	BearerFormat string // single-line, comment-safe; informational for HTTP bearer
	Reason       string // populated when Kind == "unsupported", explains the skip
}

// GenerateAuth writes the authentication helpers file. The generic helpers
// (BasicAuth, BearerToken, APIKey, …) are emitted unconditionally; when the
// spec defines components.securitySchemes, one typed wrapper per scheme is
// also emitted so call sites match spec naming.
func GenerateAuth(w io.Writer, api *spec.OpenAPI, pkg string) error {
	if err := validateSpecIdentifiers(api); err != nil {
		return err
	}
	data := authData{Package: pkg}
	if api != nil {
		for _, name := range sortedKeys(api.Components.SecuritySchemes) {
			data.Schemes = append(data.Schemes, buildAuthScheme(name, api.Components.SecuritySchemes[name]))
		}
	}
	return templates.ExecuteTemplate(w, "auth.go.tmpl", data)
}

func buildAuthScheme(name string, s *spec.SecurityScheme) authScheme {
	out := authScheme{
		GoName:       toGoName(name),
		OriginalName: name,
		Description:  sanitizeComment(s.Description),
		BearerFormat: sanitizeComment(s.BearerFormat),
	}
	switch s.Type {
	case "http":
		switch s.Scheme {
		case "bearer":
			out.Kind = "bearer"
		case "basic":
			out.Kind = "basic"
		default:
			out.Kind = "unsupported"
			out.Reason = fmt.Sprintf("http scheme %q not supported (only bearer and basic)", s.Scheme)
		}
	case "apiKey":
		switch s.In {
		case "header":
			out.Kind = "apiKeyHeader"
			out.KeyName = s.Name
		case "query":
			out.Kind = "apiKeyQuery"
			out.KeyName = s.Name
		default:
			out.Kind = "unsupported"
			out.Reason = fmt.Sprintf("apiKey in %q not supported (only header and query)", s.In)
		}
	case "oauth2":
		out.Kind = "unsupported"
		out.Reason = "oauth2 requires a custom RequestEditor that performs the flow and supplies a bearer token"
	case "openIdConnect":
		out.Kind = "unsupported"
		// OpenIDConnectURL is interpolated into a Go comment via {{.Reason}};
		// sanitizeComment strips newlines that would otherwise let a crafted
		// URL escape the comment and inject arbitrary lines into auth.go.
		out.Reason = "openIdConnect requires a custom RequestEditor; discover endpoints from " + sanitizeComment(s.OpenIDConnectURL)
	case "mutualTLS":
		out.Kind = "unsupported"
		out.Reason = "mutualTLS is configured at the TLS layer (http.Transport / tls.Config), not via RequestEditor"
	default:
		out.Kind = "unsupported"
		out.Reason = fmt.Sprintf("unknown security scheme type %q", s.Type)
	}
	return out
}
