// Command yaggo generates Go types, an HTTP server interface, and an HTTP client
// from an OpenAPI 3.x YAML spec.
//
// Install:
//
//	go install github.com/123456890987654321/yaggo/cmd/yaggo@latest
//
// Usage:
//
//	yaggo -spec api.yaml -out ./gen -package api
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/123456890987654321/yaggo/internal/gen"
	"github.com/123456890987654321/yaggo/internal/spec"
)

// pkgIdent restricts -package to a plain Go identifier. The value is
// interpolated as `package %s` into every generated file; without validation
// an attacker controlling the CLI args (e.g. a CI wrapper that derives flags
// from upstream metadata) could inject code at the package declaration line.
var pkgIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// version is overridable at build time via -ldflags "-X main.version=...".
// When unset, runtime/debug.ReadBuildInfo() provides the module version that
// `go install` recorded.
var version = ""

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, defaultTasks))
}

type genTask struct {
	filename string
	fn       func(io.Writer, *spec.OpenAPI, string) error
}

// defaultTasks lists the files the generator emits, in deterministic order.
// Tests inject their own task list into run() instead of mutating this slice,
// which keeps the production binary's task list immutable and lets tests run
// in parallel.
var defaultTasks = []genTask{
	{"types.go", gen.GenerateTypes},
	{"server.go", gen.GenerateServer},
	{"client.go", gen.GenerateClient},
	{"body_types.go", gen.GenerateBodyTypes},
	{"auth.go", gen.GenerateAuth},
}

// taskNames returns the short names ("types", "server", …) the user can pass
// to -only/-skip, derived from the supplied task list.
func taskNames(tasks []genTask) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, strings.TrimSuffix(t.filename, ".go"))
	}
	return out
}

// run is the testable entry point. It returns a process exit code so main()
// can stay tiny and side-effect-free except for os.Exit.
//
// tasks is injected so tests can swap in stub task functions without touching
// package-level state.
func run(args []string, stdout, stderr io.Writer, tasks []genTask) int {
	fs := flag.NewFlagSet("yaggo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	specFile := fs.String("spec", "", "path to OpenAPI 3.x YAML spec (required)")
	outDir := fs.String("out", ".", "output directory for generated files")
	pkg := fs.String("package", "api", "Go package name for generated files")
	only := fs.String("only", "", "comma-separated subset of files to generate ("+strings.Join(taskNames(tasks), ",")+"); mutually exclusive with -skip")
	skip := fs.String("skip", "", "comma-separated files to skip; mutually exclusive with -only")
	strict := fs.Bool("strict", false, "reject unknown YAML keys in the spec (catches typos)")
	check := fs.Bool("check", false, "validate and run code generation without writing files (CI dry-run)")
	versionFlag := fs.Bool("version", false, "print yaggo version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *versionFlag {
		fmt.Fprintln(stdout, versionString())
		return 0
	}

	if *specFile == "" {
		fmt.Fprintln(stderr, "error: -spec is required")
		fs.Usage()
		return 1
	}
	if !pkgIdent.MatchString(*pkg) {
		fmt.Fprintf(stderr, "error: -package %q is not a valid Go identifier\n", *pkg)
		return 1
	}
	if *only != "" && *skip != "" {
		fmt.Fprintln(stderr, "error: -only and -skip are mutually exclusive")
		return 1
	}

	selected, err := selectTasks(tasks, *only, *skip)
	if err != nil {
		fmt.Fprintf(stderr, "yaggo: %v\n", err)
		return 1
	}

	specPath := filepath.Clean(*specFile)
	var parseOpts []spec.ParseOption
	if *strict {
		parseOpts = append(parseOpts, spec.WithStrict())
	}
	api, err := spec.Parse(specPath, parseOpts...)
	if err != nil {
		fmt.Fprintf(stderr, "yaggo: parsing spec: %v\n", err)
		return 1
	}

	if len(api.Paths) == 0 && len(api.Webhooks) == 0 {
		fmt.Fprintln(stderr, "yaggo: warning: spec has no paths or webhooks; only types.go will have content")
	}
	warnSpecConcerns(api, stderr)

	if !*check {
		if err := os.MkdirAll(*outDir, 0o700); err != nil {
			fmt.Fprintf(stderr, "yaggo: creating output dir: %v\n", err)
			return 1
		}
	}

	for _, t := range selected {
		var buf bytes.Buffer
		if err := t.fn(&buf, api, *pkg); err != nil {
			fmt.Fprintf(stderr, "yaggo: generating %s: %v\n", t.filename, err)
			return 1
		}
		src := buf.Bytes()
		if len(bytes.TrimSpace(src)) == 0 {
			continue
		}
		out := filepath.Join(*outDir, t.filename)
		formatted, err := format.Source(src)
		if err != nil {
			if !*check {
				// Write unformatted so the user can debug the broken output.
				_ = os.WriteFile(out, src, 0o600)
			}
			fmt.Fprintf(stderr, "yaggo: formatting %s: %v\n", t.filename, err)
			if !*check {
				fmt.Fprintf(stderr, "(unformatted file written for debugging)\n")
			}
			return 1
		}
		if *check {
			fmt.Fprintf(stdout, "ok %s (%d bytes)\n", t.filename, len(formatted))
			continue
		}
		if err := os.WriteFile(out, formatted, 0o600); err != nil {
			fmt.Fprintf(stderr, "yaggo: writing %s: %v\n", out, err)
			return 1
		}
		fmt.Fprintf(stdout, "wrote %s\n", out)
	}
	return 0
}

// selectTasks resolves -only / -skip against the supplied task list. Both
// args empty → all tasks. Unknown names produce an error so a typo is loud.
func selectTasks(tasks []genTask, only, skip string) ([]genTask, error) {
	known := make(map[string]genTask, len(tasks))
	for _, t := range tasks {
		known[strings.TrimSuffix(t.filename, ".go")] = t
	}
	parse := func(csv string) ([]string, error) {
		var out []string
		for _, p := range strings.Split(csv, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if _, ok := known[p]; !ok {
				return nil, fmt.Errorf("unknown file %q (valid: %s)", p, strings.Join(taskNames(tasks), ","))
			}
			out = append(out, p)
		}
		return out, nil
	}
	switch {
	case only != "":
		picked, err := parse(only)
		if err != nil {
			return nil, err
		}
		var out []genTask
		for _, name := range picked {
			out = append(out, known[name])
		}
		return out, nil
	case skip != "":
		skipped, err := parse(skip)
		if err != nil {
			return nil, err
		}
		skipSet := make(map[string]struct{}, len(skipped))
		for _, s := range skipped {
			skipSet[s] = struct{}{}
		}
		var out []genTask
		for _, t := range tasks {
			name := strings.TrimSuffix(t.filename, ".go")
			if _, drop := skipSet[name]; drop {
				continue
			}
			out = append(out, t)
		}
		return out, nil
	default:
		return tasks, nil
	}
}

// warnSpecConcerns emits non-fatal warnings for spec patterns that are
// generated as-asked but likely to bite the user at runtime: TRACE handlers
// (XST risk), path placeholders that don't line up with declared parameters
// (silent 400s or missing args), cookie parameters (silently dropped — no
// wiring at present), and security requirements (parsed but not wired into
// the generated handlers; user must install middleware).
func warnSpecConcerns(api *spec.OpenAPI, stderr io.Writer) {
	rootSecurity := len(api.Security) > 0
	for _, path := range sortedPaths(api.Paths) {
		item := api.Paths[path]
		if item.Trace != nil {
			fmt.Fprintf(stderr, "yaggo: warning: TRACE handler declared on %s — TRACE responses echo the request and are an XST attack vector; reject TRACE at the proxy or via middleware\n", path)
		}
		// Placeholder consistency: collect both sets and diff.
		placeholders := pathPlaceholders(path)
		ops := []*spec.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete, item.Options, item.Head, item.Trace}
		methodNames := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD", "TRACE"}
		for idx, op := range ops {
			if op == nil {
				continue
			}
			method := methodNames[idx]
			declared := make(map[string]bool)
			for _, p := range item.Parameters {
				if p.In == "path" {
					declared[p.Name] = true
				}
				if p.In == "cookie" {
					fmt.Fprintf(stderr, "yaggo: warning: %s %s declares cookie parameter %q — cookie parameters are not currently wired into generated handlers; read r.Cookie(%q) manually\n", method, path, p.Name, p.Name)
				}
			}
			for _, p := range op.Parameters {
				if p.In == "path" {
					declared[p.Name] = true
				}
				if p.In == "cookie" {
					fmt.Fprintf(stderr, "yaggo: warning: %s %s declares cookie parameter %q — cookie parameters are not currently wired into generated handlers; read r.Cookie(%q) manually\n", method, path, p.Name, p.Name)
				}
			}
			for name := range placeholders {
				if !declared[name] {
					fmt.Fprintf(stderr, "yaggo: warning: path %s has placeholder {%s} with no matching parameter — handler will receive an empty string for this segment\n", path, name)
				}
			}
			for name := range declared {
				if !placeholders[name] {
					fmt.Fprintf(stderr, "yaggo: warning: path %s declares path parameter %q but the URL has no {%s} placeholder — every request will fail parsing\n", path, name, name)
				}
			}
			// Per-operation security requirements are parsed but not wired
			// into generated handlers. Root-level security applies to every
			// op; warn once below rather than per-op to keep output sane.
			if len(op.Security) > 0 {
				fmt.Fprintf(stderr, "yaggo: warning: %s %s declares 'security' but yaggo does not generate auth enforcement — install middleware (e.g. via WithMiddleware) that validates the declared schemes\n", method, path)
			}
			// Authorization conflict: spec parameter named Authorization
			// will be overwritten by any of yaggo's auth helpers (BearerToken /
			// APIKey / …) because RequestEditors run after spec headers.
			// Check both path-item-level and operation-level parameters since
			// both end up merged into the generated handler signature.
			for _, hp := range item.Parameters {
				if hp.In == "header" && strings.EqualFold(hp.Name, "Authorization") {
					fmt.Fprintf(stderr, "yaggo: warning: %s %s declares header parameter \"Authorization\" (at path-item level) — yaggo's auth helpers (BearerToken/APIKey…) run as RequestEditors after spec headers and will overwrite this value; install the spec header via WithRequestEditor if you need it to win\n", method, path)
				}
			}
			for _, hp := range op.Parameters {
				if hp.In == "header" && strings.EqualFold(hp.Name, "Authorization") {
					fmt.Fprintf(stderr, "yaggo: warning: %s %s declares header parameter \"Authorization\" — yaggo's auth helpers (BearerToken/APIKey…) run as RequestEditors after spec headers and will overwrite this value; install the spec header via WithRequestEditor if you need it to win\n", method, path)
				}
			}
			// Multi-type fields (3.1 type: [string, integer]) collapse to the
			// first non-null type; other branches silently lose decode. Warn
			// per-property so spec authors know to refactor to oneOf or a
			// single dominant type.
			warnMultiTypeOpSchemas(method, path, op, stderr)
			// Nested-array params produce broken slice<->string casts. Warn
			// before yaggo writes uncompilable code.
			warnNestedArrayParams(method, path, op, api, stderr)
			// Non-JSON / empty request bodies are silently dropped from
			// codegen — the user gets a handler with no body argument.
			// Split the two cases: empty content map is a spec defect
			// (the requestBody has no media types at all), while a
			// non-empty non-JSON list (multipart, xml, …) is a feature
			// gap we can describe more precisely.
			if op.RequestBody != nil && !anyJSONContent(op.RequestBody.Content) {
				if len(op.RequestBody.Content) == 0 {
					fmt.Fprintf(stderr, "yaggo: warning: %s %s requestBody has no content media types declared — handler will be generated without a body argument\n", method, path)
				} else {
					cts := contentTypeList(op.RequestBody.Content)
					fmt.Fprintf(stderr, "yaggo: warning: %s %s requestBody declares only non-JSON content types (%s) — yaggo does not generate decoders for these; read r.Body directly\n", method, path, cts)
				}
			}
		}
	}
	if rootSecurity {
		fmt.Fprintln(stderr, "yaggo: warning: spec declares root-level 'security' but yaggo does not generate auth enforcement — install middleware that validates the declared schemes")
	}
	// Multi-type warnings on component schemas (visited once each).
	warnMultiTypeComponents(api, stderr)
	// uniqueItems with non-comparable element types (arrays, objects) will
	// panic at runtime because the generated Validate() uses map[any]struct{}.
	warnUniqueItemsNonComparable(api, stderr)
}

// anyJSONContent reports whether the requestBody declares at least one
// JSON-compatible media type (application/json or *+json suffix).
func anyJSONContent(content map[string]spec.MediaType) bool {
	for ct := range content {
		base := ct
		if i := strings.IndexByte(base, ';'); i >= 0 {
			base = strings.TrimSpace(base[:i])
		}
		if base == "application/json" || strings.HasSuffix(base, "+json") {
			return true
		}
	}
	return false
}

// contentTypeList renders the media-type keys in sorted order for log lines.
func contentTypeList(content map[string]spec.MediaType) string {
	keys := make([]string, 0, len(content))
	for k := range content {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// warnMultiTypeOpSchemas walks the schemas reachable from an operation's
// parameters / requestBody / responses and warns on every multi-type field.
// We don't recurse into refs — those reach top-level components and get
// covered by warnMultiTypeComponents.
func warnMultiTypeOpSchemas(method, path string, op *spec.Operation, stderr io.Writer) {
	for _, p := range op.Parameters {
		warnIfMultiType(stderr, p.Schema, fmt.Sprintf("%s %s param %q", method, path, p.Name))
	}
	if op.RequestBody != nil {
		for ct, mt := range op.RequestBody.Content {
			warnIfMultiType(stderr, mt.Schema, fmt.Sprintf("%s %s requestBody %s", method, path, ct))
		}
	}
	for code, resp := range op.Responses {
		for ct, mt := range resp.Content {
			warnIfMultiType(stderr, mt.Schema, fmt.Sprintf("%s %s response %s %s", method, path, code, ct))
		}
	}
}

// warnMultiTypeComponents walks every component schema once and warns on
// every multi-type field discovered through Properties / Items / AllOf.
func warnMultiTypeComponents(api *spec.OpenAPI, stderr io.Writer) {
	visited := make(map[*spec.Schema]bool)
	var walk func(origin string, s *spec.Schema)
	walk = func(origin string, s *spec.Schema) {
		if s == nil || visited[s] {
			return
		}
		visited[s] = true
		warnIfMultiType(stderr, s, origin)
		propNames := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			propNames = append(propNames, k)
		}
		sort.Strings(propNames)
		for _, name := range propNames {
			walk(fmt.Sprintf("%s property %q", origin, name), s.Properties[name])
		}
		walk(origin+" items", s.Items)
		walk(origin+" additionalProperties", s.AdditionalSchema())
		for i, a := range s.AllOf {
			walk(fmt.Sprintf("%s allOf[%d]", origin, i), a)
		}
	}
	schemaNames := make([]string, 0, len(api.Components.Schemas))
	for k := range api.Components.Schemas {
		schemaNames = append(schemaNames, k)
	}
	sort.Strings(schemaNames)
	for _, name := range schemaNames {
		walk(fmt.Sprintf("schema %q", name), api.Components.Schemas[name])
	}
}

// warnUniqueItemsNonComparable warns when a schema uses uniqueItems: true on
// an array whose element type is itself an array or object. Go's generated
// Validate() checks uniqueness via map[any]struct{}, which panics at runtime
// when the element type is not comparable (slices and maps are not comparable).
func warnUniqueItemsNonComparable(api *spec.OpenAPI, stderr io.Writer) {
	visited := make(map[*spec.Schema]bool)
	var walk func(origin string, s *spec.Schema)
	walk = func(origin string, s *spec.Schema) {
		if s == nil || visited[s] {
			return
		}
		visited[s] = true
		if s.Type.Primary() == "array" && s.UniqueItems && s.Items != nil {
			itemPrimary := s.Items.Type.Primary()
			if itemPrimary == "array" || itemPrimary == "object" {
				fmt.Fprintf(stderr, "yaggo: warning: %s has uniqueItems: true with %s-typed items — the generated Validate() uses map[any]struct{} which panics at runtime for non-comparable types; use a primitive item type or validate uniqueness manually\n", origin, itemPrimary)
			}
		}
		propNames := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			propNames = append(propNames, k)
		}
		sort.Strings(propNames)
		for _, name := range propNames {
			walk(fmt.Sprintf("%s property %q", origin, name), s.Properties[name])
		}
		walk(origin+" items", s.Items)
		walk(origin+" additionalProperties", s.AdditionalSchema())
		for i, a := range s.AllOf {
			walk(fmt.Sprintf("%s allOf[%d]", origin, i), a)
		}
	}
	schemaNames := make([]string, 0, len(api.Components.Schemas))
	for k := range api.Components.Schemas {
		schemaNames = append(schemaNames, k)
	}
	sort.Strings(schemaNames)
	for _, name := range schemaNames {
		walk(fmt.Sprintf("schema %q", name), api.Components.Schemas[name])
	}
}

// warnNestedArrayParams flags path/query/header parameters typed as
// "array of array of …". The URL form/style spec doesn't define an
// unambiguous wire encoding for 2D arrays, and yaggo's codegen would
// emit broken element casts (slice ↔ string). Documenting it loud
// beats compile errors at the consumer's `go build`.
func warnNestedArrayParams(method, path string, op *spec.Operation, api *spec.OpenAPI, stderr io.Writer) {
	for _, p := range op.Parameters {
		if p.In != "path" && p.In != "query" && p.In != "header" {
			continue
		}
		// Resolve once: a parameter schema can be either inline or a $ref
		// to a top-level array component.
		s := p.Schema
		if s != nil && s.Ref != "" {
			if r, _, err := spec.ResolveSchema(s.Ref, api); err == nil {
				s = r
			}
		}
		if s == nil || s.Type.Primary() != "array" || s.Items == nil {
			continue
		}
		items := s.Items
		if items.Ref != "" {
			if r, _, err := spec.ResolveSchema(items.Ref, api); err == nil {
				items = r
			}
		}
		if items != nil && items.Type.Primary() == "array" {
			fmt.Fprintf(stderr, "yaggo: warning: %s %s param %q is array-of-array — URL query/header encoding for 2D arrays is ambiguous and yaggo will emit broken code; collapse to a single-dim array or send via requestBody\n", method, path, p.Name)
		}
	}
}

// warnIfMultiType emits a single warning when a schema declares more than
// one concrete type (3.1 "type: [string, integer]" form). yaggo picks the
// first non-null type and ignores the rest, so values typed against the
// dropped branches silently fail to decode.
func warnIfMultiType(stderr io.Writer, s *spec.Schema, origin string) {
	if s == nil {
		return
	}
	var concrete []string
	for _, t := range s.Type {
		if t == "null" {
			continue
		}
		concrete = append(concrete, t)
	}
	if len(concrete) > 1 {
		fmt.Fprintf(stderr, "yaggo: warning: %s declares multiple types %v — yaggo emits Go for %q only; values typed as the other branches will fail to decode\n", origin, concrete, concrete[0])
	}
}

func sortedPaths(m map[string]spec.PathItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pathPlaceholders extracts {name} segments from a URL path. Empty or
// malformed placeholders (e.g. `{}` or `{a{b}}`) are skipped — they're spec
// defects that chi would reject at registration anyway.
func pathPlaceholders(path string) map[string]bool {
	out := make(map[string]bool)
	for _, seg := range strings.Split(path, "/") {
		if len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			name := seg[1 : len(seg)-1]
			if name != "" && !strings.ContainsAny(name, "{}") {
				out[name] = true
			}
		}
	}
	return out
}

// versionString returns the version recorded in the binary (or "dev" if neither
// the build flag nor module info is available).
func versionString() string {
	if version != "" {
		return "yaggo " + version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return "yaggo " + info.Main.Version
	}
	return "yaggo dev"
}
