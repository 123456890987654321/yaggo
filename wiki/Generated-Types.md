# Generated Types

`types.go` contains one Go type for every entry in `components/schemas`, plus a `Validate()` method for any type with required fields, enum values, or referenced enums.

## Schema → Go type mapping

| OpenAPI schema | Generated Go |
|----------------|--------------|
| `type: object` with `properties` | `struct` with JSON-tagged fields |
| `type: string` with `enum` | Named `string` type + typed constants + `Validate()` |
| `type: integer` (scalar component) | Type alias (`type ID = int64`) |
| `type: array` | `[]T` |
| `type: object` with `additionalProperties: <T>` | `map[string]T` |
| `allOf: [A, B, …]` | Single merged struct (properties + required deduplicated) |
| `$ref: '#/components/schemas/X'` | The Go type `X` (or `*X` if optional/nullable) |

## Objects

```yaml
NewPet:
  type: object
  required: [name]
  properties:
    name: { type: string }
    tag:  { type: [string, "null"] }
```

becomes:

```go
type NewPet struct {
    Name string  `json:"name"`
    Tag  *string `json:"tag,omitempty"`
}

func (v NewPet) Validate() error {
    if v.Name == "" {
        return fmt.Errorf("field 'name' is required")
    }
    return nil
}
```

**Pointer rules**

- A property is non-pointer when it is in `required:` *and* not nullable.
- A property is pointer when it is optional **or** nullable (3.0's `nullable: true` or 3.1's `type: [..., "null"]`).
- The `omitempty` JSON tag is applied to optional fields so absent values stay absent on the wire.

## Enums

```yaml
PetStatus:
  type: string
  enum: [available, pending, sold]
```

becomes:

```go
type PetStatus string

const (
    PetStatusAvailable PetStatus = "available"
    PetStatusPending   PetStatus = "pending"
    PetStatusSold      PetStatus = "sold"
)

func (v PetStatus) Validate() error {
    switch v {
    case PetStatusAvailable, PetStatusPending, PetStatusSold:
        return nil
    }
    return fmt.Errorf("invalid PetStatus value: %q", string(v))
}
```

Two enum values that normalise to the same Go identifier (e.g. `"foo-bar"` and `"foo_bar"` both become `FooBar`) are caught at generation time — the generator returns a clear error instead of producing code that fails to compile.

## allOf composition

```yaml
Pet:
  allOf:
    - $ref: '#/components/schemas/NewPet'
    - type: object
      required: [id]
      properties:
        id: { type: integer, format: int64 }
```

is merged into a single flat struct (`Pet` gets `Id` plus everything from `NewPet`). Required field lists are deduplicated so `Validate()` doesn't emit duplicate checks.

## Validation surface

`Validate() error` is generated when **any** of these hold:

1. The schema has `required: [...]` with at least one string/array/map field (a required `int`/`bool`/`number` has no detectable "missing" state, so it alone does not trigger `Validate()`).
2. The schema is an enum (`string`, `integer`, `number`, or `boolean`).
3. A property's `$ref` points at an enum or a constrained primitive (the parent's `Validate()` calls into the referenced type's `Validate()`, following alias chains).
4. A property's `$ref` points at a schema with its own `Validate()` (nested struct validation).
5. A property declares any of: `minLength`, `maxLength`, `pattern` (strings); `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf` (numbers); `minItems`, `maxItems`, `uniqueItems`, or item-level constraints (arrays); `minProperties`, `maxProperties` (maps); an inline `enum`.

If none apply, no `Validate()` is generated, and the type can be used without ceremony.

### Keyword checks

```yaml
User:
  type: object
  required: [username, age]
  properties:
    username:
      type: string
      minLength: 3
      maxLength: 32
      pattern: '^[a-z][a-z0-9_]*$'
    age:
      type: integer
      minimum: 0
      maximum: 130
    tags:
      type: array
      items: { type: string }
      minItems: 1
      maxItems: 5
```

becomes (abridged):

```go
import (
    "fmt"
    "regexp"
    "unicode/utf8"
)

type User struct {
    Username string   `json:"username"`
    Age      int      `json:"age"`
    Tags     []string `json:"tags,omitempty"`
}

var (
    patternUserUsername = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func (v User) Validate() error {
    if v.Username == "" {
        return fmt.Errorf("field 'username' is required")
    }
    if l := utf8.RuneCountInString(v.Username); l < 3 {
        return fmt.Errorf("field 'username' must be at least 3 characters, got %d", l)
    }
    if l := utf8.RuneCountInString(v.Username); l > 32 {
        return fmt.Errorf("field 'username' must be at most 32 characters, got %d", l)
    }
    if !patternUserUsername.MatchString(v.Username) {
        return fmt.Errorf("field 'username' does not match required pattern")
    }
    if v.Age < 0 {
        return fmt.Errorf("field 'age' must be >= 0, got %v", v.Age)
    }
    if v.Age > 130 {
        return fmt.Errorf("field 'age' must be <= 130, got %v", v.Age)
    }
    if l := len(v.Tags); l > 5 {
        return fmt.Errorf("field 'tags' must have at most 5 items, got %d", l)
    }
    return nil
}
```

Notes on the emitted code:

- **String length** uses `utf8.RuneCountInString` — counts code points, not bytes. `"héllo"` has length 5, not 6.
- **Patterns** are compiled once at package init via a `regexp.MustCompile(...)` package-level variable named `pattern<Struct><Field>`. If your pattern uses PCRE-only features (lookaround, backreferences), the package will panic on import — those features aren't in Go's `regexp` (RE2).
- **Optional scalar fields** (which are pointers) wrap the constraint block in `if v.Field != nil { ... }`. Numbers and lengths inside use `(*v.Field)`.
- **Pattern errors don't quote the regex back** at the caller — they're noisy and rarely actionable. The field name is enough to find the relevant schema entry.

### Nested validation

When a property is a `$ref` to a struct that has its own `Validate()`, the parent's `Validate()` delegates:

```go
if err := v.Owner.Validate(); err != nil {
    return fmt.Errorf("field 'owner': %w", err)
}
```

Optional `$ref` properties are pointers; the call is guarded by `if v.Owner != nil`.

## Description handling

Descriptions on schemas and properties become single-line Go comments. Newlines in descriptions are stripped (replaced with spaces) before emission — this prevents a maliciously crafted spec from breaking the comment boundary and injecting arbitrary code. See [Authentication](Authentication) for the analogous protection on credentials.

## Vendor extensions

Two `x-` keys override the inferred Go type on a `components/schemas` entry:

```yaml
Timestamp:
  type: string
  format: date-time
  x-go-type: time.Time
  x-go-import: time
```

produces:

```go
import "time"

type Timestamp = time.Time
```

A schema with `x-go-type` skips struct / enum / `Validate()` generation entirely — the alias is the whole output. See [Vendor Extensions](Vendor-Extensions) for the full contract.

## Constraint keywords that ARE enforced

These produce checks inside the generated `Validate()`:

- Strings: `minLength`, `maxLength`, `pattern`
- Numbers: `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum` (both the 3.0 boolean and 3.1 numeric forms), `multipleOf`
- Arrays: `minItems`, `maxItems`, `uniqueItems`, plus item-level string/number constraints — including `exclusiveMinimum`/`exclusiveMaximum` on items — enforced in a per-element loop
- Maps (`additionalProperties`): `minProperties`, `maxProperties`
- Enums: `string`, `integer`, `number`, and `boolean` (named type + constant block + membership switch)

NaN / ±Inf bounds (`minimum: .nan`, `maximum: .inf`) are emitted as `math.NaN()` / `math.Inf(±1)`; NaN/±Inf as *enum entries* are rejected at generation time because Go's `const` block requires constant expressions.

### Caveats

**`multipleOf` on `number` (float) schemas** — the check uses `math.Mod(float64(v), divisor) != 0`. IEEE 754 representation means `math.Mod(0.3, 0.1) ≈ 5.6e-17`, not zero, so a value of `0.3` with `multipleOf: 0.1` will be incorrectly rejected. This is inherent to floating-point arithmetic; the OpenAPI spec permits implementation-defined behaviour for this keyword. If exact divisibility matters, declare the field as `type: integer` scaled by a power of ten, or validate manually after parsing.

**`uniqueItems` with non-primitive item types** — the generated check uses Go's `map[any]struct{}` for deduplication. This panics at runtime if the array's element type is not comparable (e.g. an `array` of `array`s, or an `object` with unconstrained additional properties, which becomes `map[string]any`). yaggo emits a warning at generation time for these cases. For non-comparable element types, remove `uniqueItems: true` and enforce the constraint in application logic.

**`required` on `integer`/`bool`/`number` fields** — JSON has no way to distinguish a missing field from the zero value (`0`, `false`, `0.0`). Go's generated struct uses a plain non-pointer for these types, so a required `integer` field generates no zero-value check — the field will silently deserialise as `0` if absent from the JSON payload. Use a pointer type via `x-go-type: *int` or enforce presence at the HTTP boundary (e.g. require the field in a JSON Schema validator).

## Keywords that are parsed but not code-generated

These are accepted by the parser (so 3.1.x specs round-trip cleanly), surfaced via the spec API, but **not** reflected in the generated Go types:

- `readOnly`, `writeOnly`, `deprecated`
- `const`, `examples`, `title`
- `oneOf`, `anyOf`, `not` (the field falls back to `any`)

If you need any of these enforced, do it in your handler or via a validation pass (e.g. a JSON Schema validator at the HTTP boundary).

## See also

- [Generated Server](Generated-Server) — how generated types flow into `ServerInterface` method signatures
- [Generated Client](Generated-Client) — how they flow into typed client methods
- [OpenAPI Coverage](OpenAPI-Coverage) — full list of supported and parsed-only keywords
