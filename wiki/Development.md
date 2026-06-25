# Development

How to build, test, and extend yaggo itself.

## Repository layout

```
.
├── cmd/yaggo/                    # main package — CLI entry point
│   ├── main.go
│   └── main_test.go              # E2E tests: parse args, generate files, exit codes
├── internal/
│   ├── spec/                     # OpenAPI YAML → Go structs
│   │   ├── types.go              # Schema, OpenAPI, SecurityScheme, …
│   │   ├── parse.go              # yaml.Unmarshal + version validation
│   │   ├── parse_test.go
│   │   └── oas31_test.go         # 3.1-specific keyword coverage
│   └── gen/                      # spec → Go source
│       ├── templates/            # text/template files (embedded)
│       │   ├── client.go.tmpl
│       │   ├── server.go.tmpl
│       │   └── auth.go.tmpl
│       ├── templates.go          # //go:embed glue
│       ├── helpers.go            # toGoName, schemaToGoType, content-type detection, …
│       ├── tmpldata.go           # buildTmplData — drives templates
│       ├── types_gen.go          # types.go output (direct printer, no template)
│       ├── server_gen.go         # server.go output
│       ├── client_gen.go         # client.go output
│       ├── bodytypes_gen.go      # body_types.go output
│       ├── auth_gen.go           # auth.go output
│       ├── *_test.go             # unit tests
│       └── integration_test.go   # end-to-end: generates a temp module + `go test`s it
├── examples/                     # separate Go module — see Examples wiki page
├── wiki/                         # this wiki, mirrored to GitHub Wiki
├── Makefile
└── README.md
```

The split into `internal/spec` (YAML decode) and `internal/gen` (codegen) is intentional. The spec types are reusable for anyone wanting to read OpenAPI 3.x in Go; the generator is opinionated.

## Makefile targets

Run `make help`:

```
Targets:
  help            Show this help.
  all             Run fmt, vet, test, then build.
  build           Build the yaggo binary into ./bin/.
  install         go install yaggo into $GOPATH/bin.
  clean           Remove ./bin and coverage artifacts.
  test            Run all tests including the slow integration test.
  test-short      Run tests, skipping the integration test that compiles generated code.
  test-race       Run tests with the race detector.
  cover           Run tests with coverage and print the total.
  fmt             Run gofmt on all packages.
  vet             Run go vet on all packages.
  tidy            Run go mod tidy in the root and examples modules.
  check           Run fmt, vet, and test.
  example         Regenerate the committed examples/petstore package.
  example-build   go build the examples module.
```

## Test layers

There are three layers of tests, escalating in cost:

### 1. Unit tests (`*_test.go`)

Drive individual functions with synthetic `spec.OpenAPI` values, assert on the rendered output via substring or `go/format` round-trips. Fast — measured in milliseconds.

```sh
make test-short
```

### 2. Generator-level tests (also `*_test.go`)

Call `GenerateClient`, `GenerateServer`, `GenerateAuth`, etc. and parse the output through `go/format.Source`. Anything that emits malformed Go is caught here before it reaches the integration test.

### 3. Integration test (`internal/gen/integration_test.go`)

The most expensive test, gated on `testing.Short()`. It:

1. Parses `examples/petstore.yaml`.
2. Generates `types.go`, `server.go`, `client.go`, `auth.go` into a fresh `t.TempDir()`.
3. Writes a hand-crafted `options_test.go` containing 25+ test functions that exercise the generated code at runtime — request editors, middleware ordering, error handler customisation, SecretString redaction, every auth helper, content-type negotiation, response body draining, the spec-driven security wrappers.
4. Runs `go mod tidy` and `go test ./...` inside the temp module.

This is the test that catches "looks right but doesn't compile" or "compiles but misbehaves at runtime" bugs. It also serves as executable documentation of the generated API — if you want to know what shape the generated code has, read the smoke string.

```sh
make test          # includes the integration test
```

## Adding a feature

Typical loop for adding a new OpenAPI keyword or template construct:

1. **Add a parsing test in `internal/spec/parse_test.go`** that drives the YAML through `spec.Parse` and asserts the new field surfaces correctly.
2. **Add the field on the `spec.Schema` / `spec.OpenAPI` / etc. type** in `internal/spec/types.go`.
3. **Use it in the generator** — `helpers.go`, `tmpldata.go`, or the template files. Add a unit test alongside.
4. **Exercise it from the integration test smoke string** if there's runtime behaviour to verify.
5. **Run `make example`** and inspect the diff in `examples/petstore/` to confirm the generated code reads well.
6. **`make check`** locally before committing.

## Working on the templates

Templates are embedded into the binary via `//go:embed templates/*.tmpl` in `internal/gen/templates.go`. There is no separate template-build step — edit the `.tmpl` and `go build` picks it up.

Within the smoke string in `integration_test.go`, **never use backticks** — the smoke is itself a backtick-quoted raw string literal. Use double-quoted strings with escape sequences instead. (The compiler will tell you if you slip up, but the error message is opaque.)

## Security review

`gosec` is wired into the typical pre-PR check:

```sh
~/go/bin/gosec -fmt=text -exclude-generated -tests ./...
```

Current baseline: **0 issues**. Several intentional patterns carry `// #nosec GXXX` annotations with a comment explaining why — preserve those.

## Performance considerations

- The CLI runs once and exits, so internal allocations don't matter much. Don't over-engineer.
- The **generated code** runs in user applications and at scale, so allocation hot paths there matter. The per-request `bytes.Buffer` allocation and bounded `drainAndClose` in the client template were designed with that in mind — see [Generated Client](Generated-Client#performance) for the rationale.
- The integration test is the dominant cost in `make test` (~0.6 s). Keep new tests fast unless they need to be E2E.

## Conventions

- **No dependencies in the generator output's client.** The generated `client.go` and `auth.go` should depend on stdlib only. `server.go` depends on chi v5.
- **Generated code must `gofmt`-cleanly.** The generator runs `go/format.Source` after emission; failures write the unformatted source for debugging and return exit 1.
- **Generated code must not import unused packages.** When the generator was migrated away from `var _ = strconv.Itoa` "keep-import" hacks, the imports were made conditional in the templates. Maintain this — every import in a template should be either unconditional and definitely used, or wrapped in a `{{if}}` based on a flag from `tmplData`.
- **Tests follow the layer.** Unit tests in the same package; the cross-package integration test in `internal/gen/integration_test.go`.

## See also

- [Examples](Examples) — the test fixture used by the integration test
- [OpenAPI Coverage](OpenAPI-Coverage) — what the parser currently knows about
- [CLI Reference](CLI-Reference) — what `cmd/yaggo` does
