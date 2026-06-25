# CLI Reference

The `yaggo` binary is a one-shot generator: it reads a spec, emits Go source files, and exits.

## Synopsis

```
yaggo -spec <path> [-out <dir>] [-package <name>]
      [-only <list>] [-skip <list>]
      [-strict] [-check]
      [-version]
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-spec` | *(required)* | Path to the OpenAPI 3.x YAML file. |
| `-out` | `.` | Output directory. Created with `0o700` if missing (not created in `-check` mode). |
| `-package` | `api` | Go package name written into every generated file. |
| `-only` | *(unset)* | Comma-separated subset of files to generate. Valid names: `types`, `server`, `client`, `body_types`, `auth`. Mutually exclusive with `-skip`. |
| `-skip` | *(unset)* | Comma-separated files to skip; everything else is generated. Mutually exclusive with `-only`. |
| `-strict` | `false` | Reject unknown YAML keys in the spec. Catches misspelled OpenAPI keywords (e.g. `responsess:`) instead of silently ignoring them. |
| `-check` | `false` | Validate the spec and run the full generation pipeline without writing any files. Each pass prints `ok <name>` on stdout when it produces well-formed Go. Useful in CI to verify a spec round-trips. |
| `-version` | — | Print yaggo's version and exit. The version comes from `-ldflags "-X main.version=..."` at build time, or from `runtime/debug.ReadBuildInfo()` when `go install`-ed; falls back to `dev` for local builds. |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All requested files generated successfully (or skipped because their content would be empty). |
| `1` | A runtime error — spec parse failure, missing required flag, output-directory creation failed, generator returned an error, or `go/format` rejected the output. When `go/format` fails (and `-check` is **not** set), the unformatted source is still written so you can inspect the broken output. |
| `2` | Flag parse error (e.g. unknown flag). |

## Example invocations

### Plain run

```sh
yaggo -spec api.yaml -out ./gen -package api
```

Writes `gen/types.go`, `gen/server.go`, `gen/client.go`, `gen/auth.go`, and (if the spec has inline request bodies) `gen/body_types.go`.

### Custom package name

```sh
yaggo -spec petstore.yaml -out ./internal/petstoreapi -package petstoreapi
```

The package name controls the `package <name>` line at the top of every generated file. Use a Go-identifier-safe name; the generator does not transform it.

### Generate just the client

```sh
yaggo -spec api.yaml -only types,client -out ./gen
```

Common shape: a CLI tool calling a third-party service. You want the type-safe client but no server scaffolding. Authentication helpers are part of the `auth` task — add `,auth` if you need them.

### Skip server-only files

```sh
yaggo -spec api.yaml -skip server -out ./gen
```

The mirror of `-only`. Generates everything except the named files.

### CI dry-run

```sh
yaggo -spec api.yaml -check -package api
```

`-check` runs the entire parse + generation + format pipeline against the spec but does not touch disk. The output directory is **not** created, no files are written, and `-out` is effectively ignored. A non-zero exit means the spec or the generated code would not have produced clean output. Pair with `-strict` to additionally fail on unknown YAML keys.

### Strict parse

```sh
yaggo -spec api.yaml -strict -check
```

By default the parser ignores unknown keys (the OpenAPI specification's documented behaviour). `-strict` flips that: the YAML decoder rejects every key that doesn't map to a known struct field. Most useful in CI for catching typos like `responsess:` or `paramters:` that would otherwise silently turn into "no responses declared."

### In-place regeneration

```sh
yaggo -spec api.yaml -out . -package api
```

Useful when the `go:generate` directive lives in the same directory as the spec.

### Print version

```sh
yaggo -version
# yaggo v0.2.0     (or "yaggo dev" for a local build)
```

## Output ordering

Files are generated in this deterministic order (matches what gets logged to stdout):

1. `types.go`
2. `server.go`
3. `client.go`
4. `body_types.go` (skipped when no inline bodies exist)
5. `auth.go`

If a generator returns nothing for a file (only `body_types.go` can do this currently), the file is not written and no log line is emitted.

## Output messages

Stdout — one line per generated file:

| Mode | Line |
|------|------|
| Normal | `wrote <path>` |
| `-check` | `ok <filename> (<n> bytes)` |
| `-version` | `yaggo <version>` |

Stderr — warnings and errors:

| Condition | Line |
|-----------|------|
| Empty `paths` and `webhooks` | `yaggo: warning: spec has no paths or webhooks; only types.go will have content` |
| Spec parse failed | `yaggo: parsing spec: …` (includes the spec filename) |
| Output dir creation failed | `yaggo: creating output dir: …` |
| Generator returned an error | `yaggo: generating <filename>: …` |
| Generated source rejected by gofmt | `yaggo: formatting <filename>: …` (followed by `(unformatted file written for debugging)` unless `-check`) |
| File write failed | `yaggo: writing <path>: …` |

## What it does NOT do

- **No `goimports` pass.** The generator runs `go/format` only; if a generated file imports something unused, that's a yaggo bug — please report it.
- **No watch mode.** Pair with [`go generate`](Installation#gogenerate-integration) and your editor's "run on save" feature, or use a tool like [`reflex`](https://github.com/cespare/reflex).
- **No config file.** Every input is a flag. Pairs well with `//go:generate`, and the three-or-four-flag invocation stays readable on one line.

## See also

- [Installation](Installation) — how to get the binary in the first place
- [Generated Types](Generated-Types), [Generated Server](Generated-Server), [Generated Client](Generated-Client) — what each output file contains
- [Vendor Extensions](Vendor-Extensions) — `x-go-type` / `x-go-import` cheat sheet
