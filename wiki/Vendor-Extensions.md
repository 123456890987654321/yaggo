# Vendor Extensions

OpenAPI reserves the `x-` namespace for tooling-specific extensions. yaggo recognises two of them. Both live on entries under `components/schemas`; placing them anywhere else (parameters, request bodies, inline schemas, properties) parses cleanly but has no effect on the generated code.

| Key | Purpose | Required |
|-----|---------|----------|
| `x-go-type` | Override the Go type emitted for this schema | yes |
| `x-go-import` | Import path that satisfies `x-go-type` | only when the type lives outside the package |

## Why this exists

The default mapping is `format`-driven: `type: string, format: date-time` → `string`, `type: string, format: uuid` → `string`. That's perfectly safe but loses semantics — your handler has to call `time.Parse(time.RFC3339, …)` everywhere a timestamp shows up, and your business logic juggles raw `string`s when a `uuid.UUID` would be more honest.

`x-go-type` lets the schema author state, once, "this is a `time.Time` everywhere it appears." The generator emits a Go type alias, and every property that `$ref`s the schema picks it up automatically.

## Examples

### Time

```yaml
components:
  schemas:
    Timestamp:
      type: string
      format: date-time
      x-go-type: time.Time
      x-go-import: time
```

generates (in `types.go`):

```go
import "time"

type Timestamp = time.Time
```

A property that references it:

```yaml
Pet:
  type: object
  properties:
    createdAt:
      $ref: '#/components/schemas/Timestamp'
```

becomes `CreatedAt *Timestamp` (optional → pointer) — which **is** `*time.Time`, because Go type aliases are transparent. You can call `t.Year()`, `t.Format(...)`, marshal it through `encoding/json` (which already knows how to handle `time.Time`), all without yaggo emitting any extra glue.

### Third-party type

```yaml
components:
  schemas:
    PetID:
      x-go-type: uuid.UUID
      x-go-import: github.com/google/uuid
```

```go
import "github.com/google/uuid"

type PetID = uuid.UUID
```

You can omit `type: string` entirely when using `x-go-type` — the override fully replaces what the generator would have inferred.

### Stdlib type already in scope

```yaml
components:
  schemas:
    UserID:
      x-go-type: string
```

No `x-go-import` because `string` is a builtin. The generator emits:

```go
type UserID = string
```

Same effect as a regular scalar alias, but explicit at the spec level — useful if you want to keep the option of switching to a richer type later without touching call sites.

### Type with a method set

```yaml
components:
  schemas:
    Decimal:
      x-go-type: decimal.Decimal
      x-go-import: github.com/shopspring/decimal
```

The generated `type Decimal = decimal.Decimal` carries all of `decimal.Decimal`'s methods. You can `d.Add(other)`, `d.Cmp(...)`, etc. directly on values of the spec type.

## Where it is honoured

Only on top-level entries in `components/schemas`. The contract is small on purpose:

- The generator emits **one type alias per overridden schema** in `types.go`.
- The corresponding import lands in `types.go`'s import block.
- Every other generated file (`server.go`, `client.go`, `body_types.go`) references the schema by name — and that name resolves through the alias to the override.

If you set `x-go-type` on an inline schema — say, a property or a parameter schema — the generator currently ignores it. There's no place to put the import that wouldn't leak it into other files. The escape hatch: lift that schema into `components/schemas` and `$ref` it.

## What is **not** validated

- yaggo validates `x-go-type` against a character allowlist (blocks injection characters like `;`, `{`, `}`, `(`, `)`, and quotes) but does not verify that the value is a syntactically correct Go type expression. A malformed type emits in the generated source and fails at `go build`.
- yaggo does not check that `x-go-import` is reachable or that the named type actually exists in the imported package. Those failures surface at `go build`, not at `yaggo`.
- The schema's original keywords (`type`, `format`, `enum`, validation constraints) are **not** enforced once `x-go-type` is set. The override is total: no `Validate()` method, no constants block, no struct body. If you need validation on top of a custom type, write a method on the underlying type yourself.

## Naming an override that already exists

A schema with `x-go-type` skips struct/enum/Validate generation entirely. That means:

- No `func (Timestamp) Validate() error` even if the original schema had an `enum` or a `pattern`.
- No `const TimestampPending Timestamp = ...` even if the original schema declared `enum:`.
- No struct body, no JSON tags — because the alias is, well, an alias.

If you want both an override **and** validation, do it in Go:

```go
// my_pkg/custom.go
package my_pkg

import "github.com/123456890987654321/yaggo/examples/petstore"

func ValidateTimestamp(t petstore.Timestamp) error { /* … */ }
```

## See also

- [Generated Types](Generated-Types) — how scalar / object / enum / alias generation works without overrides
- [CLI Reference](CLI-Reference) — flags that affect generation behaviour
- [OpenAPI Coverage](OpenAPI-Coverage) — which OpenAPI keywords actually drive code generation
