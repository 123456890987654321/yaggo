# Generated Client

`client.go` defines a `Client` struct with one typed method per operation. Constructed via `NewClient(baseURL, opts...)`; HTTP via `http.DefaultClient` unless overridden.

## Shape

```go
c, err := api.NewClient("https://api.example.com")
if err != nil { /* invalid URL */ }

pets, err := c.ListPets(ctx, ptr(int32(10)), ptr(api.PetStatusAvailable))
buddy, err := c.CreatePet(ctx, api.NewPet{Name: "Buddy"})
err  = c.DeletePet(ctx, buddy.Id)
```

Generated method signatures use:

- Path parameters → typed positional args (`petId int64`).
- Required query parameters → typed args; optional → pointers (`*int32`).
- Header parameters → typed args after query params; required values go in the request unconditionally, optional values only when non-nil. Required headers use the typed value (string `xTraceId string`), optional headers are pointers (`xPage *int`).
- Request body → typed positional arg.
- Successful 2xx body → typed return value. 4xx/5xx returns an error.
- No body → just `error`.

Full argument order: `ctx, pathParams…, queryParams…, headerParams…, body`. Operation-defined headers override `WithHeader` defaults but are themselves overridable by `RequestEditor`s (which run last).

## `NewClient` validation

```go
func NewClient(baseURL string, opts ...ClientOption) (*Client, error)
```

The constructor validates `baseURL` and normalises it:

- Must parse as a URL.
- Scheme must be `http` or `https`.
- Host must be non-empty.
- Any trailing slash is stripped (`http://x.com/` → `http://x.com`) so request paths never produce `http://x.com//pets`.

Invalid URLs fail fast at construction, not on the first request.

## Client options

```go
c, err := api.NewClient("https://api.example.com",
    api.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
    api.WithHeader("X-Trace-ID", traceID),
    api.WithUserAgent("acme-cli/1.0"),
    api.WithRequestEditor(api.BearerToken(token)),
)
```

| Option | Purpose |
|--------|---------|
| `WithHTTPClient(hc)` | Replace the default client. **Always set a `Timeout`** — the default has none and a slow server will hang goroutines indefinitely. Also used for custom transports and mTLS. |
| `WithHeader(k, v)` | Default header sent on every request. Later calls overwrite earlier ones for the same key. |
| `WithUserAgent(ua)` | Shortcut for `WithHeader("User-Agent", ua)`. |
| `WithRequestEditor(fn)` | Per-request mutator called *after* the request is built and *before* dispatch. Used for auth (see [Authentication](Authentication)) and any other late-binding header logic. |
| `WithMaxResponseBytes(n)` | Cap the bytes read from any response body. Defends against an upstream returning gigabytes of JSON. Default is `DefaultMaxResponseBytes` (8 MiB); pass `0` to disable. |

> **Always set a request timeout.** The default `*http.Client` has no timeout;
> a slow or unresponsive server will hang goroutines indefinitely. Pass a
> client with `Timeout` set:
>
> ```go
> api.WithHTTPClient(&http.Client{Timeout: 10 * time.Second})
> ```

> `NewClient` rejects a `baseURL` that carries a query string or fragment —
> attach default query params via `WithRequestEditor` instead, so request
> paths can't silently merge with the base URL's query.

> **Redirect scheme-downgrade protection.** The default client installs a
> `CheckRedirect` policy that removes the `Authorization` header when a
> redirect crosses from `https` to `http` on the same host. If you supply
> your own client via `WithHTTPClient`, add an equivalent `CheckRedirect`
> if your threat model requires it. See [Authentication](Authentication#redirect-scheme-downgrade-protection).

## RequestEditor

```go
type RequestEditor func(ctx context.Context, req *http.Request) error
```

Editors run in registration order. The first one to return an error aborts the request — the HTTP call is never made. This is the only extension point for adding auth, tracing, signature headers, etc.

## Error handling

- Network / transport failures: wrapped and returned.
- 4xx/5xx responses: `decodeResponse` first tries to JSON-decode `{"error": "<msg>"}`. On success it returns `fmt.Errorf("api error %d: %s", status, msg)`. When the body is absent, not valid JSON, or uses a different key (e.g. `{"message": "..."}` from FastAPI/DRF), the body is discarded and the error is `fmt.Errorf("http error: %d", status)`. If you need the raw error body, install a `WithRequestEditor` that wraps the response, or inspect the error in a custom `ErrorHandler` on the server side.
- Decode failures on 2xx: returned as `fmt.Errorf("decoding response: %w", err)`.

## Content-Type and Accept

Both headers come from the spec — they are **not** hardcoded to `application/json`. The generator picks:

- `Content-Type` for the request body: exact `application/json` if declared, otherwise the alphabetically first `*+json` variant (RFC 6839 structured-syntax suffix). No body → header not set.
- `Accept` for the response: the same logic against the first 2xx response. Defaults to `application/json` when no 2xx body is declared, so error-response JSON decoding still works.

This makes the client correct for `application/vnd.api+json`, `application/hal+json`, `application/problem+json` (RFC 7807), etc.

## Performance

Two non-obvious design choices live in the generated `do()`:

### Request body buffering

```go
var buf bytes.Buffer
json.NewEncoder(&buf).Encode(body)
reqBody = &buf
```

A per-request `bytes.Buffer` is allocated rather than a `sync.Pool` of buffers. The reason: `http.NewRequestWithContext` with a `*bytes.Buffer` captures `v.Bytes()` — a slice into the buffer's backing array — for `req.GetBody` (used on redirect retries). Pooling the buffer back after dispatch risks a different goroutine overwriting the array while a custom transport still holds the request for replay. The allocation cost is negligible; the data race is not.

### Response body draining

```go
const drainLimit = 64 << 10 // 64 KiB

func drainAndClose(body io.ReadCloser) {
    _, _ = io.CopyN(io.Discard, body, drainLimit)
    _ = body.Close()
}
```

HTTP/1.1 keep-alive requires the response body to be **both** fully consumed and closed before the underlying TCP connection can return to the pool. `json.Decoder.Decode` reads exactly one JSON value and stops, so a successful decode leaves at least the trailing newline that `json.Encoder.Encode` emits server-side. `drainAndClose` runs via `defer` on every code path (success, error, and bodyless), guaranteeing connection reuse.

The drain is bounded at 64 KiB. This covers typical trailing whitespace without forcing the client to download gigabytes from a misbehaving upstream. Bodies larger than the limit cause `Close()` to release the connection without reuse — a fair trade-off.

## Editing a request mid-flight

Common pattern: stamp a header derived from request context.

```go
api.WithRequestEditor(func(ctx context.Context, req *http.Request) error {
    if id := tracing.IDFrom(ctx); id != "" {
        req.Header.Set("X-Trace-ID", id)
    }
    return nil
})
```

Common pattern: refuse to send when a precondition fails.

```go
api.WithRequestEditor(func(ctx context.Context, req *http.Request) error {
    if !rateLimiter.Allow() {
        return errors.New("rate limit exceeded")
    }
    return nil
})
```

## mTLS and certificate rotation

The generated client does **not** wrap TLS configuration in its own options
on purpose — `crypto/tls` and `net/http.Transport` already expose the full
knob set, and yaggo would only ever cover a subset. The escape hatch is
`WithHTTPClient`: build the `*http.Client` you need and inject it.

### Static mTLS

When the certificate, key, and CA bundle live as files and never change for
the lifetime of the process:

```go
import (
    "crypto/tls"
    "crypto/x509"
    "net/http"
    "os"

    "your.module/gen/petstore"
)

func newMTLSClient(baseURL, certPEM, keyPEM, caPEM string) (*petstore.Client, error) {
    cert, err := tls.LoadX509KeyPair(certPEM, keyPEM)
    if err != nil {
        return nil, err
    }
    caBytes, err := os.ReadFile(caPEM)
    if err != nil {
        return nil, err
    }
    pool := x509.NewCertPool()
    if !pool.AppendCertsFromPEM(caBytes) {
        return nil, fmt.Errorf("no CA certs parsed from %s", caPEM)
    }
    return petstore.NewClient(baseURL, petstore.WithHTTPClient(&http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{
                MinVersion:   tls.VersionTLS12,
                Certificates: []tls.Certificate{cert},
                RootCAs:      pool,
            },
        },
    }))
}
```

`MinVersion: tls.VersionTLS12` refuses pre-TLS 1.2 negotiations regardless of
cipher list. Concrete cipher suites are left to Go's default — the stdlib
tracks current best practice and rotates as algorithms age, so pinning a list
here would only let it grow stale.

### Hot-reloading client certificates

Long-running services rotate certs (cert-manager renewals, SPIFFE workload
API, Vault agent template, etc.) and the running process must pick up new
material without a restart. The Go idiom is the
[`tls.Config.GetClientCertificate`](https://pkg.go.dev/crypto/tls#Config.GetClientCertificate)
callback: it fires for every new TLS handshake, so a fresh closure-returned
certificate is used on every reconnect. Pair it with an in-memory store that
swaps the cert under a mutex when the filesystem changes:

```go
type certStore struct {
    mu   sync.RWMutex
    cert tls.Certificate
}

func (s *certStore) get(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    c := s.cert
    return &c, nil
}

func (s *certStore) load(certPEM, keyPEM string) error {
    c, err := tls.LoadX509KeyPair(certPEM, keyPEM)
    if err != nil {
        return err
    }
    s.mu.Lock()
    s.cert = c
    s.mu.Unlock()
    return nil
}

// reloader polls the cert files; switch to fsnotify when atomic rename
// (mv onto target) is your only rotation signal — atime/mtime-based polling
// misses unchanged-size in-place writes.
func (s *certStore) reloader(ctx context.Context, certPEM, keyPEM string, every time.Duration) {
    t := time.NewTicker(every)
    defer t.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-t.C:
            if err := s.load(certPEM, keyPEM); err != nil {
                slog.Warn("cert reload failed", "err", err)
            }
        }
    }
}
```

Wire the store into the transport:

```go
store := &certStore{}
if err := store.load("client.pem", "client-key.pem"); err != nil { /* fatal */ }
go store.reloader(ctx, "client.pem", "client-key.pem", 30*time.Second)

c, _ := petstore.NewClient(baseURL, petstore.WithHTTPClient(&http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            MinVersion:           tls.VersionTLS12,
            GetClientCertificate: store.get, // ← per-handshake lookup
            RootCAs:              pool,
        },
    },
}))
```

Connection-reuse caveat: existing keep-alive connections continue using
whichever cert they handshook with. New handshakes pick up the rotated cert.
If your rotation cadence is shorter than your idle-timeout, force
short-lived connections with `http.Transport.IdleConnTimeout` or
`MaxIdleConnsPerHost: 0`.

For the corresponding **root-CA pool rotation** the simplest stable pattern is
`tls.Config.VerifyPeerCertificate`: it receives the peer chain and decides
acceptance against the current pool snapshot, so swapping a pool reference
under a mutex updates verification on the next handshake without rebuilding
the transport.

### Don't roll your own — use a library

The snippets above are minimum-viable. In production, prefer one of:

- [`github.com/spiffe/go-spiffe/v2`](https://github.com/spiffe/go-spiffe) —
  SPIFFE workload-API sourced identity; handles rotation, X.509 SVIDs, and
  trust bundle updates transparently.
- [cert-manager's CSI driver](https://github.com/cert-manager/csi-driver) —
  mounts short-lived certs into pods, supports atomic rotation; pair with a
  simple polling reloader (above).
- [HashiCorp Vault agent](https://developer.hashicorp.com/vault/docs/agent-and-proxy/agent/template)
  with file templates — agent renders certs to disk and signals (via
  command exec, SIGHUP, or atomic rename) when material changes.

The reloader in yaggo's wiki is *educational*. Anything user-facing in
production should sit behind one of these established components.

## See also

- [Authentication](Authentication) — the `RequestEditor`-based auth helpers and spec-driven wrappers
- [Generated Server](Generated-Server) — the matching server side
- [Generated Types](Generated-Types) — the parameter and return types
