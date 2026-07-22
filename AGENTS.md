# AGENTS.md

Guidance for AI agents (and humans) working in **yaggo** — a code generator that
reads an OpenAPI 3.x YAML spec and emits typed Go: `types.go`, a chi/v5
`server.go`, an HTTP `client.go`, `body_types.go`, and auth `auth.go`.

Keep this file in sync when conventions change. It is the source of truth for
"how we work here"; the deeper *why* lives in the wiki (see [Documentation](#documentation)).

## The one thing to internalise first

yaggo turns an **untrusted OpenAPI spec into Go source that gets compiled and
deployed**. The spec is the attack surface. Every spec-derived string that lands
in generated code is a potential injection vector. The trust boundary is
`validateSpecIdentifiers` / `validateSpecPatterns` in
[internal/gen/helpers.go](internal/gen/helpers.go), called at the top of every
`Generate*` function. When you touch codegen, assume a malicious spec and keep
that boundary intact. See [Security invariants](#security-invariants).

## Commands

Use the Makefile — `make help` lists everything. The ones you'll actually run:

| Command | Use |
|---|---|
| `make check` | **The pre-commit gate.** Runs `fmt` + `vet` + `test`. Green before you're done. |
| `make test` | All tests, including the slow integration test that compiles generated code. |
| `make test-short` | Skips the integration test (`-short`). Fast inner loop. |
| `make test-race` | Race detector. CI runs `go test -race ./...`; run it before finishing concurrency-adjacent work. |
| `make example` | Regenerate `examples/petstore/` from `examples/petstore.yaml`. **Run after any change to templates or codegen output.** |
| `make example-build` | `go build` the examples module (separate module — see below). |
| `make cover` | Coverage total. |

Run a single test: `go test ./internal/gen -run TestSecurity_XGoTypeInjectionRejected`.

### What CI enforces (`.github/workflows/ci.yml`)

Match it locally so you don't get a red PR:

- `gofmt -l cmd internal` must be **empty** (no unformatted files).
- `go vet ./...` clean.
- `go test -race ./...` passes.
- Examples module builds: `cd examples && go vet ./... && go build ./...`.
- **Generated petstore is in sync**: CI regenerates into a temp dir and `diff`s
  against the committed `examples/petstore/*.go`. If you change a template or the
  generator and forget `make example`, CI fails on the diff.
- `govulncheck ./...` clean.
- Tested against Go 1.23 (the `go.mod` floor) and `stable`.

Also run `gosec` before a PR (baseline: **0 issues**):

```sh
gosec -exclude-generated -tests ./...
```

Intentional patterns carry `// #nosec GXXX` with a justifying comment — preserve them.

## Repository layout

```
cmd/yaggo/          # CLI entry point (arg parsing, file writing, spec-concern warnings)
internal/
  spec/             # OpenAPI YAML → Go structs. Reusable decoder; no codegen here.
    types.go        # Schema, OpenAPI, SecurityScheme, custom UnmarshalYAML shims
    parse.go        # Parse(): os.ReadFile + yaml decode + version check
  gen/              # spec → Go source. Opinionated.
    templates/      # text/template files, //go:embed-ed (server, client, auth)
    helpers.go      # toGoName, schemaToGoType, the validateSpec* trust boundary
    tmpldata.go     # buildTmplData — precomputes everything templates need
    types_gen.go    # types.go output — a DIRECT PRINTER, no template
    server_gen.go   # server.go output (template-driven)
    client_gen.go   # client.go output (template-driven)
    bodytypes_gen.go # body_types.go output (inline request bodies)
    auth_gen.go     # auth.go output (template-driven)
examples/           # SEPARATE Go module. petstore.yaml + committed generated output.
```

The `internal/spec` ↔ `internal/gen` split is deliberate: `spec` is a clean,
reusable OpenAPI 3.x reader; `gen` is the opinionated generator. Don't leak
codegen concerns into `spec`.

`examples/` is its **own module** (own `go.mod`) so the generated client can be
verified to depend on stdlib-only and the server on chi-only, with no accidental
inheritance of the root module's dev deps. `make tidy` tidies both.

## Architecture notes

- **Two emission styles, on purpose.** `types.go` is produced by a hand-written
  *printer* (`p.line` / `p.linef` in [types_gen.go](internal/gen/types_gen.go))
  because its structure is data-driven and recursive. `server`/`client`/`auth`
  use `text/template` files. When editing generated *shape*, know which side
  you're on. New scalar/struct/enum logic → the printer. New HTTP handling →
  the template.
- **`tmpldata.go` does the thinking; templates stay dumb.** Precompute Go type
  strings, flags (`HasBody`, `BodyRequired`, `UsesStrconvSrv`, …), and set
  expressions in `buildOp`/`buildParam`, so templates only branch on booleans and
  interpolate ready-made strings. Don't put logic in templates that could live in
  Go.
- **`collectOps` is the iteration unit.** It merges path-item-level parameters
  into each operation and resolves parameter `$ref`s, so downstream code only
  sees concrete `op.Parameters`. Use it; don't re-walk `api.Paths` by hand.

## Coding conventions

- **Go style is the baseline.** gofmt-clean, `go vet`-clean. Match the
  surrounding code's naming and idiom.
- **Comments explain *why*, and they earn their place.** This codebase is
  heavily commented with the rationale behind non-obvious choices (security
  trade-offs, spec-compliance corners, why a race isn't worth pooling for).
  Match that density where the reasoning is non-obvious; don't narrate the
  obvious. When you fix a subtle bug, leave a comment so it doesn't regress.
- **Minimal dependencies, guarded jealously.** Root module: `yaml.v3` +
  `testify` (test-only). The **generated client and auth must import stdlib
  only**; the generated server may import chi v5. Never add a dependency to
  generator output. Adding one to the generator itself needs a real reason.
- **Determinism is mandatory.** Generated output must be byte-stable across runs
  or the CI sync-check flaps. Never range a `map` directly when emitting — sort
  keys first (`sortedKeys`, `sort.Strings`, or the sorted-name walks in
  `warn*`/`validate*`). Any new map iteration in a codegen path needs a sort.
- **Conditional imports in templates.** Generated code must not import unused
  packages. Every import in a template is either unconditional-and-always-used or
  wrapped in `{{if .UsesX}}` driven by a flag computed in `tmpldata.go`. When you
  make a template use a new stdlib package conditionally, add and set the flag.
  (No `var _ = pkg.Thing` keep-import hacks.)
- **Generated code must `gofmt`-cleanly.** The generator runs
  `go/format.Source` after emission; on failure it writes the *unformatted*
  source for debugging and returns a non-zero exit. That debug-write is exactly
  why identifier validation must happen *before* emission (see below).

## Security invariants

These are not optional. Regression tests named `TestSecurity_*` in
[internal/gen/types_gen_test.go](internal/gen/types_gen_test.go) lock them in —
add one for every new injection surface you introduce or fix.

- **Validate before you emit.** Any spec string interpolated into source as an
  identifier or type expression goes through `validateGoIdent` /
  `validateGoTypeExpr` / `validateGoImportPath` *first*. This includes schema
  names, property names (component **and** inline request-body), enum-const
  suffixes, `$ref` target names, operation IDs, parameter names, security-scheme
  names, and `x-go-type` / `x-go-import`. If you add a new place where a spec
  value becomes an identifier, extend `validateSpecIdentifiers` to cover it —
  don't rely on `go/format` to reject it downstream (the unformatted file is
  still written to disk, which is the foothold we're closing).
- **Patterns are pre-compiled.** Spec `pattern` regexes are validated as Go RE2
  via `validateSpecPatterns` before they reach a `regexp.MustCompile(%q)` in
  generated code — otherwise a malformed pattern panics at the consumer's package
  init. Emit patterns only with `%q`.
- **Descriptions/summaries are sanitised, never validated.** Free-text goes
  through `sanitizeComment` (strips CR/LF) and is emitted only as `// single-line`
  comments. Never emit spec free-text into a code position.
- **Generated server hardening — keep it.** `http.MaxBytesReader` on request
  bodies (413), Content-Type check (415), `DisallowUnknownFields` for
  `additionalProperties:false`, and a default error handler that returns
  `http.StatusText(status)` — **never echo `err.Error()` to the client** (it
  leaks decoder internals / user-sent bytes). The full `err` still reaches the
  `ErrorHandler` callback for server-side logging.
- **Generated client hardening — keep it.** `url.PathEscape` on string path
  params, `url.Values.Encode` for query, `safeRedirectPolicy` (strips
  `Authorization` on HTTPS→HTTP downgrade), `io.LimitReader` response cap,
  bounded `drainAndClose` for keep-alive.
- **Credentials are `SecretString`.** It redacts via `String`, `GoString`,
  `MarshalJSON`, `MarshalText`, and `LogValue`. The only way out is `.Reveal()`,
  called at the HTTP boundary. Auth editors refuse non-HTTPS URLs unless
  `AllowInsecure()` is passed (`requireSecure`).

## Testing conventions

Three layers, escalating in cost (see the wiki's Development page for detail):

1. **Unit tests** (`*_test.go`): drive individual functions with synthetic
   `spec.OpenAPI` values; assert via substring or `go/format` round-trip. Fast.
2. **Generator-level tests**: call `GenerateServer` / `GenerateClient` / … and
   parse the output through `go/format.Source`. Catches malformed Go before the
   integration test.
3. **Integration test** ([internal/gen/integration_test.go](internal/gen/integration_test.go)),
   gated on `testing.Short()`: parses `examples/petstore.yaml`, generates into a
   `t.TempDir()`, writes a hand-crafted smoke test, then `go mod tidy` + `go test`
   inside the temp module. This is the "compiles and behaves at runtime" gate.

Rules:

- **New keyword/behaviour → add a test at the lowest layer that can see it**, and
  exercise runtime behaviour from the integration smoke string when there is any.
- **The integration smoke string is a backtick-quoted raw literal — never use
  backticks inside it.** Use double-quoted strings with escapes. The compiler
  error if you slip is opaque.
- **Security fixes ship with a `TestSecurity_*` regression test** asserting the
  malicious spec is rejected (`require.Contains(err, "does not produce a valid Go
  identifier")` and friends) and that nothing is written to disk.
- Prefer `testify/require` (already the convention).

## Adding a feature (typical loop)

1. Parsing test in `internal/spec/parse_test.go` driving the YAML through
   `spec.Parse`.
2. Add the field to the relevant `spec` type in `internal/spec/types.go`.
3. Use it in the generator — `helpers.go` / `tmpldata.go` / a template / the
   printer. If it becomes an identifier, wire it into `validateSpecIdentifiers`.
   Add a unit test alongside.
4. If there's runtime behaviour, exercise it from the integration smoke string.
5. `make example` and eyeball the diff in `examples/petstore/`.
6. `make check` (and `gosec`) before committing.

## Documentation

The wiki is a **separate GitHub-Wiki checkout** at `../yaggo.wiki/` (sibling of
this repo, not a subdirectory). Pages: Home, Installation, CLI-Reference,
Generated-Types, Generated-Server, Generated-Client, Authentication,
OpenAPI-Coverage, Vendor-Extensions, Examples, Development.

When you change observable behaviour (generated code shape, a flag, a validation
rule, a security guarantee), **update the matching wiki page in the same change**
and make sure it describes what the code actually does — the wiki has drifted
before and inaccurate security docs are worse than none. `README.md` covers the
flag table and the five output files; keep it truthful too.

## Gotchas

- Forgetting `make example` after a template/codegen change → CI sync-check fails.
- Ranging a map in an emission path → non-deterministic output → flaky sync-check.
- `lowerFirst` lives in `server_gen.go` (an architectural oddity; it's shared).
- `examples/` is a distinct module; root `go test ./...` does not cover it —
  `make example-build` does.
- Editing `internal/gen/templates.go`? It's just the `//go:embed` glue; the real
  content is the `.tmpl` files. No separate build step — `go build` re-embeds.
