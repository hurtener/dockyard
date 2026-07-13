# Contracts (Design A)

Dockyard is contract-first ([RFC §6](/reference/rfc)). A tool's typed Go
input and output structs are the source of truth. The JSON Schema the
host sees and the TypeScript types the App imports are both
**generated** by `dockyard generate` — never hand-written. The
`dockyard validate` quality gate catches a drift.

## Where contracts live

```text
internal/
  contracts/
    contracts.go                 # typed Go structs — source of truth
    greet_input.schema.json      # GENERATED — per-tool input schema
    greet_output.schema.json     # GENERATED — per-tool output schema
    contracts.ts                 # GENERATED — App-facing TypeScript
.dockyard/
  generated-artifacts.json       # GENERATED — owned paths + SHA-256 digests
```

Never edit a generated schema, `contracts.ts`, or the ownership index.
`dockyard validate` rejects hand-edits and incomplete ownership metadata.
The ownership index is byte-canonical: unknown fields, reordered records, or
formatting changes are stale generated state even when the recorded digests are
otherwise correct.

All manifest `tools[].input` and `tools[].output` references must name exported
types in `internal/contracts`. Dockyard rejects another package rather than
generating schemas without the corresponding TypeScript declarations.

## A well-written contract

```go
// CreateMetricCardInput is the model-facing input for create_metric_card.
type CreateMetricCardInput struct {
    // Label is the metric's title (e.g. "Monthly Recurring Revenue").
    Label string `json:"label"`
    // Value is the headline numeric value.
    Value float64 `json:"value"`
    // Unit is an optional unit suffix ("USD", "ms", "%").
    Unit string `json:"unit,omitempty"`
    // Theme is the per-call theme override; "auto" honours the host.
    Theme ThemeMode `json:"theme,omitempty"`
}
```

- **Document every field.** Comments are lifted into the JSON Schema's
  `description` and the TypeScript JSDoc.
- **`omitempty` for optional fields.** Mark optional fields with
  `omitempty` so the codegen marks them optional in the schema.
- **`,string` for scalar wire strings only.** Dockyard supports
  `json:",string"` on booleans, strings, integers, floats, and one unnamed
  pointer to those scalar types. Pointer fields are nullable; `omitempty` also
  makes them optional. Aggregate, interface, named-pointer, and deeper-pointer
  uses fail generation because `encoding/json` does not quote them.
- **Named scalars for constrained values.** A `ChartType` named type
  with documented allowed values teaches the model the right input.
- **The `Kind` discriminator pattern** — for outputs that drive a
  multi-renderer App, carry a string `Kind` so the App dispatches
  without sniffing the shape.

## Generate, validate, test

```bash
dockyard generate    # rewrite the generated artifacts (idempotent)
dockyard validate    # catches stale codegen + the CrossCheck drift
dockyard test        # the full contract + spec + capability gate
```

`dockyard build` runs `generate` + `validate` automatically — a stale
contract never ships.

Generation writes one input and one output schema per manifest tool, combines
the package's Go declarations into `internal/contracts/contracts.ts`, and
records every current generated path and digest in
`.dockyard/generated-artifacts.json`. When a tool or enum artifact disappears,
generation removes the obsolete file only if the indexed path is in a known
generated namespace, its bytes still match the recorded SHA-256 digest, and its
generated marker is valid. Modified files, symlinked paths, nested projects, and
arbitrary project paths are never deleted. `validate` and `test` also require
the index to contain every current generated path with the current digest.
Artifact and ownership publication is rooted at the project directory, so a
concurrent symlink replacement cannot redirect writes or renames outside it.
Backend-only projects generate `contracts.ts` at the same path, so generated
ownership is independent of whether `web/` exists. A UI-bearing project imports
that canonical artifact from `internal/contracts/contracts.ts`.

Imported Go types are expanded to their JSON wire shape. Dockyard preserves
recursive imported structs as named recursive TypeScript declarations and
matches standard-library schema overrides such as `time.Time` and `slog.Level`.
A canonical or imported type implementing `json.Marshaler` or
`encoding.TextMarshaler` is rejected unless Dockyard has an explicit wire-shape
override for it; define an explicit contract wire type instead. Both value and
pointer receivers are checked, while unrelated methods that merely share the
`MarshalJSON` or `MarshalText` name remain valid. This includes `math/big.Int`,
`math/big.Rat`, and `math/big.Float`, whose JSON shapes depend on whether a
value is addressable; expose them as explicit integers or decimal strings.

## Schema dialect and references

Dockyard emits JSON Schema 2020-12 only. It retains the same
`google/jsonschema-go` inference engine used by the Go SDK and extends it for
recursive Go types. Recursive contracts generate deterministic local `$defs`
keys qualified by the Go package import path and local `$ref` back-edges.

Validation is deliberately local and bounded: external `$ref` and
`$dynamicRef` targets are rejected, as are schemas over 4 MiB, nesting beyond
128 levels, more than 100,000 schema nodes, or more than 1,000,000 units of
local-reference work. This prevents contract validation from becoming network
I/O or unbounded work.

## Output can be any JSON value

Tool inputs remain object-shaped, but a typed output may encode any JSON value:
an object, array, string, number, boolean, or null. Use the output type that
matches the value rather than wrapping a primitive solely to satisfy an old
object-only restriction.

A typed nil pointer, map, slice, or interface is omitted by default. To return a
present JSON null, set `StructuredPresent` explicitly:

```go
return tool.Result[*LookupOutput]{
    Structured:        nil,
    StructuredPresent: true,
}, nil
```

The runtime emits both `structuredContent: null` and the required JSON text
fallback. Leaving `StructuredPresent` false omits both.

## Why this matters

mcp-use, the closest competitor, has *types* but not *contracts* — the
widget's `useWidget<{...}>()` generic is hand-declared, and if the
server's output drifts the generic silently lies (brief
[04 §2.6](https://github.com/hurtener/dockyard/blob/main/docs/research/04-mcp-use-dx-teardown.md)).
Dockyard catches that drift at `dockyard validate`, before the binary
ships. P1 is the framework's headline. Lean on it.

## See also

- [`define-contracts` agent skill](/agent-skills/) — the same surface
  taught as an agent workflow.
- [Validate + test guide](validate) — the full quality-gate surface.
- [Decisions: D-113 — `dockyard validate` runs CrossCheck](/reference/decisions)
- [Decision D-193 — contract and response semantics](/reference/decisions)
