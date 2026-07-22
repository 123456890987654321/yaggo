# Authentication

`auth.go` is generated alongside `client.go` and contains two layers:

1. **Generic helpers** (`BasicAuth`, `BearerToken`, `BearerTokenSource`, `APIKey`) usable with any client.
2. **Spec-driven wrappers** — one named function per entry in `components.securitySchemes`, so call sites read in spec terms.

All helpers return a `RequestEditor`, so they plug into `WithRequestEditor(...)`.

## SecretString

The credential type at the centre of everything:

```go
type SecretString string

// Plugged leak paths:
func (SecretString) String() string                   // fmt %v, %s, %q, %x, %X
func (SecretString) GoString() string                 // fmt %#v (incl. struct fields)
func (SecretString) MarshalJSON() ([]byte, error)     // encoding/json
func (SecretString) MarshalText() ([]byte, error)     // encoding.TextMarshaler
func (SecretString) LogValue() slog.Value             // log/slog records
func (s SecretString) Reveal() string                  // the only documented unwrap
```

Every redaction returns the literal placeholder `[REDACTED]`.

```go
pw := SecretString(os.Getenv("API_PASSWORD"))
fmt.Println(pw)                          // [REDACTED]
fmt.Printf("%q %#v", pw, pw)             // "[REDACTED]" [REDACTED]
json.Marshal(struct{ Pw SecretString }{pw}) // {"Pw":"[REDACTED]"}
slog.Info("auth", "pw", pw)              // …pw=[REDACTED]
```

The only sanctioned bypasses are `pw.Reveal()` and `string(pw)`. Both make the leak point easy to grep for in code review. Reflection and the `unsafe` package can also recover the value — those are out of scope.

## Generic helpers

### BasicAuth

```go
func BasicAuth(username string, password SecretString, opts ...AuthOption) RequestEditor
```

Sets `Authorization: Basic <base64(user:pass)>` per RFC 7617. Refuses to run over `http://` unless `AllowInsecure()` is passed.

```go
api.WithRequestEditor(api.BasicAuth("alice", api.SecretString(pw)))
```

### BearerToken (static)

```go
func BearerToken(token SecretString, opts ...AuthOption) RequestEditor
```

Sets `Authorization: Bearer <token>` per RFC 6750. Token is captured once at construction.

### BearerTokenSource (rotating)

```go
func BearerTokenSource(fn func(ctx context.Context) (SecretString, error), opts ...AuthOption) RequestEditor
```

`fn` runs per request, receiving the request's context (so it honours deadlines and cancellation). Use it for OAuth refresh flows, vault-backed credentials, short-lived service tokens.

```go
api.WithRequestEditor(api.BearerTokenSource(func(ctx context.Context) (api.SecretString, error) {
    tok, err := tokenCache.Get(ctx)
    if err != nil {
        return "", err
    }
    return api.SecretString(tok), nil
}))
```

If `fn` is nil, every request fails with a clear error instead of silently sending no token. If `fn` returns an error, the request is aborted and the error is wrapped with `"BearerTokenSource: fetching token: ..."`.

### APIKey

```go
type APIKeyLocation int

const (
    APIKeyHeader APIKeyLocation = iota
    APIKeyQuery
)

func APIKey(name string, key SecretString, location APIKeyLocation, opts ...AuthOption) RequestEditor
```

`name` is the header or query-parameter name. `APIKeyQuery` should be a last resort — keys in URLs leak through access logs, upstream proxies, browser history, and Referer headers.

## AllowInsecure

```go
func AllowInsecure() AuthOption
```

Disables the HTTPS check. **Use only for local development** against `http://localhost`. Failures otherwise return:

```go
var ErrInsecureScheme = errors.New("refusing to send credentials over non-https URL")
```

Detect with `errors.Is(err, api.ErrInsecureScheme)`.

## Spec-driven wrappers

Every entry in `components.securitySchemes` becomes a named wrapper. Given:

```yaml
components:
  securitySchemes:
    BearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
      description: JWT issued by the auth service.
    BasicAuth:
      type: http
      scheme: basic
    ApiKey:
      type: apiKey
      in: header
      name: X-API-Key
```

the generator emits:

```go
func NewBearerAuth(token SecretString, opts ...AuthOption) RequestEditor   // wraps BearerToken
func NewBasicAuth(username string, password SecretString, opts ...AuthOption) RequestEditor  // wraps BasicAuth
func NewApiKey(key SecretString, opts ...AuthOption) RequestEditor         // wraps APIKey("X-API-Key", …, APIKeyHeader)
```

Call sites then read in spec terms:

```go
c, err := petstore.NewClient(baseURL,
    petstore.WithRequestEditor(petstore.NewBearerAuth(
        petstore.SecretString(os.Getenv("PETSTORE_TOKEN")),
    )),
)
```

## Scheme mapping

| Spec | Generated |
|------|-----------|
| `type: http`, `scheme: bearer`  | `New<Name>(token SecretString, opts ...)` → `BearerToken` |
| `type: http`, `scheme: basic`   | `New<Name>(username string, password SecretString, opts ...)` → `BasicAuth` |
| `type: apiKey`, `in: header`    | `New<Name>(key SecretString, opts ...)` → `APIKey(headerName, …, APIKeyHeader)` |
| `type: apiKey`, `in: query`     | `New<Name>(key SecretString, opts ...)` → `APIKey(paramName, …, APIKeyQuery)` + leak warning in doc |

Schemes that **don't** produce a wrapper (and why):

| Spec | Why no wrapper |
|------|---------------|
| `type: oauth2` | Requires a flow implementation. Use `BearerTokenSource` with your OAuth library's refresh function. |
| `type: openIdConnect` | Requires endpoint discovery. The doc comment includes the configured `openIdConnectUrl`. |
| `type: mutualTLS` | Configured at the TLS layer (`http.Transport.TLSClientConfig`), not as a header. |
| `type: http`, `scheme: digest` (or other) | Not supported by the generator. |
| `type: apiKey`, `in: cookie` | Not yet supported. |

In each case the generator emits a single-line comment in `auth.go` explaining the skip, so reviewers can find the gap.

## Redirect scheme-downgrade protection

Go's `net/http` client copies the `Authorization` header to redirect requests
whenever the destination is the same host — without checking whether the scheme
changed from `https` to `http`. This means a server that responds to an
initial HTTPS request with `301 → http://same-host/path` can silently receive
credentials in cleartext.

The generated `client.go` closes this gap with a custom `CheckRedirect` policy
installed on the default `*http.Client`:

```go
func safeRedirectPolicy(req *http.Request, via []*http.Request) error {
    if len(via) > 0 && via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
        req.Header.Del("Authorization")
    }
    return nil
}
```

The policy fires automatically when `NewClient` is called without
`WithHTTPClient`. If you supply your own `*http.Client` via `WithHTTPClient`,
you own the redirect policy — add an equivalent `CheckRedirect` if your threat
model requires it.

## Security posture summary

| | |
|---|---|
| HTTPS enforcement | Default-on for every auth helper; explicit `AllowInsecure()` opt-out per editor. |
| Redirect scheme downgrade | `Authorization` header stripped automatically on HTTPS→HTTP redirect by the default client's `CheckRedirect`. |
| Error messages | Mechanism name + URL scheme only — secrets never appear in error text (verified by tests). |
| Leak surface | Five common paths plugged (`fmt`/JSON/text/slog/`%#v`); reflection and `string()` bypass are intentional. |
| Generator output audit | Unit tests assert the literal `[REDACTED]` placeholder is present and that no `Reveal()`-into-fmt pattern is emitted. |
| `gosec` | 0 findings on the generator and the generated example. |

## See also

- [Generated Client](Generated-Client) — `WithRequestEditor` and the client side
- [OpenAPI Coverage](OpenAPI-Coverage#security-schemes) — the full list of scheme types and how each is handled
