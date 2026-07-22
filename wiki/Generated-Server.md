# Generated Server

`server.go` defines a `ServerInterface` (one method per operation) and `RegisterHandlers` which mounts every route onto a [chi v5](https://github.com/go-chi/chi) router.

## Shape

```go
// Implement this on your application type.
type ServerInterface interface {
    ListPets(w http.ResponseWriter, r *http.Request, params ListPetsParams)
    CreatePet(w http.ResponseWriter, r *http.Request, body NewPet)
    GetPet(w http.ResponseWriter, r *http.Request, petId int64)
    UpdatePet(w http.ResponseWriter, r *http.Request, petId int64, body NewPet)
    DeletePet(w http.ResponseWriter, r *http.Request, petId int64)
}

// Wire it up.
func RegisterHandlers(r chi.Router, si ServerInterface, opts ...ServerOption)
```

Path parameters become typed arguments (`petId int64`). Query parameters are collected into a per-operation `<OpName>Params` struct. Header parameters are collected into a separate `<OpName>Headers` struct, passed after `params`. Request bodies become typed arguments (`body NewPet`) and are JSON-decoded *and* `Validate()`-checked before your method is called.

The full signature, when every parameter location is in play, is:

```go
Op(w http.ResponseWriter, r *http.Request,
   pathParam1, pathParam2 …,    // one positional arg per path param, in spec order
   params OpParams,              // present only if the op has query params
   headers OpHeaders,            // present only if the op has header params
   body OpBody,                  // present only if the op has a request body
)
```

Each block (`Params`, `Headers`, `body`) is independent — an op with only path params and a body skips the params/headers structs entirely.

## Minimum viable server

```go
package main

import (
    "log"
    "net/http"

    "github.com/go-chi/chi/v5"
    "your.module/gen/api"
)

type server struct{ /* … */ }

func (s *server) ListPets(w http.ResponseWriter, r *http.Request, params api.ListPetsParams) {
    api.WriteJSON(w, http.StatusOK, []api.Pet{ /* … */ })
}
// … one method per operation …

func main() {
    r := chi.NewRouter()
    api.RegisterHandlers(r, &server{})
    log.Fatal(http.ListenAndServe(":8080", r))
}
```

## What the generated handler does

For every operation, the generated route function:

1. **Parses path parameters** with `strconv.ParseInt`/`ParseFloat`/`ParseBool` (typed to the spec). On parse failure → `ErrorHandler(400, "invalid path param '…'")`.
2. **Collects query parameters** into the `<OpName>Params` struct. Required strings are written directly; optional values are pointers.
3. **Reads header parameters** with `r.Header.Get(name)`. A required header that comes back empty → `ErrorHandler(400, "missing required header …")`. A non-empty value that fails to parse as the declared scalar type → `ErrorHandler(400, "invalid header …")`. Optional values are pointers in the resulting `<OpName>Headers` struct.
4. **Decodes the request body** as JSON (or whatever JSON-compatible content type the spec declares; see [OpenAPI Coverage](OpenAPI-Coverage#content-types)). On decode failure → `ErrorHandler(400, "invalid request body: …")`.
5. **Calls `body.Validate()`** if the body type has the method — but only when a body was actually received. For `requestBody.required: false`, an absent body produces a zero-value struct that is **not** validated (calling `Validate()` on a zero struct would incorrectly reject required fields). On validation failure → `ErrorHandler(400, err)`. The generated `Validate()` covers `required`, `minLength`/`maxLength`, `pattern`, `minimum`/`maximum`, `minItems`/`maxItems`, and recursive validation of enum/struct fields — see [Generated Types](Generated-Types#validation).
6. **Invokes your `ServerInterface` method.** You own the response from there.

## Options

```go
api.RegisterHandlers(r, &server{},
    api.WithMiddleware(middleware.Logger),
    api.WithMiddleware(middleware.Recoverer),
    api.WithErrorHandler(myErrorHandler),
)
```

| Option | Purpose |
|--------|---------|
| `WithMiddleware(mw)` | Append an `http.Handler` middleware. Applied to every generated route. Multiple calls compose outer-first. |
| `WithErrorHandler(h)` | Replace the default JSON error renderer. Signature: `func(w, r, err, status int)`. |

The default error handler writes `{"error": "<err.Error()>"}` as `application/json` with the given status.

## Response writers

The generated handlers don't write your responses for you — your `ServerInterface` method does. Three helpers are exported for that:

```go
// JSON encode v with Content-Type: application/json and the given status.
func WriteJSON(w http.ResponseWriter, status int, v any)

// JSON encode v with a custom Content-Type (e.g. "application/vnd.api+json").
func WriteContent(w http.ResponseWriter, status int, contentType string, v any)

// Convenience: WriteJSON with body {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string)
```

`WriteContent` is the one to use when the spec declares a `+json` structured-syntax variant (problem+json, hal+json, etc.). See [OpenAPI Coverage](OpenAPI-Coverage#content-types).

## Error flow

```
client request
     │
     ▼
chi router  ──── middleware chain (WithMiddleware)
     │
     ▼
generated handler
     │
     ├── parse path params         → ErrorHandler(400)
     ├── collect query params
     ├── read headers              → ErrorHandler(400) on missing required / parse failure
     ├── decode JSON body          → ErrorHandler(400)
     ├── body.Validate()           → ErrorHandler(400)
     │
     ▼
your ServerInterface method
     │
     └── WriteJSON / WriteContent / WriteError / your own response writer
```

All errors *before* your method runs go through the configured `ErrorHandler`. Errors *from* your method are your responsibility — most implementations call `WriteError` for that.

## Body size limit

Every generated handler that decodes a request body wraps `r.Body` in
`http.MaxBytesReader` before handing it to `json.NewDecoder`. The cap is
configurable per server:

```go
api.RegisterHandlers(r, &server{},
    api.WithMaxBodyBytes(2 << 20), // 2 MiB
)
```

The default is `api.DefaultMaxBodyBytes` (1 MiB) — large enough for typical
JSON payloads, small enough that a malicious client can't hold gigabytes of
memory by sending one slow request. A request body that exceeds the limit
returns HTTP 413 (Request Entity Too Large) via the configured `ErrorHandler`;
the decoded body type is never populated. Pass `0` to disable the cap (not
recommended on public networks).

The check happens **before** `Validate()`, so oversized payloads short-circuit
without doing schema work.

## Content-Type enforcement

Each body-decoding handler checks the request `Content-Type` against the media
type the spec declared for that operation. A mismatch returns HTTP 415
(Unsupported Media Type) before any decoding happens. Media-type parameters
(`; charset=utf-8`) are stripped before the comparison, and a missing header is
tolerated (many minimal clients omit it). This keeps a client that POSTs, say,
`text/plain` to a JSON endpoint from getting a confusing parse error instead of
a clear 415.

## Strict bodies (`additionalProperties: false`)

When a request-body schema declares `additionalProperties: false`, the
generated decoder calls `json.Decoder.DisallowUnknownFields()`, so a payload
carrying keys not present in the schema is rejected with HTTP 400 rather than
silently ignored. Schemas without that keyword decode permissively, as before.

## TLS / HTTPS

yaggo generates *handlers*, not listening servers. TLS is configured on the
`http.Server` that wraps the chi router — exactly as you would for any other
chi application:

```go
r := chi.NewRouter()
api.RegisterHandlers(r, &server{})

srv := &http.Server{
    Addr:    ":8443",
    Handler: r,
    // Refuse TLS 1.0/1.1 regardless of the certificate. Cipher suites are
    // left to the stdlib default — Go tracks current best-practice.
    TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
    ReadTimeout:  5 * time.Second,
    WriteTimeout: 10 * time.Second,
}
log.Fatal(srv.ListenAndServeTLS("cert.pem", "key.pem"))
```

The example in [`examples/server/`](../examples/server/) accepts `-cert` and
`-key` flags to demonstrate the pattern. For local development you can mint a
short-lived cert with [`mkcert`](https://github.com/FiloSottile/mkcert):

```sh
mkcert localhost 127.0.0.1 ::1
go run ./examples/server -addr :8443 -cert localhost+2.pem -key localhost+2-key.pem
```

### Mutual TLS

For mTLS, attach a `ClientCAs` pool and require client certs:

```go
caPool := x509.NewCertPool()
caPEM, _ := os.ReadFile("ca.pem")
caPool.AppendCertsFromPEM(caPEM)

srv.TLSConfig = &tls.Config{
    MinVersion: tls.VersionTLS12,
    ClientAuth: tls.RequireAndVerifyClientCert,
    ClientCAs:  caPool,
}
```

The peer certificate is available inside handlers via `r.TLS.PeerCertificates`
— useful for identity-aware middleware that runs before yaggo's generated
parameter parsing.

### Why TLS isn't in the generated code

The chi-router decoupling is intentional: a yaggo-generated package shouldn't
know whether you intend to serve plain HTTP behind a reverse proxy that
terminates TLS, listen directly on port 443, run inside a service mesh that
handles transport security, or attach via a Unix-domain socket. All of these
are configured at the `http.Server` layer.

## HEAD, OPTIONS, TRACE

The 3.1 spec adds these to `PathItem`. The parser accepts them (so specs round-trip), but the generator does not emit handlers for them. If you need them, add the routes manually on the same `chi.Router` you passed to `RegisterHandlers`.

## See also

- [Generated Types](Generated-Types) — the body/parameter types referenced in `ServerInterface`
- [Generated Client](Generated-Client) — the matching client side
- [Authentication](Authentication) — wiring auth into the middleware chain
