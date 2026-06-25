# Examples

The [`examples/`](https://github.com/123456890987654321/yaggo/tree/main/examples) directory is a standalone Go module that demonstrates the full pipeline end-to-end.

## Layout

```
examples/
├── go.mod                   # github.com/123456890987654321/yaggo/examples
├── go.sum
├── petstore.yaml            # the OpenAPI 3.1.1 spec
├── petstore/                # generated package (committed)
│   ├── doc.go               # //go:generate directive + package doc
│   ├── types.go
│   ├── server.go
│   ├── client.go
│   └── auth.go
├── server/
│   └── main.go              # in-memory server implementing ServerInterface
└── client/
    └── main.go              # client demo exercising every endpoint
```

The `examples/` module is separate from the root yaggo module so that the chi dependency required by the generated server doesn't leak into the generator's own `go.mod`.

## Running

```sh
# terminal 1 — start the server (listens on :8080)
go run ./examples/server

# terminal 2 — run the client demo
go run ./examples/client
```

The server keeps pets in an in-memory `sync.RWMutex`-guarded map, applies chi's `Logger` and `Recoverer` middleware, and uses an explicit `http.Server` with read/write/idle timeouts.

The client creates two pets, lists them with and without a status filter, fetches one by ID, updates it, then deletes one and re-lists to confirm — exercising every generated client method.

## Regenerating the petstore package

```sh
# one-time: install yaggo
go install github.com/123456890987654321/yaggo/cmd/yaggo@latest

cd examples && go generate ./petstore/...
```

The directive lives in `examples/petstore/doc.go`:

```go
//go:generate yaggo -spec ../petstore.yaml -out . -package petstore

// Package petstore contains the generated types, server interface, and HTTP
// client for the Petstore API defined in ../petstore.yaml.
package petstore
```

You can also regenerate via the project's Makefile from the repo root:

```sh
make example
```

That target builds `bin/yaggo` first, then runs it against the spec — useful when you've changed the generator and want to see the diff in `examples/petstore/`.

## What the spec exercises

[`examples/petstore.yaml`](https://github.com/123456890987654321/yaggo/blob/main/examples/petstore.yaml) is deliberately small but exercises the features users hit first:

| Feature | Where in the spec |
|---------|-------------------|
| `openapi: "3.1.1"` | top |
| `info.summary` + `info.license.identifier` | top |
| 3.1 nullable form (`type: [string, "null"]`) | `NewPet.tag` |
| String enum + `Validate()` | `PetStatus` |
| `allOf` composition | `Pet = NewPet + {id}` |
| Path parameter (int64) | `/pets/{petId}` |
| Query parameters (int32, named enum) | `GET /pets` |
| Request body with `Validate()` | `POST /pets` |
| Three security schemes (bearer / basic / apiKey-header) | `components.securitySchemes` |

Inspect the generated `examples/petstore/auth.go` to see the spec-driven wrappers (`NewBearerAuth`, `NewBasicAuth`, `NewApiKey`).

## What the generated code looks like

```go
// examples/petstore/client.go (excerpt)
func (c *Client) ListPets(ctx context.Context, limit *int32, status *PetStatus) ([]Pet, error) {
    path := "/pets"
    q := url.Values{}
    if limit != nil  { q.Set("limit",  strconv.FormatInt(int64(*limit), 10)) }
    if status != nil { q.Set("status", string(*status)) }
    resp, err := c.do(ctx, "GET", path, nil, "", "application/json", q)
    if err != nil { var zero []Pet; return zero, err }
    return decodeResponse[[]Pet](resp)
}
```

## See also

- [Installation](Installation) — `go:generate` patterns including the one used here
- [Authentication](Authentication) — the spec-driven `New*` wrappers visible in `examples/petstore/auth.go`
- [Development](Development) — building and testing the generator itself
