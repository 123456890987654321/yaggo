# OpenAPI Coverage

yaggo accepts both **OpenAPI 3.0.x** and **3.1.x** YAML. The parser handles the structural differences between them (notably nullable representation and `exclusiveMinimum` shape) so a spec authored to either version works without flags.

## Versions

| | 3.0.x | 3.1.x |
|---|---|---|
| Accepted | ✅ | ✅ |
| Nullable via `nullable: true` | ✅ | ⚠️ deprecated, still works |
| Nullable via `type: ["string","null"]` | ❌ | ✅ |
| `exclusiveMinimum: true` (boolean) | ✅ | ⚠️ deprecated, still parses |
| `exclusiveMinimum: 5` (number) | ❌ | ✅ |
| `webhooks` at root | ❌ | ✅ parsed |
| `info.summary` | ❌ | ✅ |
| `info.license.identifier` (SPDX) | ❌ | ✅ |
| `jsonSchemaDialect` | ❌ | ✅ |

The validator only rejects strings that don't start with `"3."`. Anything 3.x parses; the differences listed above are handled at the keyword level.

## HTTP methods

| Method | Server | Client |
|--------|--------|--------|
| `GET`, `POST`, `PUT`, `PATCH`, `DELETE` | ✅ wired | ✅ wired |
| `OPTIONS`, `HEAD`, `TRACE` | ✅ wired | ✅ wired |

All eight methods generate handlers and client methods. A declared `TRACE`
operation also triggers a generation-time warning — `TRACE` echoes the request
back and is a Cross-Site Tracing (XST) vector, so reject it at the proxy or via
middleware unless you have a specific reason to serve it.

## Schema features

### Types

| Schema | Status |
|--------|--------|
| `object` with `properties` | ✅ struct |
| `object` with `additionalProperties: <schema>` | ✅ `map[string]T` |
| `object` with `additionalProperties: true` | ✅ permissive (default) |
| `object` with `additionalProperties: false` | ✅ strict — request bodies decode with `DisallowUnknownFields` (unknown JSON keys → 400) |
| `array` with `items` | ✅ `[]T` |
| `string` | ✅ |
| `string`/`integer`/`number`/`boolean` with `enum` | ✅ named type + constants + `Validate()` |
| `integer` (`int32`, `int64`, default `int`) | ✅ |
| `number` (`float`, default `float64`) | ✅ |
| `boolean` | ✅ |
| `null` (alone) | ⚠️ → `any` |
| `type: [...]` array (3.1) | ✅ primary type + null-detection |
| `$ref: '#/components/schemas/X'` | ✅ |
| `allOf` | ✅ merged, required dedup'd |
| `oneOf`, `anyOf`, `not` | ⚠️ parsed only → `any` |

### Validation keywords

| Keyword | Generation impact |
|---------|---|
| `required` | Drives non-pointer fields + zero/nil check in `Validate()` |
| `nullable: true` (3.0) | Forces pointer type |
| `type: ["X","null"]` (3.1) | Forces pointer type |
| `minLength`, `maxLength` (strings) | Enforced in `Validate()` via `utf8.RuneCountInString` |
| `pattern` (strings) | Enforced in `Validate()` via a package-level `regexp.MustCompile` |
| `minimum`, `maximum` (numbers/integers) | Enforced in `Validate()` (NaN/±Inf bounds emit `math.NaN()`/`math.Inf`) |
| `exclusiveMinimum`/`exclusiveMaximum` (bool or number) | Enforced in `Validate()` (strict `>`/`<`) |
| `multipleOf` | Enforced in `Validate()` via `math.Mod` |
| `minItems`, `maxItems` (arrays) | Enforced in `Validate()` |
| `uniqueItems` (arrays) | Enforced in `Validate()` via a seen-set scan |
| array `items` constraints | `minLength`, `maxLength`, `pattern` (strings) and `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` (numbers/integers) enforced per element in a loop |
| `minProperties`, `maxProperties` (maps) | Enforced in `Validate()` via `len()` |
| `const`, `examples` (array), `example` | Parsed only |
| `readOnly`, `writeOnly`, `deprecated` | Parsed only |
| `title`, `description` | Description becomes a Go comment (newline-stripped for safety) |

"Parsed only" means the field is on the `spec.Schema` Go type and survives a round-trip but does not affect code generation. Enforce these in your handlers if you need them.

The enforced checks live inside the `Validate() error` method on each generated struct. For optional scalar fields (which are pointers), the constraint block is guarded by a nil check. For required fields, the check is unconditional. Error messages quote the JSON field name, e.g. `field 'username' must be at least 3 characters, got 1`. Pattern errors say `does not match required pattern` (without quoting the regex back at the caller — patterns can be noisy).

Regex semantics follow Go's `regexp/syntax` (RE2). Most OpenAPI patterns work unchanged; lookarounds, backreferences, and other PCRE-only features will fail at `regexp.MustCompile` time, which means generation succeeds but the generated file panics on import. If your pattern relies on those features, validate in your handler instead.

### Vendor extensions

| Key | Effect |
|-----|--------|
| `x-go-type` | Override the Go type emitted for a `components/schemas` entry. Skips struct/enum/Validate generation entirely. |
| `x-go-import` | Import path added to `types.go` for the type used by `x-go-type`. Omit for builtins. |

See [Vendor Extensions](Vendor-Extensions) for the contract.

## Content types

The generator detects JSON-compatible content types per **RFC 6839 §3.1** (structured-syntax suffix). Both the `Content-Type` request header and `Accept` are populated from the spec, not hardcoded.

| Media type | Recognised |
|------------|-----------|
| `application/json` | ✅ (preferred when present) |
| `application/json; charset=utf-8` (with params) | ✅ |
| `application/vnd.api+json` (JSON:API) | ✅ |
| `application/hal+json` (HAL) | ✅ |
| `application/problem+json` (RFC 7807) | ✅ |
| `application/x-amz-json-1.1` and other `+json` | ✅ |
| Any other `*+json` | ✅ |
| `application/xml` | ❌ |
| `multipart/form-data` | ❌ |
| `application/x-www-form-urlencoded` | ❌ |
| `text/plain` | ❌ |

When multiple JSON-compatible types are declared, exact `application/json` wins; otherwise the alphabetically first `+json` variant.

For non-JSON variants, request encoding still uses `json.NewEncoder` and response decoding uses `json.NewDecoder` — the variants must be JSON-compatible at the wire-format level (which all `+json` types are by RFC 6839 definition).

## Security schemes

See [Authentication](Authentication) for the full per-scheme handling. Summary:

| Scheme | Wrapper generated |
|--------|-------------------|
| `http` + `bearer` | ✅ → `BearerToken` |
| `http` + `basic` | ✅ → `BasicAuth` |
| `apiKey` + `header` | ✅ → `APIKey(name, …, APIKeyHeader)` |
| `apiKey` + `query` | ✅ + leak warning |
| `apiKey` + `cookie` | ❌ |
| `oauth2` | ❌ (use `BearerTokenSource` with your OAuth library) |
| `openIdConnect` | ❌ (discover endpoints and use `BearerTokenSource`) |
| `mutualTLS` | ❌ (configure at `http.Transport.TLSClientConfig`) |

## Refs

| Ref form | Supported |
|----------|-----------|
| `#/components/schemas/X` | ✅ |
| `#/components/parameters/X` | ✅ resolved into the operation's parameter list |
| `responses/...`, `requestBodies/...`, `headers/...` | parsed only |
| External file refs (`./other.yaml#/...`) | ❌ |
| Remote URL refs | ❌ |

A `$ref` whose target doesn't exist (a typo, or a missing component) is a
**generation-time error**, not a silent fallback — you get a clear message
naming the unresolved ref rather than an `undefined: T` at `go build`. Cyclic
type aliases (`A → B → A`) are also rejected up front, since Go would refuse the
recursive alias.

## Parameter locations

| `in:` | Status |
|-------|--------|
| `path` | ✅ wired into server (chi `URLParam`) and client (formatted into the path) |
| `query` | ✅ wired into server (`r.URL.Query`) and client (`url.Values`) |
| `header` | ✅ wired into server (`r.Header.Get`) and client (`req.Header.Set`); required headers return 400 when missing |
| `cookie` | parsed only |

## Root-level keywords

| Key | Behaviour |
|-----|-----------|
| `openapi` | Version-validated (`3.x`) |
| `info` | Title/version/summary/description/license/contact all parse |
| `servers` | Parses (`Server`, `ServerVariable`) — not used for default `baseURL` |
| `paths` | Drives generation |
| `webhooks` (3.1) | Parses — no generation |
| `components` | Drives generation (`schemas`, `securitySchemes`) |
| `security` | Parses (root + per-operation) — not enforced in generated code |
| `tags` | Parses |
| `externalDocs` | Parses |
| `jsonSchemaDialect` (3.1) | Parses |

## Hard limitations

These are out of scope and unlikely to change soon:

- **Non-JSON content types.** Generation is JSON-only at the encode/decode level.
- **`oneOf` / `anyOf` polymorphism.** Fields are emitted as `any`. Add a custom `UnmarshalJSON` to your type if needed.
- **External `$ref`.** Only local component refs are resolved.
- **OAuth2 flow implementations.** Use a library (e.g. `golang.org/x/oauth2`) and pipe the token through `BearerTokenSource`.
- **Cookie parameters.** Parsed but not wired into the router or client. Use middleware that translates cookies into headers if you need them.

## See also

- [Generated Types](Generated-Types) — exactly what each schema construct produces
- [Authentication](Authentication) — security-scheme detail
- [Generated Server](Generated-Server), [Generated Client](Generated-Client) — what each method signature looks like
