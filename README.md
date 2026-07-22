# yaggo

Yet Another Go code Generator for OpenAPI 3.x specs.

Given an OpenAPI 3.x YAML file, `yaggo` emits five ready-to-use, `gofmt`-formatted Go source files:

| File | Contents |
|------|----------|
| `types.go` | Structs and type aliases for every schema in `components/schemas` |
| `server.go` | `ServerInterface` + `RegisterHandlers` (requires [chi v5](https://github.com/go-chi/chi)) |
| `client.go` | Type-safe HTTP client |
| `body_types.go` | Inline request-body structs for operations that don't use a named `$ref` |
| `auth.go` | Auth `RequestEditor` helpers (`BearerToken`, `BasicAuth`, `APIKey`, …) plus typed wrappers for `components.securitySchemes` |

Empty files are skipped (e.g. `body_types.go` is not written when every request body is a `$ref`).

## Installation

```sh
go install github.com/123456890987654321/yaggo/cmd/yaggo@latest
```

Or use `go run` directly — no prior install needed (see [go:generate](#gogenerate)).

## Usage

```sh
yaggo -spec api.yaml -out ./gen -package api
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-spec` | *(required)* | Path to the OpenAPI 3.x YAML spec |
| `-out` | `.` | Output directory; created if it does not exist |
| `-package` | `api` | Go package name written into every generated file |
| `-only` | *(unset)* | Comma-separated subset to generate: `types,server,client,body_types,auth` |
| `-skip` | *(unset)* | Comma-separated files to skip (mutually exclusive with `-only`) |
| `-strict` | `false` | Reject unknown YAML keys in the spec (catches typos) |
| `-check` | `false` | Validate + generate without writing files; useful in CI |
| `-version` | — | Print version and exit |

See the [CLI Reference](https://github.com/123456890987654321/yaggo/wiki/CLI-Reference) for details.

## go:generate

Add a directive to any `.go` file in your project:

```go
//go:generate go run github.com/123456890987654321/yaggo/cmd/yaggo@latest -spec api.yaml -out ./gen -package api
```

Then regenerate whenever the spec changes:

```sh
go generate ./...
```

The `go run` form is preferred: it requires no separate install step and lets you pin an exact version:

```go
//go:generate go run github.com/123456890987654321/yaggo/cmd/yaggo@v0.1.0 -spec api.yaml -out ./gen -package api
```

If you prefer a locally installed binary, add it to your `tools.go`:

```go
//go:build tools

package tools

import _ "github.com/123456890987654321/yaggo/cmd/yaggo"
```

## Generated output

### types.go

Each entry in `components/schemas` becomes one of:

- **Object / allOf** → `struct` with JSON tags. Required fields are non-pointer; optional fields are pointers.
- **String enum** → named `string` type with typed constants and a `Validate() error` method.
- **Scalar alias** → type alias (`type ID = int64`).

Structs with required fields get a `Validate() error` method that checks zero values and delegates to any enum `Validate()` methods on their fields.

### server.go

Requires [chi v5](https://github.com/go-chi/chi) (`github.com/go-chi/chi/v5`) in the consuming module.

- **`ServerInterface`** — one method per operation. Implement this interface to serve the API.
- **`RegisterHandlers(r chi.Router, si ServerInterface, opts ...ServerOption)`** — mounts all routes onto the router.
- **`WithMiddleware(mw)`** — appends an `http.Handler` middleware applied to every route.
- **`WithErrorHandler(h)`** — overrides the default JSON error renderer (`{"error": "..."}`).
- **`WriteJSON(w, status, v)`** / **`WriteError(w, status, msg)`** — helpers for writing responses from your handler implementations.

The generated handlers parse and type-check path parameters before calling your method, collect query parameters into a `<OpName>Params` struct, JSON-decode request bodies, and call `Validate()` when the body type has the method.

### client.go

- **`NewClient(baseURL, ...ClientOption)`** — creates a client backed by `http.DefaultClient`.
- One method per operation with typed path params, optional query params (`*T`), and a body argument where applicable.
- Successful 2xx responses are JSON-decoded into the return type; 4xx/5xx returns an error.
- **`WithHTTPClient(hc)`** — swap the underlying transport (timeouts, custom `RoundTripper`).
- **`WithHeader(key, value)`** / **`WithUserAgent(ua)`** — default headers sent on every request.
- **`WithRequestEditor(fn)`** — hook called after the request is built; use for auth headers, tracing, etc.

### body_types.go

Structs for request bodies that are defined inline in the spec (no `$ref`). Not written when every request body references a named component schema.

### auth.go

Authentication helpers for the generated client. The file contains two layers:

**Generic helpers** — usable with any client, irrespective of the spec:

| Helper | Purpose |
|--------|---------|
| `SecretString` | A `string` newtype that redacts itself via `fmt` (`%v`, `%s`, `%q`, `%x`, `%X`, `%#v`), `encoding/json`, `encoding.TextMarshaler`, and `log/slog`. The underlying value can only be retrieved via `.Reveal()` or an explicit `string()` cast. |
| `BasicAuth(user, pass, opts...)` | HTTP Basic credentials (RFC 7617). |
| `BearerToken(token, opts...)` | Static `Authorization: Bearer …` (RFC 6750). |
| `BearerTokenSource(fn, opts...)` | Dynamic bearer token — `fn(ctx)` is called per request, so OAuth refresh / vault-backed tokens work transparently. |
| `APIKey(name, key, location, opts...)` | API key in either `APIKeyHeader` or `APIKeyQuery`. |
| `AllowInsecure()` | Opt-out of the default HTTPS-only enforcement; intended for local development against loopback servers. |
| `ErrInsecureScheme` | Sentinel for `errors.Is`, returned when HTTPS is required and the request URL is `http`. |

All helpers return a `RequestEditor`, so they plug straight into `WithRequestEditor(...)`. Errors raised by an editor (e.g. failing the HTTPS check or a `BearerTokenSource` callback) abort the request — there is no fallback to "send unauthenticated."

**Spec-driven wrappers** — emitted for every entry in `components.securitySchemes`:

| Spec scheme | Generated helper |
|-------------|------------------|
| `type: http`, `scheme: bearer` | `New<Name>(token SecretString, ...)` → wraps `BearerToken` |
| `type: http`, `scheme: basic`  | `New<Name>(username string, password SecretString, ...)` → wraps `BasicAuth` |
| `type: apiKey`, `in: header`   | `New<Name>(key SecretString, ...)` → wraps `APIKey(name, ..., APIKeyHeader)` |
| `type: apiKey`, `in: query`    | `New<Name>(key SecretString, ...)` → wraps `APIKey(name, ..., APIKeyQuery)` plus an inline leak warning in the doc comment |
| `oauth2`, `openIdConnect`, `mutualTLS`, or unknown `scheme` | No helper; a comment in `auth.go` explains why |

The wrappers exist so call sites read in spec terms (`petstore.NewBearerAuth(token)`), not generic terms. For example, given:

```yaml
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
    ApiKey:
      type: apiKey
      in: header
      name: X-API-Key
```

a call site looks like:

```go
import "os"

c, err := petstore.NewClient("https://api.example.com",
    petstore.WithRequestEditor(petstore.NewBearerAuth(
        petstore.SecretString(os.Getenv("PETSTORE_TOKEN")),
    )),
)
```

**Security posture**

- HTTPS is required by default for every auth helper. Passing an `http://` URL produces `ErrInsecureScheme` with no secret in the message.
- `SecretString` plugs the five most common accidental-leak paths (`fmt`, JSON, text marshal, slog, `%#v`). Reflection and explicit string conversion are intentional bypasses and remain available for code that has to forward credentials.
- The generator never emits a secret into godoc, error messages, or struct tags. The auth template is itself unit-tested for the absence of leak patterns.

## Examples

[`examples/petstore.yaml`](examples/petstore.yaml) is a self-contained Petstore spec that exercises the main features. The [`examples/`](examples/) directory is a standalone Go module containing:

| Path | Description |
|------|-------------|
| [`examples/petstore/`](examples/petstore/) | Generated types, server interface, and client (committed output) |
| [`examples/server/`](examples/server/) | In-memory server implementing `ServerInterface` with chi |
| [`examples/client/`](examples/client/) | Client demo that creates, lists, updates, and deletes pets |

### Run the examples

```sh
# terminal 1 — start the server
go run ./examples/server

# terminal 2 — run the client
go run ./examples/client
```

### Regenerate the petstore package

```sh
# one-time: install the tool
go install github.com/123456890987654321/yaggo/cmd/yaggo@latest

cd examples && go generate ./petstore/...
```

The `//go:generate` directive lives in [`examples/petstore/doc.go`](examples/petstore/doc.go).

## Vendor extensions

Two `x-` keys on a `components/schemas` entry override the generator's type inference:

```yaml
components:
  schemas:
    Timestamp:
      type: string
      format: date-time
      x-go-type: time.Time
      x-go-import: time
    PetID:
      x-go-type: uuid.UUID
      x-go-import: github.com/google/uuid
```

- `x-go-type` — Go type emitted for this schema (verbatim).
- `x-go-import` — optional import path added to `types.go`.

`x-go-type` only applies to entries under `components/schemas`; inline schemas are parsed but ignored. See [the wiki](https://github.com/123456890987654321/yaggo/wiki/Vendor-Extensions) for the full contract.

## What is supported

- OpenAPI **3.0.x** and **3.1.1** YAML (the parser handles both; specs are accepted with no version-flag dance)
- HTTP methods: `GET`, `POST`, `PUT`, `PATCH`, `DELETE` for code generation; `HEAD`, `OPTIONS`, `TRACE` are parsed but not wired into the server template
- Path, query, and **header** parameters: `string`, `integer`, `number`, `boolean`, named enum types
- Request and response bodies: `application/json` plus any RFC 6839 `+json` structured-syntax variant (e.g. `application/vnd.api+json`, `application/hal+json`, `application/problem+json`). Per-operation `Content-Type` and `Accept` headers are emitted from the spec, not hardcoded; the JSON encoder/decoder is used for all variants.
- Schema types: object, string enum, array, `additionalProperties` map, scalar, `allOf`
- `$ref` references within `#/components/schemas/`
- Nullable fields via **both** 3.0's `nullable: true` and 3.1's `type: [..., "null"]` — produce pointer types in either case
- **Validation keywords** emitted into the generated `Validate()` method on structs: `minLength`, `maxLength`, `pattern` (strings), `minimum`, `maximum` (numbers), `minItems`, `maxItems` (arrays) — in addition to `required` and enum delegation
- `components.securitySchemes` — see [auth.go](#authgo) for the generated wrappers
- Vendor extensions: `x-go-type` and `x-go-import` on `components/schemas` entries
- 3.1-only fields parsed (no code-generation impact, but specs round-trip cleanly): `webhooks`, `jsonSchemaDialect`, `info.summary`, `info.license.identifier`, `exclusiveMinimum`/`exclusiveMaximum` as numbers, `const`, `examples` array, `readOnly`/`writeOnly`/`deprecated`

## Limitations

- Only local `$ref`s are resolved (`#/components/schemas/...` and `#/components/parameters/...`); external file references are not supported.
- `oneOf` / `anyOf` schemas are not code-generated (fields fall back to `any`).
- Cookie parameters are parsed but not wired in the generated server or client (header parameters are).
- Non-JSON request bodies (`multipart/form-data`, XML, form-encoded) are not decoded — a warning is printed and the handler is generated without a body argument.
- The generated server router requires chi v5; other routers are not supported.

## Requirements

- Go 1.23 or later to build/install the `yaggo` binary
- Go 1.21 or later in the module that consumes the generated code (generics in the client, `http.MaxBytesError` in the server)
- [chi v5](https://github.com/go-chi/chi) in the consuming module (for the generated server)

## License

[MIT](LICENSE)
