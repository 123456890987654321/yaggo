# Installation

Three ways to use yaggo: install the binary, run it via `go run`, or pin it via `tools.go`.

## go install (recommended for terminal use)

```sh
go install github.com/123456890987654321/yaggo/cmd/yaggo@latest
```

Installs the `yaggo` binary into `$GOPATH/bin` (typically `~/go/bin`). Make sure that's on your `PATH`.

To pin a specific version, replace `@latest` with a tag:

```sh
go install github.com/123456890987654321/yaggo/cmd/yaggo@v0.1.0
```

## go run (no install)

```sh
go run github.com/123456890987654321/yaggo/cmd/yaggo@latest -spec api.yaml -out ./gen -package api
```

The first invocation downloads to the module cache; subsequent calls are cached. Useful in CI where you don't want a separate install step.

## go:generate integration

The recommended way to wire yaggo into your project: put a `go:generate` directive next to the spec or in a package-level `doc.go`:

```go
//go:generate go run github.com/123456890987654321/yaggo/cmd/yaggo@v0.1.0 -spec ../api.yaml -out . -package api

package api
```

Then regenerate any time the spec changes:

```sh
go generate ./...
```

### Why `go run` and not the installed binary?

Pinning via `@v0.1.0` in the directive guarantees that every developer and every CI run uses the same generator version. The installed-binary form (`//go:generate yaggo ...`) is shorter but requires every developer to install the right version separately.

## tools.go (lock the version in go.mod)

If you prefer the installed-binary form but still want version pinning, use the `tools.go` pattern. Create a file like this in your module:

```go
//go:build tools

package tools

import _ "github.com/123456890987654321/yaggo/cmd/yaggo"
```

Run `go mod tidy` and `yaggo` will appear in `go.mod` (under a build tag so it's never linked into your binaries). Install with:

```sh
go install github.com/123456890987654321/yaggo/cmd/yaggo
```

The installed version now matches `go.mod`.

## From source

```sh
git clone https://github.com/123456890987654321/yaggo
cd yaggo
make install
```

The `Makefile` runs `go install ./cmd/yaggo`. See [Development](Development) for the full target list.

## Verify

```sh
yaggo -spec /dev/null -out /tmp/probe -package probe 2>&1 | head -1
```

If the binary is on `PATH`, you'll see an `error: ...` from the spec parser (because `/dev/null` is empty) — that's the success indicator that the binary runs.
