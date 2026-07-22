package gen

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/123456890987654321/yaggo/internal/spec"
)

// goIdent matches a valid Go identifier (per the language spec: letter,
// followed by letters or digits, where "letter" is restricted here to ASCII
// — broader Unicode identifiers are legal in Go but would mask security
// issues like RTL overrides, homoglyphs, and zero-width joiners).
var goIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateGoIdent returns a descriptive error when name would, if interpolated
// raw into generated source as an identifier, produce broken or attacker-shaped
// Go. The check is intentionally stricter than what the language allows so we
// fail loudly at generation time rather than relying on go/format to reject
// downstream — the unformatted file is written to disk for debugging, which
// would otherwise leave a foothold for a malicious spec to land arbitrary
// bytes in the developer's workspace.
//
// origin is included in the error so a misbehaving spec value is easy to
// locate (e.g. "schema 'X; init()...'").
func validateGoIdent(origin, name string) error {
	if !goIdent.MatchString(name) {
		return fmt.Errorf("%s: %q does not produce a valid Go identifier (allowed: ASCII letters, digits, underscore; must not start with a digit)", origin, name)
	}
	return nil
}

// goTypeExpr matches a Go *type expression* with no statement-level escape
// hatches: identifiers, package-qualified identifiers, pointers, slices,
// arrays, maps, and generics composed thereof. The whitelist is on what each
// character can be, not on the overall shape — we leave "this composes into a
// real type" to the downstream Go compiler. What we MUST exclude is anything
// that lets a spec value smuggle past x-go-type substitution: ";", "{", "}",
// "(", ")", quotes, backslashes.
//
// Examples that match: time.Time, *uuid.UUID, []string, map[string]int,
//
//	Foo[Bar], []*pkg.Type
//
// Examples that don't: "T; func init(){…}", "func() int", `pkg.New()`.
var goTypeExpr = regexp.MustCompile(`^[A-Za-z0-9_.*\[\] ,]+$`)

// validateGoTypeExpr enforces that an x-go-type value is a plain type
// expression, blocking obvious code-injection vectors (init bodies, multiple
// declarations on one line, embedded function calls). The check is
// conservative — function and channel type literals are also rejected, but
// users wanting those should hide them behind a named type in their own code
// and reference that named type from x-go-type instead.
func validateGoTypeExpr(origin, expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("%s: x-go-type is empty", origin)
	}
	if !goTypeExpr.MatchString(expr) {
		return fmt.Errorf("%s: x-go-type %q is not a plain Go type expression; only identifiers, qualified names, *, [], map[…] and generics are allowed", origin, expr)
	}
	return nil
}

// goImportPath matches Go import paths conservatively: alphanumerics, dot,
// slash, underscore, dash. Real Go accepts a broader Unicode set, but limiting
// to ASCII closes off homoglyph attacks and matches the realistic set of
// import paths in practice.
var goImportPath = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// validateGoImportPath rejects values that don't look like Go import paths.
// The path is later emitted via %q, so quoting injection is impossible — but
// a path containing ";" or quotes would still be a confusing failure at the
// developer's "go build" step. Catching it here gives a clear yaggo error.
func validateGoImportPath(origin, path string) error {
	if path == "" {
		return nil // optional
	}
	if !goImportPath.MatchString(path) {
		return fmt.Errorf("%s: x-go-import %q is not a valid Go import path (allowed chars: A-Z a-z 0-9 . / _ -)", origin, path)
	}
	return nil
}

// validateSpecPatterns rejects spec patterns that won't compile as Go RE2.
// Without this check the generated code calls regexp.MustCompile at the user's
// package init time — a malformed pattern would panic on every import,
// including unrelated tests that touch the package transitively. Catching it
// at generation time turns that runtime panic into a clear yaggo error.
//
// The walk covers every property schema reachable from a component or from an
// inline request body (so emitBodyPatternVars never emits an unvalidated regex
// literal).
func validateSpecPatterns(api *spec.OpenAPI) error {
	if api == nil {
		return nil
	}
	visited := make(map[*spec.Schema]bool)
	var walk func(origin string, s *spec.Schema) error
	walk = func(origin string, s *spec.Schema) error {
		if s == nil || visited[s] {
			return nil
		}
		visited[s] = true
		if s.Pattern != "" {
			if _, err := regexp.Compile(s.Pattern); err != nil {
				return fmt.Errorf("%s: pattern %q is not a valid Go (RE2) regex: %w", origin, s.Pattern, err)
			}
		}
		for propName, prop := range s.Properties {
			if err := walk(fmt.Sprintf("%s property %q", origin, propName), prop); err != nil {
				return err
			}
		}
		if err := walk(origin+" items", s.Items); err != nil {
			return err
		}
		if err := walk(origin+" additionalProperties", s.AdditionalSchema()); err != nil {
			return err
		}
		for i, a := range s.AllOf {
			if err := walk(fmt.Sprintf("%s allOf[%d]", origin, i), a); err != nil {
				return err
			}
		}
		return nil
	}
	for name, s := range api.Components.Schemas {
		if err := walk(fmt.Sprintf("schema %q", name), effectiveSchema(s, api)); err != nil {
			return err
		}
	}
	// Inline request body schemas — emitBodyPatternVars emits a
	// regexp.MustCompile for any string-typed property here, so the patterns
	// must compile.
	for _, mo := range collectOps(api) {
		if mo.Op.RequestBody == nil {
			continue
		}
		for ct, mt := range mo.Op.RequestBody.Content {
			if mt.Schema == nil || mt.Schema.Ref != "" {
				continue
			}
			origin := fmt.Sprintf("operation %s %s requestBody %s", mo.Method, mo.Path, ct)
			if err := walk(origin, effectiveSchema(mt.Schema, api)); err != nil {
				return err
			}
		}
	}
	return nil
}

// reservedParamLowerNames are identifiers the generated server/client
// signatures bind structurally. A spec parameter that, after toGoName +
// lowerFirst, lands on one of these names would collide with the
// synthesized argument or struct field and produce uncompilable Go. Only
// signature-level / structural names are listed here; function-local names
// (q, h, err, …) are renamed in the templates instead so common query
// parameter names like `q` remain spec-legal.
var reservedParamLowerNames = map[string]string{
	"w":       "server handler binds w http.ResponseWriter",
	"r":       "server handler binds r *http.Request",
	"ctx":     "client method binds ctx context.Context",
	"body":    "server/client bind body to the request body argument",
	"params":  "server handler binds params {Name}Params",
	"headers": "server handler binds headers {Name}Headers",
}

// goKeywords are Go's 25 reserved words. A parameter whose lowerFirst form
// matches one of them is unusable as a variable name regardless of where in
// the signature it lives, so we reject early with a clear message rather
// than letting the user discover a syntax error at `go build` time.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// validateSpecIdentifiers is the entry point that walks every spec value the
// generator interpolates into Go source as an identifier or type expression
// and refuses generation if any would produce broken or attacker-shaped code.
// Called once at the top of each Generate* function.
func validateSpecIdentifiers(api *spec.OpenAPI) error {
	if api == nil {
		return nil
	}
	// Component schema names → top-level type identifiers.
	for name, s := range api.Components.Schemas {
		origin := fmt.Sprintf("schema %q", name)
		if err := validateGoIdent(origin, toGoName(name)); err != nil {
			return err
		}
		if s == nil {
			continue
		}
		if s.XGoType != "" {
			if err := validateGoTypeExpr(origin, s.XGoType); err != nil {
				return err
			}
		}
		if err := validateGoImportPath(origin, s.XGoImport); err != nil {
			return err
		}
		// Property names → struct field identifiers (after toGoName).
		// Detect collisions early: "Name" and "name" both produce field
		// "Name" and the generated struct would have two fields with the
		// same name (compile error).
		seenFields := make(map[string]string, len(s.Properties))
		for prop := range s.Properties {
			field := toGoFieldName(prop)
			if err := validateGoIdent(fmt.Sprintf("%s property %q", origin, prop), field); err != nil {
				return err
			}
			if prev, dup := seenFields[field]; dup {
				return fmt.Errorf("%s: properties %q and %q both produce Go field %q", origin, prev, prop, field)
			}
			seenFields[field] = prop
		}
		// Enum values become typed const identifiers via enumConstSuffix
		// (which knows how to render NaN/Inf, negatives, booleans etc.).
		// The validator MUST use the same helper as the emitter; an
		// independent toGoName-based formula here used to reject specs
		// the emitter would happily generate.
		kind := enumScalarKind(s)
		if len(s.Enum) > 0 && kind != "" {
			for _, v := range s.Enum {
				suffix, ok := enumConstSuffix(v, kind)
				if !ok {
					return fmt.Errorf("%s enum value %v: cannot be represented as a Go identifier suffix", origin, v)
				}
				constName := toGoName(name) + suffix
				if err := validateGoIdent(fmt.Sprintf("%s enum value %v", origin, v), constName); err != nil {
					return err
				}
			}
		}
	}
	// Security-scheme names → exported function suffix in auth.go.
	for name := range api.Components.SecuritySchemes {
		if err := validateGoIdent(fmt.Sprintf("security scheme %q", name), toGoName(name)); err != nil {
			return err
		}
	}
	// Component parameter Name fields surface as Go identifiers downstream
	// (every operation that references them ends up with the same Name).
	for compName, p := range api.Components.Parameters {
		if p == nil || p.Name == "" {
			continue
		}
		if err := validateGoIdent(fmt.Sprintf("components.parameters %q", compName), toGoName(p.Name)); err != nil {
			return err
		}
	}
	// Operation IDs and parameter names → method names, struct field names,
	// local variable names. We iterate via collectOps so path-item-level
	// parameters are checked too.
	//
	// Collision tracking: operationName must be unique across all paths
	// (duplicates produce two methods with the same name), parameter Go
	// fields must be unique inside a {Name}Params/{Name}Headers struct,
	// and parameter Go-locals must not collide with reserved handler args.
	seenOps := make(map[string]string)
	for _, mo := range collectOps(api) {
		opName := operationName(mo.Method, mo.Path, mo.Op)
		origin := fmt.Sprintf("operation %s %s", mo.Method, mo.Path)
		if err := validateGoIdent(origin, opName); err != nil {
			return err
		}
		if prev, dup := seenOps[opName]; dup {
			return fmt.Errorf("operation name %q is produced by both %s and %s %s — give them distinct operationIds", opName, prev, mo.Method, mo.Path)
		}
		seenOps[opName] = fmt.Sprintf("%s %s", mo.Method, mo.Path)

		// Field-level collisions, scoped per location (query, header).
		// Path params don't form a struct, but they do form positional
		// arguments and would collide there.
		seenQueryFields := make(map[string]string)
		seenHeaderFields := make(map[string]string)
		seenLocals := make(map[string]string)
		for _, p := range mo.Op.Parameters {
			// Unresolved parameter $ref: collectOps left the entry with an
			// empty Name; the cryptic "does not produce a valid Go
			// identifier" check below wouldn't tell the user it was the
			// ref that failed. Surface a clear error first.
			if p.Ref != "" && p.Name == "" {
				return fmt.Errorf("%s: parameter $ref %q could not be resolved (check components/parameters and spelling)", origin, p.Ref)
			}
			pname := toGoName(p.Name)
			if err := validateGoIdent(fmt.Sprintf("%s param %q", origin, p.Name), pname); err != nil {
				return err
			}
			local := lowerFirst(pname)
			if reason, reserved := reservedParamLowerNames[local]; reserved {
				return fmt.Errorf("%s param %q resolves to local %q which is reserved (%s)", origin, p.Name, local, reason)
			}
			if goKeywords[local] {
				return fmt.Errorf("%s param %q resolves to local %q which is a Go keyword (rename or alias the parameter)", origin, p.Name, local)
			}
			if prev, dup := seenLocals[local]; dup {
				return fmt.Errorf("%s: params %q and %q both resolve to local %q", origin, prev, p.Name, local)
			}
			seenLocals[local] = p.Name
			switch p.In {
			case paramInQuery:
				if prev, dup := seenQueryFields[pname]; dup {
					return fmt.Errorf("%s: query params %q and %q both produce field %q", origin, prev, p.Name, pname)
				}
				seenQueryFields[pname] = p.Name
			case paramInHeader:
				// HTTP headers are case-insensitive on the wire (RFC 9110)
				// — `X-Trace-ID` and `x-trace-id` are the same header.
				// Two spec entries differing only in case both compile in
				// Go (distinct toGoName outputs) but at runtime r.Header.Get
				// returns the same value into both fields. Almost certainly
				// a spec typo; reject early.
				if prev, dup := seenHeaderFields[pname]; dup {
					return fmt.Errorf("%s: header params %q and %q both produce field %q", origin, prev, p.Name, pname)
				}
				seenHeaderFields[pname] = p.Name
				lower := strings.ToLower(p.Name)
				if prev, dup := seenHeaderFields["_lc_"+lower]; dup {
					return fmt.Errorf("%s: header params %q and %q only differ in case — HTTP headers are case-insensitive (RFC 9110); pick one canonical spelling", origin, prev, p.Name)
				}
				seenHeaderFields["_lc_"+lower] = p.Name
			}
		}
	}
	// Inline request-body property names surface as struct field identifiers
	// AND as raw values inside Validate() error strings ("field '<name>' is
	// required") via bodytypes_gen.go → writeStruct. Unlike component-schema
	// properties (validated in the loop above), inline body properties were
	// otherwise ungated: a crafted name would produce a malformed field
	// declaration that go/format rejects, but the unformatted bytes still land
	// on disk. Validate them here so the "no unvalidated spec string reaches
	// generated source" invariant holds for inline bodies too. Mirror
	// bodytypes_gen exactly: only the JSON-compatible body schema is emitted,
	// and only when it's inline (a $ref reuses an already-validated component).
	for _, mo := range collectOps(api) {
		_, bs := requestBodyContent(mo.Op)
		if bs == nil || bs.Ref != "" {
			continue
		}
		origin := fmt.Sprintf("operation %s %s requestBody", mo.Method, mo.Path)
		eff := effectiveSchema(bs, api)
		seenBodyFields := make(map[string]string, len(eff.Properties))
		for prop := range eff.Properties {
			field := toGoFieldName(prop)
			if err := validateGoIdent(fmt.Sprintf("%s property %q", origin, prop), field); err != nil {
				return err
			}
			if prev, dup := seenBodyFields[field]; dup {
				return fmt.Errorf("%s: properties %q and %q both produce Go field %q", origin, prev, prop, field)
			}
			seenBodyFields[field] = prop
		}
	}
	// Every $ref name is interpolated by schemaToGoType / bodyTypeName as a
	// raw Go identifier (via toGoName(spec.RefName(ref))). The loops above only
	// see component names; a $ref string with a malformed target (typo, or a
	// crafted name like `#/components/schemas/T; var pwned = "x"`) would slip
	// through. Walk every reachable schema and validate each ref name.
	if err := validateSchemaRefs(api); err != nil {
		return err
	}
	// Cyclic alias chains (A → B → A) compile to `type A = B; type B = A`
	// which Go rejects as "invalid recursive type alias". Detect the
	// cycle in the spec so the user gets a clear error.
	if err := validateAliasCycles(api); err != nil {
		return err
	}
	return nil
}

// validateAliasCycles checks every top-level pure-$ref schema for a cycle
// back to itself. Only chains of "schema is just a $ref" matter — a $ref
// from within a struct property is fine because Go's pointer-typed field
// breaks the recursion.
func validateAliasCycles(api *spec.OpenAPI) error {
	for name, s := range api.Components.Schemas {
		if s == nil || s.Ref == "" {
			continue
		}
		seen := map[string]bool{name: true}
		cur := s
		for cur != nil && cur.Ref != "" {
			tgt := spec.RefName(cur.Ref)
			if seen[tgt] {
				return fmt.Errorf("schema %q: alias chain forms a cycle through %q (Go would reject the recursive type aliases)", name, tgt)
			}
			seen[tgt] = true
			next, ok := api.Components.Schemas[tgt]
			if !ok {
				break
			}
			cur = next
		}
	}
	return nil
}

// validateSchemaRefs walks every schema reachable from components and from
// operations, and validates the toGoName() form of any $ref's target name.
// Cyclic refs are broken with a visited set keyed by the ref string itself —
// the ref name is what we check, so visiting the same ref twice is redundant.
func validateSchemaRefs(api *spec.OpenAPI) error {
	visited := make(map[string]bool)
	check := func(origin, ref string) error {
		if visited[ref] {
			return nil
		}
		visited[ref] = true
		name := toGoName(spec.RefName(ref))
		if err := validateGoIdent(fmt.Sprintf("%s $ref %q", origin, ref), name); err != nil {
			return err
		}
		return nil
	}
	var walk func(origin string, s *spec.Schema, walked map[*spec.Schema]bool) error
	walk = func(origin string, s *spec.Schema, walked map[*spec.Schema]bool) error {
		if s == nil || walked[s] {
			return nil
		}
		walked[s] = true
		if s.Ref != "" {
			if err := check(origin, s.Ref); err != nil {
				return err
			}
			// Resolve the target so dangling refs become a generation-time
			// error instead of an opaque "undefined: T" at the consumer's
			// `go build`. Component refs to non-existent names are the most
			// common form of typo, and silently letting them through wastes
			// debugging time.
			resolved, _, err := spec.ResolveSchema(s.Ref, api)
			if err != nil {
				return fmt.Errorf("%s $ref %q: %w", origin, s.Ref, err)
			}
			return walk(origin+" (resolved)", resolved, walked)
		}
		for propName, prop := range s.Properties {
			if err := walk(fmt.Sprintf("%s property %q", origin, propName), prop, walked); err != nil {
				return err
			}
		}
		if err := walk(origin+" items", s.Items, walked); err != nil {
			return err
		}
		if err := walk(origin+" additionalProperties", s.AdditionalSchema(), walked); err != nil {
			return err
		}
		for i, a := range s.AllOf {
			if err := walk(fmt.Sprintf("%s allOf[%d]", origin, i), a, walked); err != nil {
				return err
			}
		}
		return nil
	}
	walked := make(map[*spec.Schema]bool)
	for name, s := range api.Components.Schemas {
		if err := walk(fmt.Sprintf("schema %q", name), s, walked); err != nil {
			return err
		}
	}
	for _, mo := range collectOps(api) {
		opOrigin := fmt.Sprintf("operation %s %s", mo.Method, mo.Path)
		for _, p := range mo.Op.Parameters {
			if err := walk(fmt.Sprintf("%s param %q", opOrigin, p.Name), p.Schema, walked); err != nil {
				return err
			}
		}
		if mo.Op.RequestBody != nil {
			for ct, mt := range mo.Op.RequestBody.Content {
				if err := walk(fmt.Sprintf("%s requestBody %s", opOrigin, ct), mt.Schema, walked); err != nil {
					return err
				}
			}
		}
		for code, resp := range mo.Op.Responses {
			for ct, mt := range resp.Content {
				if err := walk(fmt.Sprintf("%s response %s %s", opOrigin, code, ct), mt.Schema, walked); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// commentLineEndings normalises every line-ending variant to a single space so
// a description embedded as `// {text}` never breaks out of the comment line.
// Order matters: \r\n is matched first so we don't double-replace its halves.
var commentLineEndings = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

// sanitizeComment strips newlines from a description string so it is safe to
// embed as a single-line Go comment. Embedded newlines would otherwise break
// the comment boundary and inject arbitrary lines into the generated file.
func sanitizeComment(s string) string {
	return commentLineEndings.Replace(s)
}

// toGoName converts any identifier (snake_case, kebab-case, camelCase) to PascalCase.
func toGoName(s string) string {
	s = strings.ReplaceAll(s, "-", "_")
	parts := strings.Split(s, "_")
	var b strings.Builder
	b.Grow(len(s))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	// If the input was empty or entirely separators (e.g. "___"), every part
	// got skipped. Return the original so we never emit an empty identifier
	// into generated Go — gofmt would reject that downstream.
	if b.Len() == 0 {
		return s
	}
	return b.String()
}

// toGoFieldName returns an exported struct field name.
func toGoFieldName(name string) string {
	return toGoName(name)
}

// operationName returns a PascalCase function name for an operation.
// Used only when the spec doesn't supply an explicit operationId.
func operationName(method, path string, op *spec.Operation) string {
	if op.OperationID != "" {
		return toGoName(op.OperationID)
	}
	method = strings.ToLower(method)
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var out []string
	switch method {
	case "get":
		// GET /pets/{id} → "GetPetById"; GET /pets → "ListPets". The path's
		// trailing segment being a parameter is the conventional signal that
		// the operation fetches a single resource rather than enumerating.
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

// paramGoType returns the Go type string for a path/query/header parameter
// schema. It deliberately ignores OpenAPI's "nullable: true" hint because
// query/path/header values have no meaningful "null" wire encoding — JSON's
// null doesn't survive URL encoding, an empty string isn't the same thing,
// and the server side can't distinguish "explicitly null" from "missing"
// either way. Conflating nullable with the schemaToGoType pointer wrap
// produces double-pointer field types and broken casts (see the audit notes
// for #1/#2). Required-vs-optional is layered on top of this in buildParam.
func paramGoType(s *spec.Schema) string {
	if s == nil {
		return "string"
	}
	if s.Ref != "" {
		return toGoName(spec.RefName(s.Ref))
	}
	switch s.Type.Primary() {
	case "string":
		return "string"
	case "integer":
		switch s.Format {
		case "int32":
			return "int32"
		case "int64":
			return "int64"
		default:
			return "int"
		}
	case "number":
		if s.Format == "float" {
			return "float32"
		}
		return "float64"
	case "boolean":
		return "bool"
	case "array":
		inner := "any"
		if s.Items != nil {
			inner = paramGoType(s.Items)
		}
		return "[]" + inner
	case "object":
		if s.AdditionalSchema() != nil {
			return "map[string]" + paramGoType(s.AdditionalSchema())
		}
		return "map[string]any"
	default:
		return "any"
	}
}

// schemaToGoType returns the Go type string for an OpenAPI schema.
// optional=true wraps scalar types in a pointer; a schema whose type list
// includes "null" (3.1) or that has nullable: true (3.0) is also wrapped.
//
// Do NOT use for path/query/header parameter types — those go through
// paramGoType, which ignores nullable.
func schemaToGoType(s *spec.Schema, optional bool) string {
	if s == nil {
		return "any"
	}
	pointer := optional || s.IsNullable()
	if s.Ref != "" {
		t := toGoName(spec.RefName(s.Ref))
		if pointer {
			return "*" + t
		}
		return t
	}
	switch s.Type.Primary() {
	case "string":
		if pointer {
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
		if pointer {
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
		if pointer {
			return "*" + t
		}
		return t
	case "boolean":
		if pointer {
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
		if s.AdditionalSchema() != nil {
			return "map[string]" + schemaToGoType(s.AdditionalSchema(), false)
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

// httpMethods lists the HTTP methods in PathItem order. OPTIONS/HEAD/TRACE
// are included so a spec author who declares them gets a handler — earlier
// versions silently dropped these operations. Spec authors who rely on a
// middleware (e.g. CORS preflight) for OPTIONS can still get that behaviour
// by simply not declaring the operation in the spec.
var httpMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE"}

// MethodOp pairs an HTTP method and URL path with its resolved Operation.
// It is the unit of iteration used by the code generators.
type MethodOp struct {
	Method string
	Path   string
	Op     *spec.Operation
}

// collectOps returns all operations from the spec in a stable order.
//
// PathItem-level parameters (declared once and shared by every operation on
// the path) are merged into each operation's Parameters list before the op is
// returned, so downstream code only has to look at op.Parameters. The merge
// follows OpenAPI semantics: operation-level entries override path-item-level
// entries with the same (name, in) pair.
//
// Parameter $refs that target components/parameters are resolved here so
// downstream code only ever sees parameters with concrete Name/In/Schema
// fields. Unresolvable refs surface during validateSpecIdentifiers.
func collectOps(api *spec.OpenAPI) []MethodOp {
	paths := sortedKeys(api.Paths)
	var ops []MethodOp
	for _, path := range paths {
		item := api.Paths[path]
		itemParams := resolveParamRefs(item.Parameters, api)
		for _, m := range httpMethods {
			op := pathItemOp(&item, m)
			if op == nil {
				continue
			}
			merged := *op
			opParams := resolveParamRefs(op.Parameters, api)
			merged.Parameters = mergeParams(itemParams, opParams)
			ops = append(ops, MethodOp{Method: m, Path: path, Op: &merged})
		}
	}
	return ops
}

// resolveParamRefs returns ps with every $ref-typed entry replaced by the
// concrete parameter from components/parameters. Refs that can't be resolved
// are left in place (with empty Name) so validateSpecIdentifiers reports a
// clear error — silently dropping them would hide spec defects.
func resolveParamRefs(ps []spec.Parameter, api *spec.OpenAPI) []spec.Parameter {
	if len(ps) == 0 {
		return ps
	}
	out := make([]spec.Parameter, len(ps))
	for i, p := range ps {
		if p.Ref == "" {
			out[i] = p
			continue
		}
		resolved, _, err := spec.ResolveParameter(p.Ref, api)
		if err != nil || resolved == nil {
			out[i] = p // leave as-is; validation will catch the empty Name
			continue
		}
		out[i] = *resolved
	}
	return out
}

// mergeParams returns the parameters that apply to an operation: every entry
// from the operation's own list plus every path-item-level entry that isn't
// shadowed by an operation-level entry with the same (name, in) key. The
// result preserves operation-level order followed by inherited path-item
// entries — stable enough for generated code to read predictably.
func mergeParams(pathItem, op []spec.Parameter) []spec.Parameter {
	if len(pathItem) == 0 {
		return op
	}
	type key struct{ name, in string }
	seen := make(map[key]struct{}, len(op))
	for _, p := range op {
		seen[key{p.Name, p.In}] = struct{}{}
	}
	out := make([]spec.Parameter, 0, len(op)+len(pathItem))
	out = append(out, op...)
	for _, p := range pathItem {
		if _, dup := seen[key{p.Name, p.In}]; dup {
			continue
		}
		out = append(out, p)
	}
	return out
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
	case "OPTIONS":
		return item.Options
	case "HEAD":
		return item.Head
	case "TRACE":
		return item.Trace
	}
	return nil
}

// Parameter "in:" locations that the generator supports. Cookie parameters
// are parsed but not wired into either client or server output.
const (
	paramInPath   = "path"
	paramInQuery  = "query"
	paramInHeader = "header"
)

// paramsByLocation returns the operation parameters whose `in:` field matches
// location. Header names are case-insensitive on the wire but preserved as
// written here so the generated Go signatures read consistently with the spec.
func paramsByLocation(op *spec.Operation, location string) []spec.Parameter {
	var out []spec.Parameter
	for _, p := range op.Parameters {
		if p.In == location {
			out = append(out, p)
		}
	}
	return out
}

// requestBodyContent returns the JSON-compatible content type and schema for
// an operation's request body. Exact "application/json" wins; otherwise the
// first "*+json" variant in sorted order (e.g. "application/vnd.api+json",
// "application/hal+json"). Returns ("", nil) if no JSON-compatible content
// type is declared — the generator does not currently emit code for XML,
// multipart, or form-encoded bodies.
func requestBodyContent(op *spec.Operation) (string, *spec.Schema) {
	if op.RequestBody == nil {
		return "", nil
	}
	if mt, ok := op.RequestBody.Content["application/json"]; ok {
		return "application/json", mt.Schema
	}
	for _, ct := range sortedKeys(op.RequestBody.Content) {
		if isJSONCompatible(ct) {
			return ct, op.RequestBody.Content[ct].Schema
		}
	}
	return "", nil
}

// successResponseContent returns the JSON-compatible content type and schema
// for the first 2xx response that declares one. The lookup order is:
//
//  1. Concrete 2xx codes in canonical priority — 200 → 201 → 202 → 203 → 204
//     → 205 → 206 — so the most common "happy path" wins when multiple are
//     declared.
//  2. The "2XX" wildcard form (OpenAPI permits it as a catch-all per range).
//  3. The "default" response, which spec authors sometimes use as both the
//     error and success path for endpoints with a single response shape.
//
// Only responses that actually declare JSON-compatible content are
// returned; the order is otherwise a tie-breaker, not a filter.
func successResponseContent(op *spec.Operation) (string, *spec.Schema) {
	codes := []string{"200", "201", "202", "203", "204", "205", "206", "2XX", "default"}
	for _, code := range codes {
		resp, ok := op.Responses[code]
		if !ok {
			continue
		}
		if mt, ok := resp.Content["application/json"]; ok {
			return "application/json", mt.Schema
		}
		for _, ct := range sortedKeys(resp.Content) {
			if isJSONCompatible(ct) {
				return ct, resp.Content[ct].Schema
			}
		}
	}
	return "", nil
}

// isJSONCompatible reports whether a media type uses JSON as its base format.
// Per RFC 6839 §3.1, the "+json" structured-syntax suffix marks any media
// type whose payload is JSON (e.g. application/hal+json, application/vnd.api+json,
// application/problem+json from RFC 7807). Media-type parameters
// (";charset=utf-8" and friends) are stripped before comparison.
func isJSONCompatible(ct string) bool {
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	return ct == "application/json" || strings.HasSuffix(ct, "+json")
}

// mergeAllOf merges allOf schemas into a single flat schema (for code gen purposes).
// Unresolvable $refs inside the allOf are silently skipped — surfacing the error
// here would block generation of every other schema in the spec, and missing
// refs already produce a clear error when they're referenced as field types.
//
// Cyclic allOf chains (A allOf [$ref B], B allOf [$ref A]) are broken with a
// visited-ref set rather than blowing the stack; the entry that re-enters is
// treated as an empty contribution.
func mergeAllOf(schemas []*spec.Schema, api *spec.OpenAPI) *spec.Schema {
	return mergeAllOfVisited(schemas, api, map[string]bool{})
}

func mergeAllOfVisited(schemas []*spec.Schema, api *spec.OpenAPI, visited map[string]bool) *spec.Schema {
	merged := &spec.Schema{
		Type:       spec.SchemaType{"object"},
		Properties: make(map[string]*spec.Schema),
	}
	for _, s := range schemas {
		src := s
		if s.Ref != "" {
			if visited[s.Ref] {
				continue
			}
			visited[s.Ref] = true
			resolved, _, err := spec.ResolveSchema(s.Ref, api)
			if err != nil {
				continue
			}
			src = resolved
		}
		// If the resolved schema is itself an allOf composition, flatten it
		// recursively so nested compositions merge cleanly.
		if len(src.AllOf) > 0 {
			nested := mergeAllOfVisited(src.AllOf, api, visited)
			maps.Copy(merged.Properties, nested.Properties)
			merged.Required = append(merged.Required, nested.Required...)
			continue
		}
		maps.Copy(merged.Properties, src.Properties)
		merged.Required = append(merged.Required, src.Required...)
	}
	merged.Required = dedupStrings(merged.Required)
	return merged
}

func dedupStrings(ss []string) []string {
	if len(ss) == 0 {
		return ss
	}
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
