# yaggo

**Yet Another OpenAPI Go Generator** — turns an OpenAPI 3.0.x or 3.1.x YAML spec into idiomatic, type-safe Go: structs, a [chi](https://github.com/go-chi/chi) server interface, an HTTP client, and authentication helpers driven by the spec's `securitySchemes`.

## Quick start

```sh
go install github.com/123456890987654321/yaggo/cmd/yaggo@latest

yaggo -spec api.yaml -out ./gen -package api
```

Four files land in `./gen`:

| File | Contents |
|------|----------|
| `types.go` | One Go type per `components/schemas` entry |
| `server.go` | `ServerInterface` + `RegisterHandlers(r chi.Router, ...)` |
| `client.go` | `NewClient(baseURL, ...)` with one method per operation |
| `auth.go` | `SecretString`, generic auth helpers, and one named wrapper per `components.securitySchemes` entry |
| `body_types.go` | Inline request-body structs (only when needed) |

## Navigation

**Getting started**
- [Installation](Installation) — `go install`, `go:generate`, `tools.go`
- [CLI Reference](CLI-Reference) — flags, exit codes, examples

**Generated code**
- [Generated Types](Generated-Types) — schemas → structs, enums, allOf, nullability
- [Generated Server](Generated-Server) — `ServerInterface`, chi wiring, error handling, response writers
- [Generated Client](Generated-Client) — `NewClient`, options, request editors, buffer pooling, body draining
- [Authentication](Authentication) — `SecretString`, `BasicAuth`/`BearerToken`/`APIKey`, spec-driven wrappers

**Reference**
- [OpenAPI Coverage](OpenAPI-Coverage) — 3.0/3.1 features, content types, validation keywords, limitations
- [Vendor Extensions](Vendor-Extensions) — `x-go-type` / `x-go-import` for custom Go types
- [Examples](Examples) — the `examples/` directory walkthrough
- [Development](Development) — building, testing, Makefile

## Project status

| | |
|---|---|
| OpenAPI versions | 3.0.x and 3.1.x |
| Go version | 1.21+ (generated code uses generics and `log/slog`) |
| Server router | [chi v5](https://github.com/go-chi/chi) |
| License | MIT |

## Design goals

1. **Read the generated code.** Output is `gofmt`-clean, hand-readable, and pinned to the smallest dependency surface possible (chi v5 in generated server, stdlib only in generated client and auth).
2. **Spec-driven, not reflection-driven.** Type-safe method signatures, typed enums, typed auth wrappers — no `any`-soup at call sites.
3. **Secure defaults.** HTTPS-only auth helpers with explicit opt-out. Secrets that redact themselves across `fmt`, JSON, `log/slog`, and `encoding.TextMarshaler`.
4. **Tested generated code.** The end-to-end test suite generates the petstore package into a temp module and runs `go test ./...` against it on every CI run.
