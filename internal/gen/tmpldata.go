package gen

import (
	"fmt"
	"strings"

	"github.com/123456890987654321/yago/internal/spec"
)

// tmplData is the root data structure handed to server/client templates.
type tmplData struct {
	Package    string
	Operations []tmplOp
}

// tmplOp is one HTTP operation, precomputed so templates stay simple.
type tmplOp struct {
	Name            string
	Method          string
	Path            string
	Summary         string
	PathParams      []tmplParam
	QueryParams     []tmplParam
	HasQuery        bool
	HasBody         bool
	BodyType        string
	HasBodyValidate bool
	HasReturn       bool
	ReturnType      string
	PathExpr        string // Go expression that yields the request path with params substituted.
}

// tmplParam describes one path or query parameter for templates.
type tmplParam struct {
	GoName            string // lower-camel variable name (e.g. petId)
	GoField           string // PascalCase struct field name (e.g. PetId)
	JSONName          string // original spec name
	GoType            string // resolved Go type (e.g. "int64", "PetStatus", "string")
	GoTypeOptional    string // GoType, possibly prefixed with "*" when not required
	Required          bool
	Kind              string // underlying scalar: string, int, int32, int64, float32, float64, bool
	Bits              string // bits for ParseInt/ParseFloat ("32"/"64"); empty for non-numeric
	IsNamed           bool   // true when GoType is a named type aliased to Kind (e.g. PetStatus -> string)
	QuerySetExpr      string // expression turning the param value into a string for q.Set
	QuerySetExprDeref string // same, but for a dereferenced (*p) optional value
}

func buildTmplData(api *spec.OpenAPI, pkg string) tmplData {
	data := tmplData{Package: pkg}
	for _, mo := range collectOps(api) {
		data.Operations = append(data.Operations, buildOp(mo, api))
	}
	return data
}

func buildOp(mo MethodOp, api *spec.OpenAPI) tmplOp {
	name := operationName(mo.Method, mo.Path, mo.Op)
	op := tmplOp{
		Name:    name,
		Method:  mo.Method,
		Path:    mo.Path,
		Summary: mo.Op.Summary,
	}
	for _, pp := range pathParams(mo.Op) {
		op.PathParams = append(op.PathParams, buildParam(pp, api))
	}
	if qps := queryParams(mo.Op); len(qps) > 0 {
		op.HasQuery = true
		for _, qp := range qps {
			op.QueryParams = append(op.QueryParams, buildParam(qp, api))
		}
	}
	if bs := requestBodySchema(mo.Op); bs != nil {
		op.HasBody = true
		op.BodyType = bodyTypeName(name, mo, api)
		op.HasBodyValidate = bodyHasValidate(bs, api)
	}
	if rs := successResponseSchema(mo.Op); rs != nil {
		op.HasReturn = true
		op.ReturnType = schemaToGoType(rs, false)
	}
	op.PathExpr = buildPathExpr(mo.Path, mo.Op)
	return op
}

func buildParam(p spec.Parameter, api *spec.OpenAPI) tmplParam {
	goType := "string"
	if p.Schema != nil {
		goType = schemaToGoType(p.Schema, false)
	}
	goTypeOpt := goType
	if !p.Required {
		goTypeOpt = "*" + goType
	}
	kind, bits := paramKind(p.Schema, api)
	param := tmplParam{
		GoName:         lowerFirst(toGoName(p.Name)),
		GoField:        toGoFieldName(p.Name),
		JSONName:       p.Name,
		GoType:         goType,
		GoTypeOptional: goTypeOpt,
		Required:       p.Required,
		Kind:           kind,
		Bits:           bits,
		IsNamed:        goType != kind,
	}
	param.QuerySetExpr = querySetExpr(param.GoName, param.Kind, param.IsNamed, param.GoType)
	param.QuerySetExprDeref = querySetExpr("*"+param.GoName, param.Kind, param.IsNamed, param.GoType)
	return param
}

// paramKind walks $refs to find the underlying scalar kind of a parameter schema.
func paramKind(s *spec.Schema, api *spec.OpenAPI) (kind, bits string) {
	if s == nil {
		return "string", ""
	}
	if s.Ref != "" {
		resolved, _, err := spec.ResolveSchema(s.Ref, api)
		if err != nil || resolved == nil {
			return "string", ""
		}
		return paramKind(resolved, api)
	}
	switch s.Type {
	case "integer":
		switch s.Format {
		case "int32":
			return "int32", "32"
		case "int64":
			return "int64", "64"
		default:
			return "int", "64"
		}
	case "number":
		if s.Format == "float" {
			return "float32", "32"
		}
		return "float64", "64"
	case "boolean":
		return "bool", ""
	default:
		return "string", ""
	}
}

// querySetExpr returns the Go expression that converts the param value to a string for q.Set.
// For named scalar types it inserts the necessary cast (e.g. string(*status), int64(limit)).
func querySetExpr(varExpr, kind string, isNamed bool, goType string) string {
	switch kind {
	case "int", "int32", "int64":
		return fmt.Sprintf("strconv.FormatInt(int64(%s), 10)", varExpr)
	case "float32", "float64":
		return fmt.Sprintf("strconv.FormatFloat(float64(%s), 'f', -1, 64)", varExpr)
	case "bool":
		if isNamed {
			return fmt.Sprintf("strconv.FormatBool(bool(%s))", varExpr)
		}
		return fmt.Sprintf("strconv.FormatBool(%s)", varExpr)
	default: // string
		if isNamed {
			return fmt.Sprintf("string(%s)", varExpr)
		}
		return varExpr
	}
}

// bodyHasValidate reports whether the body type for an operation has a generated Validate() method.
func bodyHasValidate(bs *spec.Schema, api *spec.OpenAPI) bool {
	if bs == nil {
		return false
	}
	if bs.Ref != "" {
		resolved, _, err := spec.ResolveSchema(bs.Ref, api)
		if err != nil {
			return false
		}
		return hasValidation(effectiveSchema(resolved, api), api)
	}
	return hasValidation(effectiveSchema(bs, api), api)
}

// buildPathExpr returns a Go expression that produces the URL path with parameters substituted.
func buildPathExpr(path string, op *spec.Operation) string {
	pps := pathParams(op)
	if len(pps) == 0 {
		return fmt.Sprintf("%q", path)
	}
	fmtPath := path
	var args []string
	for _, pp := range pps {
		goVar := lowerFirst(toGoName(pp.Name))
		verb := "%s"
		if pp.Schema != nil {
			goType := schemaToGoType(pp.Schema, false)
			if goType == "int" || goType == "int32" || goType == "int64" {
				verb = "%d"
			}
		}
		fmtPath = strings.ReplaceAll(fmtPath, "{"+pp.Name+"}", verb)
		args = append(args, goVar)
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", fmtPath, strings.Join(args, ", "))
}
