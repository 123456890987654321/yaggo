package gen

import (
	"fmt"
	"strings"

	"github.com/123456890987654321/yaggo/internal/spec"
)

// tmplData is the root data structure handed to server/client templates.
type tmplData struct {
	Package        string
	Operations     []tmplOp
	UsesStrconvSrv bool // server: any non-string path/query/header param triggers strconv.ParseX
	UsesStrconvCli bool // client: ONLY non-string query/header params trigger strconv.FormatX (path uses fmt.Sprintf directly)
	UsesFmtSrv     bool // true if the server template needs fmt (body decode errors, path/query parse errors)
	UsesErrors     bool // true if the server template needs errors (body-size 413 path via errors.As)
	UsesIOSrv      bool // true if the server template needs io (optional-body io.EOF short-circuit)
	UsesStringsSrv bool // true if the server template needs strings (Content-Type media-type comparison)
}

// tmplOp is one HTTP operation, precomputed so templates stay simple.
type tmplOp struct {
	Name               string
	Method             string
	Path               string
	Summary            string
	PathParams         []tmplParam
	QueryParams        []tmplParam
	HeaderParams       []tmplParam
	HasQuery           bool
	HasHeader          bool
	HasBody            bool
	BodyRequired       bool // mirrors requestBody.required; false → server accepts empty body without 400
	BodyStrict         bool // mirrors body schema's additionalProperties:false; true → server uses DisallowUnknownFields
	BodyType           string
	HasBodyValidate    bool
	RequestContentType string // media type of the request body (e.g. "application/json", "application/vnd.api+json")
	HasReturn          bool
	ReturnType         string
	ResponseAccept     string // value for the Accept request header; falls back to "application/json" when no 2xx body is declared, so error responses remain decodable
	PathExpr           string // Go expression that yields the request path with params substituted.
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

	// Array-typed query parameters (OpenAPI default: style=form, explode=true).
	// When IsArray is true:
	//   - GoType/GoTypeOptional are slice types ([]int64, []string, …) OR a
	//     named alias to a slice ($ref to a top-level array schema)
	//   - Kind/Bits describe the ELEMENT scalar
	//   - the templates emit per-element q.Add / parse loops
	//   - ItemSetExpr is the expression that stringifies one element ("v")
	//   - ElemGoType is the explicit Go type to cast each element to; it
	//     matches the items' resolved type (e.g. "int64", "PetStatus") and
	//     replaces an earlier "strip leading []" template hack that broke
	//     for ref-typed array params (GoType = alias, not "[]X").
	IsArray     bool
	ItemSetExpr string // expression stringifying a single element variable named "v"
	ElemGoType  string // explicit element Go type for array params
}

func buildTmplData(api *spec.OpenAPI, pkg string) tmplData {
	data := tmplData{Package: pkg}
	for _, mo := range collectOps(api) {
		op := buildOp(mo, api)
		data.Operations = append(data.Operations, op)
		for _, p := range op.PathParams {
			if p.Kind != "string" {
				// Server side parses path params via strconv.ParseInt/etc.
				// Client side formats path params via fmt.Sprintf("%d", v),
				// so non-string path params do NOT pull strconv into the
				// client output.
				data.UsesStrconvSrv = true
				data.UsesFmtSrv = true
			}
		}
		for _, p := range op.QueryParams {
			if p.Required {
				// Required query params surface missing/parse errors via fmt.Errorf.
				data.UsesFmtSrv = true
			}
			if p.Kind != "string" {
				data.UsesStrconvSrv = true
				data.UsesStrconvCli = true
				data.UsesFmtSrv = true
			}
		}
		for _, p := range op.HeaderParams {
			// Headers always go through fmt for missing-required errors,
			// and through strconv whenever the value isn't a string.
			data.UsesFmtSrv = true
			if p.Kind != "string" {
				data.UsesStrconvSrv = true
				data.UsesStrconvCli = true
			}
		}
		if op.HasBody {
			data.UsesFmtSrv = true
			data.UsesErrors = true
			// Content-Type validation strips ";charset=…" suffixes via the
			// strings package and EqualFolds against the spec literal.
			data.UsesStringsSrv = true
			if !op.BodyRequired {
				// The optional-body shortcut compares err to io.EOF.
				data.UsesIOSrv = true
			}
		}
	}
	return data
}

func buildOp(mo MethodOp, api *spec.OpenAPI) tmplOp {
	name := operationName(mo.Method, mo.Path, mo.Op)
	op := tmplOp{
		Name:    name,
		Method:  mo.Method,
		Path:    mo.Path,
		Summary: sanitizeComment(mo.Op.Summary),
	}
	for _, pp := range paramsByLocation(mo.Op, paramInPath) {
		op.PathParams = append(op.PathParams, buildParam(pp, api))
	}
	if qps := paramsByLocation(mo.Op, paramInQuery); len(qps) > 0 {
		op.HasQuery = true
		for _, qp := range qps {
			op.QueryParams = append(op.QueryParams, buildParam(qp, api))
		}
	}
	if hps := paramsByLocation(mo.Op, paramInHeader); len(hps) > 0 {
		op.HasHeader = true
		for _, hp := range hps {
			op.HeaderParams = append(op.HeaderParams, buildParam(hp, api))
		}
	}
	if ct, bs := requestBodyContent(mo.Op); bs != nil {
		op.HasBody = true
		op.BodyRequired = mo.Op.RequestBody.Required
		op.RequestContentType = ct
		op.BodyType = bodyTypeName(name, mo, api)
		op.HasBodyValidate = bodyHasValidate(bs, api)
		// Strict mode: spec author wrote `additionalProperties: false` on
		// the body schema. Honour it by enabling DisallowUnknownFields on
		// the JSON decoder so unknown JSON keys produce a 400.
		bodyEff := bs
		if bs.Ref != "" {
			if resolved, _, err := spec.ResolveSchema(bs.Ref, api); err == nil {
				bodyEff = resolved
			}
		}
		op.BodyStrict = effectiveSchema(bodyEff, api).AdditionalPropertiesForbidden()
	}
	op.ResponseAccept = "application/json" // default keeps error-body JSON decoding working
	if ct, rs := successResponseContent(mo.Op); rs != nil {
		op.HasReturn = true
		op.ResponseAccept = ct
		op.ReturnType = schemaToGoType(rs, false)
	}
	op.PathExpr = buildPathExpr(mo.Path, mo.Op)
	return op
}

func buildParam(p spec.Parameter, api *spec.OpenAPI) tmplParam {
	// paramGoType (not schemaToGoType) on purpose: for path/query/header
	// parameters OpenAPI's "nullable" has no meaningful wire form, so we
	// ignore it. Required → value type, optional → single pointer.
	goType := paramGoType(p.Schema)
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
	// Array-typed parameters: rebuild the typed metadata so Kind/Bits describe
	// the element scalar and the slice itself is the GoType. The unwrapping is
	// shallow on purpose — nested arrays of arrays are unusual in OpenAPI and
	// we fall back to "string" with the original slice shape.
	//
	// Resolve $ref before the array check: a parameter whose schema is just
	// "$ref: '#/components/schemas/ArrAlias'" would have empty Type — we'd
	// otherwise treat it as a scalar and emit broken slice<->string casts.
	arrSchema := resolveSchemaForKind(p.Schema, api)
	if arrSchema != nil && arrSchema.Type.Primary() == "array" && arrSchema.Items != nil {
		// Nested array of arrays: there's no canonical 1-D wire encoding
		// for them in URL form, and a per-element string cast can't round-
		// trip a slice. Collapse to []any so the generated code compiles;
		// the user already got a warning from warnNestedArrayParams.
		itemsResolved := resolveSchemaForKind(arrSchema.Items, api)
		if itemsResolved != nil && itemsResolved.Type.Primary() == "array" {
			param.IsArray = true
			param.GoType = "[]any"
			param.GoTypeOptional = "[]any"
			param.Kind = "string"
			param.Bits = ""
			param.IsNamed = false
			param.ElemGoType = "any"
			// fmt.Sprint is the only universally-applicable any→string for
			// the client; the server side just dumps the raw string into
			// the slice (the spec asked for the impossible).
			param.ItemSetExpr = "fmt.Sprint(v)"
			return param
		}
		param.IsArray = true
		// Optional arrays don't need pointer-wrapping in Go — a nil slice is
		// already a clear "absent" signal.
		param.GoTypeOptional = goType
		elemKind, elemBits := paramKind(arrSchema.Items, api)
		param.Kind = elemKind
		param.Bits = elemBits
		// IsNamed reflects whether the element is an alias type (e.g. PetStatus).
		// Use paramGoType (never schemaToGoType) so a nullable item doesn't
		// produce a pointer cast like *string(v).
		elemGoType := paramGoType(arrSchema.Items)
		param.IsNamed = elemGoType != elemKind
		param.ElemGoType = elemGoType
		// Items are stringified one at a time with a loop variable named "v".
		param.ItemSetExpr = querySetExpr("v", elemKind, param.IsNamed, elemGoType)
	} else {
		param.QuerySetExpr = querySetExpr(param.GoName, param.Kind, param.IsNamed, param.GoType)
		param.QuerySetExprDeref = querySetExpr("*"+param.GoName, param.Kind, param.IsNamed, param.GoType)
	}
	return param
}

// resolveSchemaForKind follows a single $ref hop so a parameter whose schema
// is "{$ref: ...}" still surfaces the underlying type (array, enum-string,
// scalar). Returns the input untouched for non-ref schemas, and nil for
// unresolvable refs — callers fall back to the scalar path in either case.
func resolveSchemaForKind(s *spec.Schema, api *spec.OpenAPI) *spec.Schema {
	if s == nil {
		return nil
	}
	if s.Ref == "" {
		return s
	}
	resolved, _, err := spec.ResolveSchema(s.Ref, api)
	if err != nil || resolved == nil {
		return nil
	}
	return resolved
}

// paramKind walks $refs to find the underlying scalar kind of a parameter schema.
func paramKind(s *spec.Schema, api *spec.OpenAPI) (kind, bits string) {
	return paramKindVisited(s, api, map[string]bool{})
}

// paramKindVisited is the cycle-safe core. A ref chain that revisits a schema
// it already followed terminates with the "string" fallback rather than
// recursing forever — circular ref chains are unusual but legal, and the
// fallback keeps generation from blowing the stack.
func paramKindVisited(s *spec.Schema, api *spec.OpenAPI, visited map[string]bool) (kind, bits string) {
	if s == nil {
		return "string", ""
	}
	if s.Ref != "" {
		if visited[s.Ref] {
			return "string", ""
		}
		visited[s.Ref] = true
		resolved, _, err := spec.ResolveSchema(s.Ref, api)
		if err != nil || resolved == nil {
			return "string", ""
		}
		return paramKindVisited(resolved, api, visited)
	}
	switch s.Type.Primary() {
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
//
// String-typed path arguments are wrapped in url.PathEscape so that values
// containing "/", "?", "#", or other reserved characters cannot corrupt the
// request URL (and, in extreme cases, redirect the request to a different
// path). Numeric and boolean params don't need escaping because their string
// form (%d, %t) is already URL-safe.
//
// When the same {placeholder} appears more than once in a path, the arg
// expression is repeated for each verb — strings.ReplaceAll already replaces
// every occurrence, so the verb count must match the arg count to avoid a
// runtime %!d(MISSING) in the URL.
func buildPathExpr(path string, op *spec.Operation) string {
	pps := paramsByLocation(op, paramInPath)
	if len(pps) == 0 {
		return fmt.Sprintf("%q", path)
	}
	fmtPath := path
	var args []string
	for _, pp := range pps {
		goVar := lowerFirst(toGoName(pp.Name))
		verb := "%s"
		needsEscape := true
		argExpr := goVar
		if pp.Schema != nil {
			// paramGoType: ignore nullable on path params, same reasoning as
			// in buildParam. The handler signature uses the value type, not a
			// pointer, so the verb selection here must match.
			goType := paramGoType(pp.Schema)
			switch goType {
			case "int", "int32", "int64":
				verb = "%d"
				needsEscape = false
			case "float32", "float64":
				// %v handles both float types without trailing zeros.
				verb = "%v"
				needsEscape = false
			case "bool":
				verb = "%t"
				needsEscape = false
			default:
				// string or named-string scalar. A named scalar (e.g. PetStatus)
				// needs an explicit string() cast before url.PathEscape sees it.
				if goType != "string" {
					argExpr = "string(" + goVar + ")"
				}
			}
		}
		if needsEscape {
			argExpr = "url.PathEscape(" + argExpr + ")"
		}
		// Count how many times the placeholder occurs; each occurrence gets
		// its own copy of the arg expression so fmt.Sprintf has matching
		// verb/arg counts.
		token := "{" + pp.Name + "}"
		occurrences := strings.Count(fmtPath, token)
		fmtPath = strings.ReplaceAll(fmtPath, token, verb)
		for i := 0; i < occurrences; i++ {
			args = append(args, argExpr)
		}
	}
	return fmt.Sprintf("fmt.Sprintf(%q, %s)", fmtPath, strings.Join(args, ", "))
}
